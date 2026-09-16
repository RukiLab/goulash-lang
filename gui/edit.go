package gui

import (
	"fmt"
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Custom single-line editor (inputbox): self-drawn box with caret,
// horizontal scroll and OS IME conversion. It replaces the ebitenui
// TextInput so the caret position is known: the IME candidate window
// anchors at the real caret (via caretPixels) instead of (0,0).
//
// Focus model: left-click focuses one box (blurring the rest);
// clicking elsewhere blurs the box. The IME field itself is sticky:
// it stays focused until ime(0) or box blur via Enter/Escape, so
// conversion survives between keystrokes while polling input().
// While a box is focused it owns the key stream: input() reports ""
// and Update routes committed characters into the caret. Focusing a
// box also focuses the IME field (conversion works out of the box).
// ime(0) still force-disables conversion (the next focus re-enables).

// inputState is one editor. Guarded by backend mu; game-thread
// mutated in pumpInputs, script-thread read in InputText.
type inputState struct {
	rect    image.Rectangle
	text    []rune
	caret   int // rune index 0..len(text)
	focused bool
	scroll  int // x pixel offset of the visible text
	// held-key repeat (backspace/delete/arrows on the game thread).
	repKey  ebiten.Key
	repNext int64
}

// insert adds runes at the caret.
func (e *inputState) insert(rs []rune) {
	if len(rs) == 0 {
		return
	}
	e.text = append(e.text[:e.caret], append(rs, e.text[e.caret:]...)...)
	e.caret += len(rs)
}

// backspace deletes before the caret; del deletes at it.
func (e *inputState) backspace() {
	if e.caret > 0 {
		e.text = append(e.text[:e.caret-1], e.text[e.caret:]...)
		e.caret--
	}
}

func (e *inputState) del() {
	if e.caret < len(e.text) {
		e.text = append(e.text[:e.caret], e.text[e.caret+1:]...)
	}
}

func (e *inputState) moveLeft() {
	if e.caret > 0 {
		e.caret--
	}
}

func (e *inputState) moveRight() {
	if e.caret < len(e.text) {
		e.caret++
	}
}

// repFire gates a held editing key: just-pressed fires at once and
// arms the repeat; a different key takes over; release disarms.
// Pure logic, unit-testable.
func (e *inputState) repFire(key ebiten.Key, just, held bool, now int64) bool {
	if !held {
		if e.repKey == key {
			e.repKey = 0
		}
		return false
	}
	if just || e.repKey != key {
		e.repKey, e.repNext = key, now+repDelay
		return true
	}
	if now >= e.repNext {
		e.repNext = now + repInterval
		return true
	}
	return false
}

// editAction is one editing-key effect, applied by pumpInputs.
func (e *inputState) editAction(key ebiten.Key) {
	switch key {
	case ebiten.KeyBackspace:
		e.backspace()
	case ebiten.KeyDelete:
		e.del()
	case ebiten.KeyArrowLeft:
		e.moveLeft()
	case ebiten.KeyArrowRight:
		e.moveRight()
	case ebiten.KeyHome:
		e.caret = 0
	case ebiten.KeyEnd:
		e.caret = len(e.text)
	}
}

// focusedLocked returns the focused editor; caller holds mu.
func (b *WindowBackend) focusedLocked() *inputState {
	for _, e := range b.widgets {
		if e.kind == wInput && e.edit != nil && e.edit.focused {
			return e.edit
		}
	}
	return nil
}

// AddInput creates (or replaces) a text input with initial text
// (caret at the end). Custom-drawn: no ebitenui child.
func (b *WindowBackend) AddInput(id, x, y, w, h int, text string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("inputbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	rs := []rune(text)
	st := &inputState{rect: image.Rect(x, y, x+w, y+h), text: rs, caret: len(rs)}
	b.runOnLoop(func() {
		b.placeCustom(id, &widgetEntry{id: id, kind: wInput, selIdx: -1, edit: st})
	})
	return nil
}

// InputText returns the current input content (script-thread safe,
// never blocks: the state itself is mu-guarded).
func (b *WindowBackend) InputText(id int) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wInput || e.edit == nil {
		return "", fmt.Errorf("gettext：不明な inputbox %d です", id)
	}
	return string(e.edit.text), nil
}

// blurEditsLocked defocuses every editor; caller holds mu.
func (b *WindowBackend) blurEditsLocked() {
	for _, e := range b.widgets {
		if e.kind == wInput && e.edit != nil && e.edit.focused {
			e.edit.focused = false
			e.edit.repKey = 0
		}
	}
}

// pumpInputs handles focus and editing on the game thread (called
// from Update after pumpIME). Clicks are observed, not consumed.
func (b *WindowBackend) pumpInputs() {
	x, y := ebiten.CursorPosition()
	clicked := false
	b.mu.Lock()
	if b.clickFlag[0] {
		clicked = true
	}
	b.mu.Unlock()

	b.mu.Lock()
	if b.widgets != nil && clicked {
		pt := image.Pt(x, y)
		hit := false
		for _, e := range b.widgets {
			if e.kind != wInput || e.edit == nil || e.disabled {
				continue
			}
			if pt.In(e.edit.rect) {
				hit = true
				if !e.edit.focused {
					b.blurEditsLocked()
					e.edit.focused = true
					e.edit.repKey = 0
				}
				break
			}
		}
		if !hit {
			b.blurEditsLocked()
		}
	}
	ed := b.focusedLocked()
	b.mu.Unlock()

	// The IME field follows box focus so conversion works without
	// an explicit ime(1) (game-thread only). Focus is otherwise
	// sticky: immediate-mode scripts poll input() with no box
	// focused, and an idle auto-blur would kill conversion between
	// keystrokes. Blur only via ime(0) or box defocus. Without a
	// focused box the candidate window falls back to the origin
	// (there is no caret to anchor it to).

	if ed == nil {
		return
	}
	now := b.Tick()
	// Committed characters land at the caret. While the IME field is
	// focused they arrive via pumpIME; otherwise plain.
	b.mu.Lock()
	imeOn := b.imeField.IsFocused()
	comp := b.imeComposing
	pending := b.imePending
	b.imePending = ""
	b.mu.Unlock()
	// ed は game thread の pumpInputs/drawInputs と script の InputText で
	// 共有されるため、変異は mu 保持中に行う (保持なしの insert は
	// Draw スナップショットと競合し、入力文字のちらつき・欠落になる)。
	if imeOn {
		if len(pending) > 0 {
			b.mu.Lock()
			ed.insert([]rune(pending))
			b.mu.Unlock()
		}
	} else {
		if chars := ebiten.AppendInputChars(nil); len(chars) > 0 {
			b.mu.Lock()
			ed.insert(chars)
			b.mu.Unlock()
		}
	}
	// While converting, Enter/Escape belong to the IME (they commit
	// or cancel the composition); otherwise they blur the box. While
	// the field is focused it also owns the editing keys below, or
	// keystrokes would apply twice (field and box).
	if !imeOn || comp == "" {
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyKPEnter) ||
			inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			b.mu.Lock()
			b.blurEditsLocked()
			// Drop field residue like the line editor does on
			// confirm: the entry is finished.
			b.imePending = ""
			b.imePrev = ""
			b.imeComposing = ""
			b.mu.Unlock()
			b.imeField.SetTextAndSelection("", 0, 0)
			b.imeField.Blur()
			return
		}
	}
	if imeOn {
		return
	}
	// Editing keys (game thread: inpututil is safe here).
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, k := range []ebiten.Key{
		ebiten.KeyBackspace, ebiten.KeyDelete,
		ebiten.KeyArrowLeft, ebiten.KeyArrowRight,
	} {
		if ed.repFire(k, inpututil.IsKeyJustPressed(k), ebiten.IsKeyPressed(k), now) {
			ed.editAction(k)
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyHome) {
		ed.caret = 0
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
		ed.caret = len(ed.text)
	}
}

