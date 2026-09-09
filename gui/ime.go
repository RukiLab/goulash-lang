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
	// (cell math drifts on proportional fonts).
	cx, cy, ok := b.caretPixels()
	b.mu.Lock()
	lh := b.lineH
	b.mu.Unlock()
	bounds := image.Rect(0, 0, 1, int(lh+0.5))
	if ok {
		x := int(cx + 0.5)
		y := int(float64(cy)*lh + 0.5)
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
	if len(full) > b.imeSeen {
		b.imePending += full[b.imeSeen:]
		b.imeSeen = len(full)
	}
	b.imeComposing = comp
	// Bound field memory once everything is drained and idle.
	if ulen == 0 && b.imePending == "" && b.imeSeen > 0 {
		b.imeField.SetTextAndSelection("", 0, 0)
		b.imeSeen = 0
	}
	b.mu.Unlock()
}
