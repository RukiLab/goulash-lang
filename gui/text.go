package gui

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Out exposes os.Stdout for exec passthrough in GUI mode.
func (b *WindowBackend) Out() io.Writer {
	return os.Stdout
}

// textOp is one ordered step of a mes()/print() call: an optional buffer
// wipe or upward scroll, followed by an optional text fragment. A whole
// call is applied as one game-thread closure in order, so text shares the
// canvas layer with pset()/line()/boxf(): a later boxf() covers earlier
// text, and later text covers an earlier boxf() (call order wins, like HSP).
type textOp struct {
	clear    bool // wipe the buffer (a scroll jump past a whole screen)
	scrollPx int  // shift the buffer image up first
	frag     bool // draw the fragment after scrolling
	x, y     int  // character-cell origin
	s        string
	fg       color.NRGBA
	st       TextStyle
	face     *text.GoTextFace
	charW    float64
	lineH    float64
	fontSize float64
}

// textDraw is one Print/Println call bound to its target buffer.
type textDraw struct {
	sel int
	ops []textOp
}

// textBatch accumulates consecutive Print/Println calls before they are
// handed to the game thread. A burst of mes()/print() calls is merged into
// one ordered op list (and normally one queued closure), while any other
// drawing, immediate frame read, yield, or target switch flushes first so
// script call order is preserved. Batched calls are not atomic: they are
// only a spool for repetition, mirroring Ebiten's own command batching.
type textBatch struct {
	sel int
	ops []textOp
}

func (x *textBatch) empty() bool {
	return x == nil || len(x.ops) == 0
}

// merge appends the next text call. Adjacent upward scrolls can be summed:
// with no fragment/clear in between, shifting twice in a row moves the
// same pixels as shifting once by the total.
func (x *textBatch) merge(sel int, ops []textOp) {
	if len(ops) == 0 {
		return
	}
	if x.sel != sel {
		x.sel = sel
	}
	for _, op := range ops {
		if n := len(x.ops); n > 0 && op.scrollPx > 0 && !op.clear && !op.frag &&
			x.ops[n-1].scrollPx > 0 && !x.ops[n-1].clear && !x.ops[n-1].frag {
			x.ops[n-1].scrollPx += op.scrollPx
			continue
		}
		x.ops = append(x.ops, op)
	}
}

// Print appends text without a newline, rasterized onto the current canvas
// buffer like any other drawing.
func (b *WindowBackend) Print(s string, st TextStyle) {
	b.printRaw(s, st, false)
}

// Println appends text with a trailing newline, rasterized onto the
// current canvas buffer like any other drawing.
func (b *WindowBackend) Println(s string, st TextStyle) {
	b.printRaw(s, st, true)
}

// printRaw lays out one call on the cell grid and stages it in the pending
// text batch (nil when nothing is visible). It returns the draw so tests
// can inspect fragment positions and styles.
func (b *WindowBackend) printRaw(s string, st TextStyle, newline bool) *textDraw {
	b.mu.Lock()
	d := b.layoutTextLocked(s, st, newline)
	if d != nil {
		if b.textBatch == nil {
			b.textBatch = &textBatch{}
		}
		b.textBatch.merge(d.sel, d.ops)
	}
	b.mu.Unlock()
	return d
}

// flushTextBatchLocked moves the accumulated Print/Println calls into one
// game-thread closure. Caller must hold mu.
func (b *WindowBackend) flushTextBatchLocked() {
	if b.textBatch.empty() {
		return
	}
	d := &textDraw{sel: b.textBatch.sel, ops: b.textBatch.ops}
	b.textBatch = nil
	if b.yieldedOnce {
		b.pending = append(b.pending, func() { b.applyTextDraw(d) })
		return
	}
	b.queue = append(b.queue, func() { b.applyTextDraw(d) })
}

