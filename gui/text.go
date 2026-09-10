//go:build gui

package gui

import (
	"fmt"
	"image/color"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// Out exposes os.Stdout for exec passthrough in GUI mode.
func (b *WindowBackend) Out() io.Writer {
	return os.Stdout
}

// Print appends text without a newline.
func (b *WindowBackend) Print(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.partial == "" {
		b.partX = b.curX
		b.partFg = b.fg
	}
	b.partial += s
	b.curX += len([]rune(s))
}

// Println appends text with a trailing newline.
func (b *WindowBackend) Println(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	firstFg := b.fg
	if b.partial != "" {
		firstFg = b.partFg
	}
	full := b.partial + s
	b.partial = ""
	lines := strings.Split(full, "\n")
	// First chunk completes the partial line.
	b.segs = append(b.segs, textSeg{x: b.partX, y: b.curY, s: lines[0], fg: firstFg})
	for _, ln := range lines[1:] {
		b.curY++
		b.segs = append(b.segs, textSeg{x: 0, y: b.curY, s: ln, fg: b.fg})
	}
	// Bound memory for chatty scripts; Draw only shows the tail anyway.
	if len(b.segs) > 10000 {
		b.segs = append([]textSeg(nil), b.segs[len(b.segs)-10000:]...)
	}
	b.curX = 0
	b.curY++
}

// ReadLine shows a prompt and blocks until Enter (script goroutine only).
// EOF (window closed mid-edit) yields ok=false.
//
// IME: call ime(1) before input() to enable conversion input
// (Windows/macOS/Web); committed text lands in the line, imeget()
// reports the in-conversion string. Otherwise only committed text
// arrives via AppendInputChars.
func (b *WindowBackend) ReadLine(prompt string) (string, bool) {
	req := &lineReq{prompt: prompt, done: make(chan string, 1)}
	b.mu.Lock()
	// The line editor takes the key stream: defocus boxes so
	// keystrokes cannot split between the box and the line.
	b.blurEditsLocked()
	// Flush pending partial text above the editor line.
	if b.partial != "" {
		b.segs = append(b.segs, textSeg{x: b.partX, y: b.curY, s: b.partial, fg: b.partFg})
		b.partial = ""
	}
	b.line = req
	b.mu.Unlock()
	s, ok := <-req.done
	if !ok {
		return "", false
	}
	return s, true
}

// Clear wipes the canvas and the text.
func (b *WindowBackend) Clear() {
	b.mu.Lock()
	b.segs = nil
	b.partial = ""
	b.curX, b.curY = 0, 0
	sel := b.sel
	b.mu.Unlock()
	b.enqueue(func() {
		b.ensureTarget(sel).Clear()
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

// SetFontFile switches the typeface to spec (path or system font name)
// at the given pixel size (8..64), updating cell metrics like
// SetFontSize. Script-thread safe.
func (b *WindowBackend) SetFontFile(spec string, size int) error {
	if size < 8 || size > 64 {
		return fmt.Errorf("font：サイズは 8 から 64 の範囲で指定してください。%d が指定されました", size)
	}
	src, path, err := loadFontSpec(spec)
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

// caretPixels returns the active editor caret in device pixels,
// measured with the current face (proportional fonts drift from the
// cell grid, so cell math misplaces the IME composition). The
// focused inputbox wins over the input() line; ok is false with no
// active editor.
func (b *WindowBackend) caretPixels() (x, y float64, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.face == nil {
		return 0, 0, false
	}
	if ed := b.focusedLocked(); ed != nil {
		w, _ := text.Measure(string(ed.text[:ed.caret]), b.face, 0)
		return float64(ed.rect.Min.X) + 4 + w - float64(ed.scroll),
			float64(ed.rect.Min.Y), true
	}
	if b.line == nil {
		return 0, 0, false
	}
	w, _ := text.Measure(b.line.prompt+string(b.line.buf), b.face, 0)
	return w, float64(b.curY) * b.lineH, true
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
func (b *WindowBackend) Sleep(ms int64) {
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
