//go:build gui

// Package gui implements the Ebiten WindowBackend (G0: window, text,
// line input, close handling). Drawing (G1), polling input/audio/dialog
// (G2) and widgets (G3) extend this file set.
package gui

import (
	"errors"
	"image"
	"image/color"
	"io/fs"
	"sync"

	"github.com/ebitenui/ebitenui"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/exp/textinput"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	fontSize   = 16.0
	lineHeight = 20.0
	charWidth  = 8.0
)

// textSeg is one laid-out text fragment.
type textSeg struct {
	x, y int // character-cell origin
	s    string
	fg   color.NRGBA
}

// lineReq is an in-progress ReadLine request (script goroutine waits).
type lineReq struct {
	prompt string
	buf    []rune
	done   chan string
}

// WindowBackend is a Backend rendering into an Ebiten window.
// All state is guarded by mu; the Ebiten loop and the script goroutine
// share it. Blocking calls (ReadLine) must come from the script goroutine.
// Canvas images are touched only on the game thread: the script enqueues
// draw commands processed by Update.
type WindowBackend struct {
	mu      sync.Mutex
	w, h    int
	segs    []textSeg
	partial string // unflushed print() text
	partX   int    // cell x where partial started
	partFg  color.NRGBA
	curX    int
	curY    int
	gx, gy  int // pixel cursor for graphics (gcopy destination)
	fg      color.NRGBA
	line    *lineReq
	// Font state (guarded by mu): face plus the cell metrics derived
	// from it, so the mes/pos grid stays aligned at any size.
	face     *text.GoTextFace
	fontSize float64
	charW    float64
	lineH    float64
	fontPath string // resolved font file in use ("": discovery default)
	wantDone bool   // script finished; keep window open
	wantQuit bool   // end() called; close the window
	// Canvas state (images only on the game thread).
	targets map[int]*ebiten.Image // draw buffers by id; 0 is main
	bufs    map[int]bool          // allocated ids (guarded by mu)
	sel     int                   // current draw target (guarded by mu)
	blend   ebiten.CompositeMode
	queue   []func()
	// Input latch + audio (G2). Mouse buttons are indexed 0 left,
	// 1 middle, 2 right (clicked() numbering).
	mouseDown  [3]bool
	clickFlag  [3]bool
	wheelAccum float64 // unconsumed vertical wheel detents
	audio      *audioState
	// Global drawing alpha 0-255 (galpha; shapes and blits).
	alpha uint8
	// Frame counter for await()/tick() (guarded by mu).
	tick     int64
	tickWait []chan struct{}
	// closingArmed enables close-button handling (closing(); guarded).
	closingArmed bool
	// Dropped files (dropfiles/dropload; guarded by mu).
	dropNames []string
	dropFS    fs.FS // last snapshot (kept for dropload)
	// Keychar repeat state (keychar(); guarded by mu).
	charSt charRep
	// IME state (ime/imeget; Field is pumped on the game thread only,
	// mirrors are guarded by mu for script-side reads).
	imeField     textinput.Field
	imeSeen      int    // committed bytes consumed from the field
	imePending   string // committed text awaiting the line editor
	imeComposing string // uncommitted (conversion) text mirror
	// Retained widgets (G3, game thread only except where noted).
	ui        *ebitenui.UI
	root      *widget.Container
	fixLayout *fixedLayout
	widgets   map[int]*widgetEntry
	uiW, uiH  int // root location size applied
}

// New creates a window backend of w x h logical pixels.
func New(w, h int) (*WindowBackend, error) {
	face, err := loadFace(fontSize)
	if err != nil {
		return nil, err
	}
	return &WindowBackend{
		w: w, h: h,
		fg:       color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF},
		face:     face,
		fontSize: fontSize,
		charW:    charWidth,
		lineH:    lineHeight,
		alpha:    0xFF,
		// Image maps must exist before the first image op: PicLoad
		// and DropLoad write targets without going through
		// ensureTarget, so lazy init would panic on nil map.
		targets: make(map[int]*ebiten.Image),
		bufs:    make(map[int]bool),
	}, nil
}

// RunLoop runs run() in a goroutine and the Ebiten loop on this thread.
// It returns when the window closes or the script calls end().
func RunLoop(be *WindowBackend, run func()) int {
	ebiten.SetWindowSize(be.w, be.h)
	ebiten.SetWindowTitle("Goulash")
	ebiten.SetVsyncEnabled(true)
	go run()
	if err := ebiten.RunGame(be); err != nil && !errors.Is(err, ebiten.Termination) {
		return 1
	}
	return 0
}

// SetDone marks natural script completion (window stays open).
func (b *WindowBackend) SetDone() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantDone = true
}

// RequestClose asks the game loop to terminate (end() builtin).
func (b *WindowBackend) RequestClose() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantQuit = true
}

// Closed reports whether the loop should exit.
func (b *WindowBackend) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wantQuit
}

// advanceTick bumps the frame counter and wakes await() callers.
// Called from Update (game thread); tests drive it directly.
func (b *WindowBackend) advanceTick() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tick++
	for _, ch := range b.tickWait {
		close(ch)
	}
	b.tickWait = nil
}

// Tick is the Update counter (script-thread safe).
func (b *WindowBackend) Tick() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tick
}