// drawInputs paints custom editors above the UI (game thread; called
// from Draw). Text is clipped to the box via a sub-image viewport.
func (b *WindowBackend) drawInputs(screen *ebiten.Image) {
	type snap struct {
		id       int
		st       inputState
		disabled bool
		face     *text.GoTextFace
		ascent   float64
		lineH    float64
		comp     string
		tick     int64
	}
	b.mu.Lock()
	var ss []snap
	for id, e := range b.widgets {
		if e.kind != wInput || e.edit == nil {
			continue
		}
		s := snap{id: id, st: *e.edit, disabled: e.disabled, face: b.face, comp: b.imeComposing, tick: b.tick}
		if b.face != nil {
			s.ascent = b.face.Metrics().HAscent
			s.lineH = b.lineH
		}
		ss = append(ss, s)
	}
	b.mu.Unlock()
	for i := range ss {
		drawInput(screen, &ss[i].st, ss[i].disabled, ss[i].face, ss[i].ascent, ss[i].comp, ss[i].tick)
	}
	// scroll は Draw で確定するが、コピーへの調整を捨てると毎フレーム
	// 0 から再計算して行端で前後振動 (ちらつき) するため書き戻す。
	b.mu.Lock()
	for _, s := range ss {
		if e, ok := b.widgets[s.id]; ok && e.edit != nil {
			e.edit.scroll = s.st.scroll
		}
	}
	b.mu.Unlock()
}

