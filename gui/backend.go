// Package gui implements the Ebiten WindowBackend (G0: window, text,
// close handling). Drawing (G1), polling input/audio/dialog (G2) and
// widgets (G3) extend this file set.
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
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	fontSize   = 16.0
	lineHeight = 20.0
	charWidth  = 8.0
	// italicShear slants faux-italic glyphs (~11 degrees).
	italicShear = 0.2
)

// TextStyle is a bitmask of faux text decorations for mes()/print().
// Styles deform the single loaded face (no variant files needed):
// bold over-strikes, italic shears, underline rules below the baseline.
type TextStyle int

const (
	StyleBold TextStyle = 1 << iota
	StyleItalic
	StyleUnderline
)

// ParseTextStyle maps a mes()/print() trailing keyword to a style flag.
// Exact lowercase match only, so ordinary words never vanish from output.
func ParseTextStyle(s string) (TextStyle, bool) {
	switch s {
	case "bold":
		return StyleBold, true
	case "italic":
		return StyleItalic, true
	case "bolditalic":
		return StyleBold | StyleItalic, true
	case "underline":
		return StyleUnderline, true
	}
	return 0, false
}

// textSeg is one laid-out text fragment.
type textSeg struct {
	x, y int // character-cell origin
	s    string
	fg   color.NRGBA
	st   TextStyle
}

// WindowBackend is a Backend rendering into an Ebiten window.
// All state is guarded by mu; the Ebiten loop and the script goroutine
// share it. Canvas images are touched only on the game thread: the
// script enqueues draw commands processed by Update.
type WindowBackend struct {
	mu      sync.Mutex
	w, h    int
	segs    []textSeg
	partial string // unflushed print() text
	partX   int    // cell x where partial started
	partFg  color.NRGBA
	partSt  TextStyle // style at partial start
	curX    int
	curY    int
	// draw* は Draw が読むコミット済みスナップショット。Draw は
	// script 側の編集中状態 (segs/partial/curY/face...) を一切見ない。
	// Print/Println/Clear/MoveTo は編集中状態だけを即時更新し、
	// await()/sleep() (フレーム境界) で publishTextLocked が一括公開する。
	// これで cls()->…描画…->mes() の途中を Draw が観測できず、
	// 空テキストや旧テキストの1フレーム混入 (ちらつき) が出ない。
	drawSegs    []textSeg
	drawPartial string
	drawPartX   int
	drawPartFg  color.NRGBA
	drawPartSt  TextStyle
	drawCurY    int
	drawFace    *text.GoTextFace
	drawCharW   float64
	drawLineH   float64
	drawFontSz  float64
	drawH       int
	// yieldedOnce: the script has parked in await()/sleep() at least once.
	// From then on the text snapshot is published only at yield points
	// (await/sleep/end) instead of every Update, so a frame reaches the
	// screen only after the script finished building it (see
	// publishAtYieldLocked).
	yieldedOnce bool

	gx, gy int // pixel cursor for graphics (gcopy destination)
	fg     color.NRGBA
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
	// pending はフレーム構築中の canvas コマンド置き場。yieldedOnce 以後は
	// enqueue がここへ溜め、await()/sleep()/end のフレーム境界で queue へ
	// 一括移動する。これで cls() 直後の空 canvas や描きかけが1フレーム
	// 表示される撕裂 (ちらつき) が出ない。await 前のスクリプトは高速に
	// 全コマンドを積み、次の Update で原子的に適用される。
	pending []func()
	// drawSel は Draw が読む公開済み可視バッファ。gsel() の途中切替が
	// そのまま画面に出て裏バッファの描きかけが見えるのを防ぐ。
	drawSel int
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
	// Immediate-input repeat state (input(); guarded by mu).
	charSt charRep
	ctrlSt []ctrlState // one slot per ctrlKeys entry
	// IME state (ime/imeget; Field is pumped on the game thread only,
	// mirrors are guarded by mu for script-side reads).
	imeField   textinput.Field
	imePrev    string // previous tick's committed field text (diff base)
	imePending string // committed text awaiting input()
	// imeComposing is the uncommitted (conversion) text mirror.
	imeComposing string
	// imeAnchor is a manual candidate-window anchor (imepos; window
	// pixels, same system as inputbox x/y). When set it wins over
	// the focused inputbox caret in pumpIME; otherwise the caret
	// (or the (0,0) fallback with no focused editor) is used.
	imeAnchorSet bool
	imeAnchorX   int
	imeAnchorY   int
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
	b := &WindowBackend{
		w: w, h: h,
		ctrlSt:   make([]ctrlState, len(ctrlKeys)),
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
	}
	// 初期テキストも Draw に公開しておく (空スナップ)。
	b.mu.Lock()
	b.publishTextLocked()
	b.mu.Unlock()
	return b, nil
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
// The final frame is published so the finished output stays on screen.
func (b *WindowBackend) SetDone() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantDone = true
	b.publishTextLocked()
	b.yieldedOnce = true
}