// layoutTextLocked splits s into drawable fragments, advancing the text
// cursor. When the cursor passes the bottom edge the whole screen scrolls
// (text and graphics share one screen, like HSP). Caller must hold mu;
// the returned draw captures all rendering state, so later font()/color()
// calls never affect already-queued text.
func (b *WindowBackend) layoutTextLocked(s string, st TextStyle, newline bool) *textDraw {
	sel := b.sel
	// Text honors its own alpha multiplied by the global alpha (galpha),
	// exactly like fillColor() does for shapes.
	fg := color.NRGBA{R: b.fg.R, G: b.fg.G, B: b.fg.B,
		A: uint8(uint16(b.fg.A) * uint16(b.alpha) / 255)}
	face, charW, lineH, fontSize := b.face, b.charW, b.lineH, b.fontSize
	if lineH <= 0 {
		lineH = lineHeight
	}
	h := b.h
	rows := int(float64(h) / lineH)
	if rows < 1 {
		rows = 1
	}
	lastRow := rows - 1
	d := &textDraw{sel: sel}
	// scrollForRow keeps the cursor row visible. A jump past a whole
	// screen (e.g. pos() far below) clears instead of replaying hundreds
	// of full-screen shifts: the old content would scroll away anyway.
	scrollForRow := func() {
		if b.curY <= lastRow {
			return
		}
		if b.curY-lastRow > rows {
			d.ops = append(d.ops, textOp{clear: true})
			b.curY = lastRow
			return
		}
		for b.curY > lastRow {
			// Fractional line heights (odd font sizes) round here;
			// the carry keeps the long-term error under half a pixel.
			b.scrollExact += lineH
			want := int(b.scrollExact + 0.5)
			px := want - b.scrollDone
			if px < 1 {
				// Sub-pixel heights can round to no step; always move at
				// least one pixel so the text never stalls.
				px = 1
				want = b.scrollDone + px
			}
			b.scrollDone = want
			d.ops = append(d.ops, textOp{scrollPx: px})
			b.curY--
		}
	}
	if s != "" {
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			if i > 0 {
				b.curY++
				b.curX = 0
			}
			// pos() may have parked the cursor off-screen: scroll (or
			// clear on a huge jump) before drawing, not after.
			scrollForRow()
			if ln == "" {
				continue
			}
			d.ops = append(d.ops, textOp{frag: true, x: b.curX, y: b.curY, s: ln,
				fg: fg, st: st, face: face, charW: charW, lineH: lineH, fontSize: fontSize})
			// Advance by display cells (full-width = 2), not runes:
			// continuation print() fragments start at curX, so a
			// rune-count advance overlaps CJK after the first char.
			b.curX += cellCount(ln)
		}
	}
	if newline {
		b.curY++
		b.curX = 0
		scrollForRow()
	}
	if len(d.ops) == 0 {
		return nil
	}
	return d
}

// applyTextDraw runs a laid-out call on the game thread, in order.
func (b *WindowBackend) applyTextDraw(d *textDraw) {
	img := b.ensureTarget(d.sel)
	for _, op := range d.ops {
		if op.clear {
			img.Clear()
		}
		if op.scrollPx > 0 {
			b.scrollCanvasUp(img, op.scrollPx)
		}
		if op.frag {
			drawTextFrag(img, op)
		}
	}
}