// drawInput paints one editor: frame, clipped text, caret, IME
// composition. Pure drawing (no backend), unit-testable where noted.
func drawInput(screen *ebiten.Image, st *inputState, disabled bool, face *text.GoTextFace, ascent float64, comp string, tick int64) {
	r := st.rect
	bg := color.NRGBA{0x22, 0x26, 0x2E, 0xFF}
	frame := color.NRGBA{0x55, 0x5B, 0x68, 0xFF}
	if st.focused {
		frame = color.NRGBA{0x6A, 0x9A, 0xE8, 0xFF}
	}
	if disabled {
		bg = color.NRGBA{0x1A, 0x1D, 0x24, 0xFF}
		frame = color.NRGBA{0x33, 0x39, 0x45, 0xFF}
	}
	vector.DrawFilledRect(screen, float32(r.Min.X), float32(r.Min.Y), float32(r.Dx()), float32(r.Dy()), bg, false)
	vector.StrokeRect(screen, float32(r.Min.X)+0.5, float32(r.Min.Y)+0.5, float32(r.Dx())-1, float32(r.Dy())-1, 1, frame, false)
	if face == nil || r.Dy() < 8 || r.Dx() <= 12 {
		return
	}
	inner := image.Rect(r.Min.X+4, r.Min.Y+2, r.Max.X-4, r.Max.Y-2)
	if inner.Dx() <= 0 || inner.Dy() <= 0 {
		return
	}
	caretX := measureInput(face, st.text[:st.caret])
	if st.focused {
		// The caret rides past the in-conversion text while typing.
		caretX += measureInput(face, []rune(comp))
	}
	visX := caretX - float64(st.scroll)
	// Keep the caret visible.
	if visX > float64(inner.Dx()) {
		st.scroll = int(caretX) - inner.Dx()
		visX = caretX - float64(st.scroll)
	}
	if visX < 0 {
		st.scroll = int(caretX)
		visX = caretX - float64(st.scroll)
	}
	if st.scroll < 0 {
		st.scroll = 0
	}
	dst, ok := screen.SubImage(inner).(*ebiten.Image)
	if !ok {
		return
	}
	fg := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	if disabled {
		fg = color.NRGBA{0x77, 0x77, 0x77, 0xFF}
	}
	op := &text.DrawOptions{}
	op.GeoM.Translate(float64(inner.Min.X)-float64(st.scroll), float64(inner.Min.Y)+ascent)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(fg)
	text.Draw(dst, string(st.text), face, op)
	// In-conversion text follows the caret in gray.
	if comp != "" && st.focused {
		cop := &text.DrawOptions{}
		cop.GeoM.Translate(float64(inner.Min.X)+visX, float64(inner.Min.Y)+ascent)
		cop.ColorScale.Reset()
		cop.ColorScale.ScaleWithColor(color.NRGBA{0x99, 0x99, 0x99, 0xFF})
		text.Draw(dst, comp, face, cop)
	}
	// Caret blink (~2Hz).
	if st.focused && !disabled && tick/30%2 == 0 {
		cx := float32(inner.Min.X) + float32(visX)
		if cx >= float32(inner.Min.X) && cx <= float32(inner.Max.X) {
			vector.DrawFilledRect(screen, cx, float32(inner.Min.Y), 2, float32(inner.Dy()),
				color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}, false)
		}
	}
}

// measureInput widths a rune prefix at face (caret placement).
// Pure, unit-testable.
func measureInput(face *text.GoTextFace, prefix []rune) float64 {
	if face == nil || len(prefix) == 0 {
		return 0
	}
	w, _ := text.Measure(string(prefix), face, 0)
	return w
}
