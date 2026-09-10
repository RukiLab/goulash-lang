//go:build gui

package gui

import (
	"image"
	"unicode/utf8"
)

// IME via ebiten/exp/textinput (Windows/macOS/Web; other platforms get
// committed text only through AppendInputChars as before).
//
// One backend-wide Field: ime(1) focuses it, ime(0) blurs it. While
// focused, Update pumps it every tick; committed text flows into the
// input() line editor, and the in-conversion string is mirrored for
// imeget(). Script-side reads use the mu-guarded mirrors because the
// Field itself is only safe on the game thread.

// IMEState reports 1 when the IME field is focused, else 0.
func (b *WindowBackend) IMEState() int {
	if b.imeField.IsFocused() {
		return 1
	}
	return 0
}

// IMESet focuses (on != 0) or blurs the IME field, returning the state.
func (b *WindowBackend) IMESet(on bool) int {
	if on {
		b.imeField.Focus()
	} else {
		b.imeField.Blur()
	}
	return b.IMEState()
}

// IMEComposition returns the in-conversion (uncommitted) text.
func (b *WindowBackend) IMEComposition() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.imeComposing
}

// pumpIME advances the IME field; game thread only (called from Update).
func (b *WindowBackend) pumpIME() {
	if !b.imeField.IsFocused() {
		return
	}
	// Anchor the composition/candidate window at the measured caret
	// (cell math drifts on proportional fonts; the focused inputbox
	// reports its own caret so candidates no longer pile at (0,0)).
	cx, cy, ok := b.caretPixels()
	b.mu.Lock()
	lh := b.lineH
	b.mu.Unlock()
	bounds := image.Rect(0, 0, 1, int(lh+0.5))
	if ok {
		x := int(cx + 0.5)
		y := int(cy + 0.5)
		bounds = image.Rect(x, y, x+1, y+int(lh+0.5))
	}
	if _, err := b.imeField.HandleInputWithBounds(bounds); err != nil {
		return
	}
	full := b.imeField.Text()
	start, _ := b.imeField.Selection()
	ulen := b.imeField.UncommittedTextLengthInBytes()
	comp := ""
	if ulen > 0 && start >= 0 {
		rendering := b.imeField.TextForRendering()
		if start+ulen <= len(rendering) && utf8.ValidString(rendering) {
			comp = rendering[start : start+ulen]
		}
	}
	b.mu.Lock()
	// Mirror the field into the pending stream: typed text appends,
	// in-field deletions (backspace etc.) truncate the stream tail.
	// The tail covers the undrained pending first, then the line
	// buffer or the focused editor, because a consumer drains every
	// tick and already holds older text.
	if drop, add := imeDiff(b.imePrev, full); drop > 0 || add != "" {
		if drop > 0 {
			b.dropLastLocked(drop)
		}
		b.imePending += add
	}
	b.imePrev = full
	b.imeComposing = comp
	// Drained and idle: reset the field so the next tick starts from
	// a clean, append-only slate (invisible-field caret drifts cannot
	// accumulate across ticks).
	if ulen == 0 && b.imePending == "" && b.imePrev != "" {
		b.imeField.SetTextAndSelection("", 0, 0)
		b.imePrev = ""
	}
	b.mu.Unlock()
}

// imeDiff splits new committed field text against the previous tick:
// drop is the rune count to remove from the consumer tail (in-field
// deletions), add is the newly typed text. Pure logic, unit-testable.
func imeDiff(prev, full string) (drop int, add string) {
	or, nr := []rune(prev), []rune(full)
	i := 0
	for i < len(or) && i < len(nr) && or[i] == nr[i] {
		i++
	}
	return len(or) - i, string(nr[i:])
}

// dropLastLocked removes n runes from the end of the IME consumer
// stream: undrained pending first, then the focused editor. Caller
// holds mu (game thread, from pumpIME).
func (b *WindowBackend) dropLastLocked(n int) {
	for n > 0 && b.imePending != "" {
		pr := []rune(b.imePending)
		b.imePending = string(pr[:len(pr)-1])
		n--
	}
	if n == 0 {
		return
	}
	if ed := b.focusedLocked(); ed != nil {
		for n > 0 && len(ed.text) > 0 {
			ed.text = ed.text[:len(ed.text)-1]
			if ed.caret > len(ed.text) {
				ed.caret = len(ed.text)
			}
			n--
		}
	}
}