// scrollCanvasUp shifts the buffer image up by px pixels, leaving the
// bottom strip transparent (black shows through). A scratch copy keeps the
// overlapping self-blit well-defined; the scratch is reused per size so
// scrolling stays allocation-free after the first shift.
func (b *WindowBackend) scrollCanvasUp(img *ebiten.Image, px int) {
	if px <= 0 {
		return
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if px >= h {
		img.Clear()
		return
	}
	upper := img.SubImage(image.Rect(0, px, w, h)).(*ebiten.Image)
	if b.scrollScratch == nil {
		b.scrollScratch = map[[2]int]*ebiten.Image{}
	}
	key := [2]int{w, h - px}
	tmp, ok := b.scrollScratch[key]
	if !ok || tmp == nil {
		tmp = ebiten.NewImage(w, h-px)
		b.scrollScratch[key] = tmp
	}
	tmp.DrawImage(upper, nil)
	img.Clear()
	img.DrawImage(tmp, nil)
}

// drawTextFrag rasterizes one fragment onto the canvas buffer with the
// faux text styles (single deformed face: bold over-strikes, italic
// shears, underline rules below the baseline). Because it writes straight
// into the canvas, shapes can cover the text and the text can cover
// shapes, depending on call order.
func drawTextFrag(img *ebiten.Image, op textOp) {
	if op.s == "" || op.face == nil || op.charW <= 0 || op.lineH <= 0 {
		return
	}
	baseX := float64(op.x) * op.charW
	baseY := float64(op.y) * op.lineH
	italic := op.st&StyleItalic != 0
	draw := func(dx float64) {
		dop := &text.DrawOptions{}
		// Region top lands on the origin; glyphs hang below it.
		dop.GeoM.Translate(baseX+dx, baseY)
		if italic {
			// Element (0,1) is b in x' = a*x + b*y + tx:
			// slant glyphs right around the pen point.
			dop.GeoM.SetElement(0, 1, italicShear)
		}
		// ColorScale zero value is transparent; reset to identity first.
		dop.ColorScale.Reset()
		dop.ColorScale.ScaleWithColor(op.fg)
		text.Draw(img, op.s, op.face, dop)
	}
	draw(0)
	if op.st&StyleBold != 0 {
		dx := 1.0
		if op.fontSize >= 40 {
			dx = 2
		}
		draw(dx)
	}
	if op.st&StyleUnderline != 0 {
		w, _ := text.Measure(op.s, op.face, op.lineH)
		ascent := op.face.Metrics().HAscent
		descent := op.face.Metrics().HDescent
		th := float32(1)
		if op.fontSize >= 32 {
			th = 2
		}
		y := float32(baseY + ascent + descent*0.5) // baseline + half descent
		vector.StrokeLine(img, float32(baseX), y, float32(baseX+w), y, th, op.fg, false)
	}
}

// caretPixels returns the focused inputbox caret in device pixels,
// measured with the current face (proportional fonts drift from the
// cell grid, so cell math misplaces the IME composition). ok is
// false with no focused editor.
func (b *WindowBackend) caretPixels() (x, y float64, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.face == nil {
		return 0, 0, false
	}
	if ed := b.focusedLocked(); ed != nil {
		// The caret rides past the in-conversion text so it (and the
		// candidate window anchored here) tracks what is typed.
		w, _ := text.Measure(string(ed.text[:ed.caret])+b.imeComposing, b.face, 0)
		return float64(ed.rect.Min.X) + 4 + w - float64(ed.scroll),
			float64(ed.rect.Min.Y), true
	}
	return 0, 0, false
}

// ReadLine is a Backend-interface stub: the GUI has no blocking line
// editor. GUI input() is immediate (see ReadImmediate); script text
// entry uses inputbox widgets. Always reports EOF so any accidental
// use ends promptly instead of hanging.
//
// IME: focus an inputbox (or call ime(1)) to enable conversion input
// (Windows/macOS/Web); committed text lands in the box or the
// input() stream, imeget() reports the in-conversion string.
func (b *WindowBackend) ReadLine(prompt string) (string, bool) {
	_ = prompt
	return "", false
}

// Clear wipes the current canvas buffer (text is baked into it, like any
// other drawing, so it goes together) and all widgets (clrobj without args).
// Canvas/widget removal runs on the game thread via enqueue, so after the
// first yield it is staged in pending and applied together with the rest of
// the frame at await()/sleep()/end (no one-frame flicker).
func (b *WindowBackend) Clear() {
	b.mu.Lock()
	b.curX, b.curY = 0, 0
	b.scrollExact, b.scrollDone = 0, 0
	sel := b.sel
	b.mu.Unlock()
	b.enqueue(func() {
		b.ensureTarget(sel).Clear()
		b.mu.Lock()
		ids := make([]int, 0, len(b.widgets))
		for id := range b.widgets {
			ids = append(ids, id)
		}
		b.mu.Unlock()
		for _, id := range ids {
			_ = b.removeWidgetLocked(id)
		}
	})
}

// SetColor sets subsequent text/draw color (alpha included).
func (b *WindowBackend) SetColor(r, g, bl, a int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fg = color.NRGBA{R: uint8(clamp(r)), G: uint8(clamp(g)), B: uint8(clamp(bl)), A: uint8(clamp(a))}
}

// ResetColor restores white text.
func (b *WindowBackend) ResetColor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fg = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
}

// IsFullscreen reports fullscreen state (script-thread safe).
func (b *WindowBackend) IsFullscreen() bool {
	return ebiten.IsFullscreen()
}

// Closing reports whether the user asked to close the window. The
// first call opts in: the close button stops terminating the loop so
// the script can save and call end() itself.
func (b *WindowBackend) Closing() bool {
	b.mu.Lock()
	armed := b.closingArmed
	b.mu.Unlock()
	if !armed {
		ebiten.SetWindowClosingHandled(true)
		b.mu.Lock()
		b.closingArmed = true
		b.mu.Unlock()
	}
	return ebiten.IsWindowBeingClosed()
}

// SetFullscreen toggles fullscreen on the game thread.
func (b *WindowBackend) SetFullscreen(on bool) {
	b.runOnLoop(func() {
		ebiten.SetFullscreen(on)
	})
}

// IsResizable reports whether the window can be dragged to resize.
func (b *WindowBackend) IsResizable() bool {
	return ebiten.IsWindowResizable()
}

// SetResizable allows (or forbids) drag-resizing the window.
// The canvas keeps its logical size (screen() controls that);
// a larger window scales the view.
func (b *WindowBackend) SetResizable(on bool) {
	ebiten.SetWindowResizable(on)
}

// ScreenSize reports fullscreen width, height and monitor count.
// Used for resolution-aware layouts; safe to call headless in tests.
func ScreenSize() (w, h, monitors int) {
	w, h = ebiten.ScreenSizeInFullscreen()
	return w, h, len(ebiten.AppendMonitors(nil))
}

// MoveWindow moves the window (concurrent-safe; desktop only).
func (b *WindowBackend) MoveWindow(x, y int) {
	ebiten.SetWindowPosition(x, y)
}