// Await blocks until n frames pass. Must come from the script goroutine,
// never the game thread (Update could not run to wake it).
func (b *WindowBackend) Await(n int64) {
	if n <= 0 {
		return
	}
	for {
		b.mu.Lock()
		target := b.tick + n
		// Re-check under the lock: ticks may have passed already.
		if b.tick >= target {
			b.mu.Unlock()
			return
		}
		ch := make(chan struct{})
		b.tickWait = append(b.tickWait, ch)
		b.mu.Unlock()
		<-ch
		n = target - b.Tick()
		if n <= 0 {
			return
		}
	}
}

// --- ebiten.Game ---

// Update pumps input, applies queued draw commands, updates widgets.
func (b *WindowBackend) Update() error {
	b.drainQueue()
	b.pumpInput()
	b.pumpDrops()
	b.pumpIME()
	if b.ui != nil {
		b.ui.Update()
	}
	b.syncWidgetCache()
	b.advanceTick()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wantQuit {
		return ebiten.Termination
	}
	if b.line != nil {
		// While the IME field is focused, committed text arrives via
		// pumpIME (textinput owns the key stream); otherwise use the
		// plain committed characters.
		if b.imeField.IsFocused() {
			if b.imePending != "" {
				b.line.buf = append(b.line.buf, []rune(b.imePending)...)
				b.imePending = ""
			}
		} else {
			b.line.buf = append(b.line.buf, ebiten.AppendInputChars(nil)...)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyKPEnter) {
			req := b.line
			b.line = nil
			// Advance past the editor line so later output continues below.
			b.curX = 0
			b.curY++
			req.done <- string(req.buf)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(b.line.buf) > 0 {
			b.line.buf = b.line.buf[:len(b.line.buf)-1]
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			b.line.buf = b.line.buf[:0]
		}
	}
	return nil
}

// Draw renders buffered text and the active line editor.
func (b *WindowBackend) Draw(screen *ebiten.Image) {
	screen.Fill(color.Black)
	// Visible canvas: the currently selected buffer (0 = main).
	b.mu.Lock()
	sel := b.sel
	b.mu.Unlock()
	if img := b.targets[sel]; img != nil {
		screen.DrawImage(img, nil)
	}
	b.mu.Lock()
	segs := make([]textSeg, len(b.segs))
	copy(segs, b.segs)
	partial, partX, partFg := b.partial, b.partX, b.partFg
	curY := b.curY
	var editor *lineReq
	var edBuf string
	comp := b.imeComposing
	if b.line != nil {
		editor = b.line
		edBuf = string(b.line.buf)
	}
	face := b.face
	charW, lineH := b.charW, b.lineH
	ascent := face.Metrics().HAscent
	// Caret x in pixels (measured: proportional fonts drift from cells).
	caretX := 0.0
	if editor != nil {
		if w, _ := text.Measure(editor.prompt+edBuf, face, 0); w > 0 {
			caretX = w
		}
	}
	b.mu.Unlock()

	rows := int(float64(b.h) / lineH)
	// Vertical scroll: drop lines above the visible window.
	minY := 0
	maxY := curY
	if editor != nil {
		maxY = curY + 1
	}
	if maxY-minY >= rows {
		minY = maxY - rows + 1
	}
	drawText := func(cx int, cy int, s string, fg color.NRGBA) {
		if cy < minY || s == "" {
			return
		}
		op := &text.DrawOptions{}
		// text/v2 draws from the baseline, so shift down by the ascent.
		op.GeoM.Translate(float64(cx)*charW, float64(cy-minY)*lineH+ascent)
		// ColorScale zero value is transparent; reset to identity first.
		op.ColorScale.Reset()
		op.ColorScale.ScaleWithColor(fg)
		text.Draw(screen, s, face, op)
	}
	for _, sg := range segs {
		drawText(sg.x, sg.y, sg.s, sg.fg)
	}
	if partial != "" {
		drawText(partX, curY, partial, partFg)
	}
	if editor != nil {
		drawText(0, curY, editor.prompt+edBuf, color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
		// In-conversion IME text follows the caret in gray, measured
		// in pixels (cell math drifts on proportional fonts).
		if comp != "" && curY >= minY {
			op := &text.DrawOptions{}
			op.GeoM.Translate(caretX, float64(curY-minY)*lineH+ascent)
			op.ColorScale.Reset()
			op.ColorScale.ScaleWithColor(color.NRGBA{0x99, 0x99, 0x99, 0xFF})
			text.Draw(screen, comp, face, op)
		}
	}
	if b.ui != nil {
		// Root fills the window; relocate on resize.
		b.mu.Lock()
		ww, hh := b.w, b.h
		b.mu.Unlock()
		if b.root != nil && (b.uiW != ww || b.uiH != hh) {
			b.uiW, b.uiH = ww, hh
			b.root.SetLocation(image.Rect(0, 0, ww, hh))
		}
		if b.fixLayout != nil {
			b.fixLayout.w, b.fixLayout.h = ww, hh
		}
		b.ui.Draw(screen)
	}
}

// Layout fixes the logical screen size.
func (b *WindowBackend) Layout(outsideWidth, outsideHeight int) (int, int) {
	return b.w, b.h
}