// RequestClose asks the game loop to terminate (end() builtin).
// The last frame is published before the window goes away.
func (b *WindowBackend) RequestClose() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wantQuit = true
	b.publishTextLocked()
	b.yieldedOnce = true
}

// publishAtYieldLocked publishes the text snapshot at a script yield
// point (await()/sleep()) and switches Update off from publishing.
// Caller must NOT hold mu.
func (b *WindowBackend) publishAtYieldLocked() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.publishTextLocked()
	b.yieldedOnce = true
}

// Closed reports whether the loop should exit.
func (b *WindowBackend) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wantQuit
}

// publishTextLocked copies the script-side text into the Draw snapshot.
// Caller must hold mu.
// Draw reads only the snapshot, so the working state of a cls()/mes()
// sequence in progress is never rendered: a frame is committed either at
// a script yield point (Await/Sleep, i.e. once the frame is complete) or
// by Update while the script has not yielded yet.
// Canvas も同時に確定する: pending の描画コマンドを queue へ一括移動し、
// 可視バッファ sel を drawSel へ snapshot する。Draw は drawSel の完成
// バッファだけを見るので、cls() 直後の空 canvas や gsel() 途中の裏面が
// 1フレーム混入しない (GUI部品・図形のちらつき防止)。
func (b *WindowBackend) publishTextLocked() {
	if len(b.segs) == 0 {
		b.drawSegs = nil
	} else {
		snap := make([]textSeg, len(b.segs))
		copy(snap, b.segs)
		b.drawSegs = snap
	}
	b.drawPartial = b.partial
	b.drawPartX = b.partX
	b.drawPartFg = b.partFg
	b.drawPartSt = b.partSt
	b.drawCurY = b.curY
	b.drawFace = b.face
	b.drawCharW = b.charW
	b.drawLineH = b.lineH
	b.drawFontSz = b.fontSize
	b.drawH = b.h
	if len(b.pending) > 0 {
		b.queue = append(b.queue, b.pending...)
		b.pending = nil
	}
	b.drawSel = b.sel
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
// The call is the frame boundary: the text built since the previous
// await is published here, so Draw never observes a half-drawn frame
// (cls() 直後や mes() 前の状態が画面に出るちらつきの防止).
// await(0) therefore doubles as an explicit "show this frame now".
func (b *WindowBackend) Await(n int64) {
	b.publishAtYieldLocked()
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
	b.pumpInputs()
	// Root は window 全面。リサイズ反映は Update 側で行い、Draw 中の
	// SetLocation を避ける (Draw 中のレイアウト変更は ebitenui の描画を
	// 1フレーム乱し、GUI部品のちらつき・ずれに見えるため)。
	if b.ui != nil {
		b.mu.Lock()
		ww, hh := b.w, b.h
		needReloc := b.root != nil && (b.uiW != ww || b.uiH != hh)
		b.mu.Unlock()
		if needReloc {
			b.mu.Lock()
			b.uiW, b.uiH = ww, hh
			b.mu.Unlock()
			b.root.SetLocation(image.Rect(0, 0, ww, hh))
		}
		if b.fixLayout != nil {
			b.fixLayout.w, b.fixLayout.h = ww, hh
		}
		b.ui.Update()
	}
	b.syncWidgetCache()
	// テキスト公開は原則ここでは行わない (Draw は公開済みスナップのみ読む)。
	// await()/sleep() を一度も使わないスクリプトはフレーム境界が無いので、
	// その場合だけ Update で公開して文字がいつまでも出ないのを防ぐ。
	b.mu.Lock()
	if !b.yieldedOnce {
		b.publishTextLocked()
	}
	b.mu.Unlock()
	b.advanceTick()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wantQuit {
		return ebiten.Termination
	}
	return nil
}

// textRowTop returns the pixel top of text row cy (scroll-adjusted by
// minY). text/v2 puts the rendering region's top at the GeoM origin,
// so no ascent offset is added here; the baseline sits ascent lower
// (used for the underline rule). Pure math, unit-testable.
func textRowTop(cy, minY int, lineH float64) float64 {
	return float64(cy-minY) * lineH
}

// Draw renders buffered text and widgets.
func (b *WindowBackend) Draw(screen *ebiten.Image) {
	screen.Fill(color.Black)
	// Visible canvas: the committed buffer (drawSel). gsel() の途中切替が
	// そのまま出ると裏バッファの描きかけが1フレーム見えるため、frame 境界で
	// 公開された面だけを描く。targets 自体は game thread のみが触る。
	b.mu.Lock()
	sel := b.drawSel
	b.mu.Unlock()
	if img := b.targets[sel]; img != nil {
		screen.DrawImage(img, nil)
	}
	// テキストは公開済みスナップ (draw*) だけを読む。script 側で編集中の
	// segs/partial/curY を直接読むと、cls() 直後の空状態や mes() 前の
	// 途中状態が1フレーム見えてちらつく (フレーム撕裂) ため統一する。
	// b.h の非ロック読みも同時に解消する。
	b.mu.Lock()
	segs := b.drawSegs
	partial, partX, partFg, partSt := b.drawPartial, b.drawPartX, b.drawPartFg, b.drawPartSt
	curY := b.drawCurY
	face := b.drawFace
	charW, lineH := b.drawCharW, b.drawLineH
	fontSize := b.drawFontSz
	winH := b.drawH
	b.mu.Unlock()
	// メトリクスが未公開ならテキストだけ省く (canvas と widgets は描く)。
	ascent, descent := 0.0, 0.0
	if face == nil || charW <= 0 || lineH <= 0 {
		segs, partial = nil, ""
	} else {
		ascent = face.Metrics().HAscent
		descent = face.Metrics().HDescent
	}

	// Faux-style tuning (single face deformed): bold over-strikes with a
	// size-scaled shift, italic shears ~11 degrees, underline rules below
	// the baseline.
	boldDx := 1.0
	underThick := float32(1)
	if fontSize >= 40 {
		boldDx = 2
	}
	if fontSize >= 32 {
		underThick = 2
	}
	rows := 0
	if lineH > 0 {
		rows = int(float64(winH) / lineH)
	}
	// Vertical scroll: drop lines above the visible window.
	minY := 0
	maxY := curY
	if maxY-minY >= rows {
		minY = maxY - rows + 1
	}
	drawText := func(cx int, cy int, s string, fg color.NRGBA, st TextStyle) {
		if cy < minY || s == "" {
			return
		}
		baseX := float64(cx) * charW
		baseY := textRowTop(cy, minY, lineH)
		italic := st&StyleItalic != 0
		draw := func(dx float64) {
			op := &text.DrawOptions{}
			// Region top lands on the origin; glyphs hang below it.
			op.GeoM.Translate(baseX+dx, baseY)
			if italic {
				// Element (0,1) is b in x' = a*x + b*y + tx:
				// slant glyphs right around the pen point.
				op.GeoM.SetElement(0, 1, italicShear)
			}
			// ColorScale zero value is transparent; reset to identity first.
			op.ColorScale.Reset()
			op.ColorScale.ScaleWithColor(fg)
			text.Draw(screen, s, face, op)
		}
		draw(0)
		if st&StyleBold != 0 {
			draw(boldDx)
		}
		if st&StyleUnderline != 0 {
			w, _ := text.Measure(s, face, lineH)
			y := float32(baseY + ascent + descent*0.5) // baseline + half descent
			vector.StrokeLine(screen, float32(baseX), y, float32(baseX+w), y, underThick, fg, false)
		}
	}
	for _, sg := range segs {
		drawText(sg.x, sg.y, sg.s, sg.fg, sg.st)
	}
	if partial != "" {
		drawText(partX, curY, partial, partFg, partSt)
	}
	if b.ui != nil {
		b.ui.Draw(screen)
	}
	b.drawInputs(screen)
}

// Layout fixes the logical screen size.
func (b *WindowBackend) Layout(outsideWidth, outsideHeight int) (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.w, b.h
}