// SetTitle changes the window title.
func (b *WindowBackend) SetTitle(s string) {
	ebiten.SetWindowTitle(s)
}

// SetFontSize changes the text face size in pixels (8..64). Cell metrics
// scale with it so the mes/pos grid stays aligned; widgets follow through
// uiFace. Script-thread safe (face/buffer swaps are mutex-guarded).
func (b *WindowBackend) SetFontSize(size int) error {
	if size < 8 || size > 64 {
		return fmt.Errorf("font：サイズは 8 から 64 の範囲で指定してください。%d が指定されました", size)
	}
	face, err := loadFace(float64(size))
	if err != nil {
		return fmt.Errorf("font：%s", err.Error())
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.face = face
	b.fontSize = float64(size)
	b.charW = charWidth * float64(size) / fontSize
	b.lineH = lineHeight * float64(size) / fontSize
	return nil
}

// FontSize reports the current face size in pixels.
func (b *WindowBackend) FontSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int(b.fontSize + 0.5)
}

// FontFile reports the resolved font file in use ("" when the
// discovery default is active and unchanged).
func (b *WindowBackend) FontFile() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fontPath != "" {
		return b.fontPath
	}
	return FontPath()
}

// TextWidth measures s in pixels at the current face (whole string;
// editors typically pass a single line for caret placement).
func (b *WindowBackend) TextWidth(s string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.face == nil {
		return len([]rune(s))
	}
	w, _ := text.Measure(s, b.face, 0)
	return int(w + 0.5)
}

// cellWidth is the terminal-cell width of r: East Asian wide and
// fullwidth runes take 2 cells, everything else 1 (half-width
// katakana, ASCII, and controls keep the historical advance).
// Pure logic, unit-testable.
func cellWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0x303E, // CJK radicals/punctuation
		r >= 0x3041 && r <= 0x33FF, // Hiragana/Katakana/CJK compat
		r >= 0x3400 && r <= 0x4DBF, // CJK ext A
		r >= 0x4E00 && r <= 0x9FFF, // CJK unified
		r >= 0xA000 && r <= 0xA4CF, // Yi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compat ideographs
		r >= 0xFE30 && r <= 0xFE4F, // CJK compat forms
		r >= 0xFF00 && r <= 0xFF60, // fullwidth ASCII/variants
		r >= 0xFFE0 && r <= 0xFFE6, // fullwidth signs
		r >= 0x20000 && r <= 0x3FFFD: // CJK ext B and beyond
		return 2
	}
	return 1
}

// cellCount sums cellWidth over s (cursor advance for print()).
// Pure logic, unit-testable.
func cellCount(s string) int {
	n := 0
	for _, r := range s {
		n += cellWidth(r)
	}
	return n
}

// SetFontFile switches the typeface to spec (path or system font name)
// at the given pixel size (8..64), updating cell metrics like
// SetFontSize. Script-thread safe. extraDirs are searched before the
// system font directories (script directory, working directory).
func (b *WindowBackend) SetFontFile(spec string, size int, extraDirs ...string) error {
	if size < 8 || size > 64 {
		return fmt.Errorf("font：サイズは 8 から 64 の範囲で指定してください。%d が指定されました", size)
	}
	src, path, err := loadFontSpec(spec, extraDirs...)
	if err != nil {
		return err
	}
	face := &text.GoTextFace{Source: src, Size: float64(size)}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.face = face
	b.fontSize = float64(size)
	b.charW = charWidth * float64(size) / fontSize
	b.lineH = lineHeight * float64(size) / fontSize
	b.fontPath = path
	return nil
}

// MoveTo sets the cursor. In GUI it is pixel-based for the graphics
// cursor (gcopy etc); the text cursor (mes) is derived as the nearest
// character cell so existing line-based text still works. In CUI the
// console backend moves by character cells.
func (b *WindowBackend) MoveTo(x, y int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gx = x
	b.gy = y
	// Text grid stays cell-based: map pixel pos to cell for mes/print.
	// 負値も正しくセルへ丸めるため floor を用いる。
	if b.charW > 0 {
		b.curX = int(math.Floor(float64(x) / b.charW))
	} else {
		b.curX = x
	}
	if b.lineH > 0 {
		b.curY = int(math.Floor(float64(y) / b.lineH))
	} else {
		b.curY = y
	}
}

// Sleep pauses the script; the window keeps rendering.
// Like await() it is a frame boundary: the frame built so far (text and
// shapes alike, all staged canvas commands) is published first, so the
// paused frame is complete (no half-drawn content left over during pause).
func (b *WindowBackend) Sleep(ms int64) {
	b.publishAtYieldLocked()
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
