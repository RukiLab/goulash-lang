package gui

import (
	"image"
	"testing"
	"time"
)

// TestIMEBoundsPriority locks candidate-window placement: the manual
// imepos() anchor wins, then the focused inputbox caret, then the
// (0,0) fallback.
func TestIMEBoundsPriority(t *testing.T) {
	// Automatic caret wins over the (0,0) fallback.
	got := imeBounds(false, 0, 0, true, 10.4, 20.6, 20)
	want := image.Rect(10, 21, 11, 41)
	if got != want {
		t.Fatalf("caret bounds = %v, want %v", got, want)
	}
	// Manual imepos wins over the caret.
	got = imeBounds(true, 123, 45, true, 10.4, 20.6, 20)
	want = image.Rect(123, 45, 124, 65)
	if got != want {
		t.Fatalf("anchor bounds = %v, want %v", got, want)
	}
	// Manual imepos also wins with no focused editor.
	got = imeBounds(true, 5, 6, false, 0, 0, 20)
	want = image.Rect(5, 6, 6, 26)
	if got != want {
		t.Fatalf("anchor-only bounds = %v, want %v", got, want)
	}
	// Neither anchor nor caret: the (0,0) fallback.
	got = imeBounds(false, 0, 0, false, 0, 0, 20)
	want = image.Rect(0, 0, 1, 20)
	if got != want {
		t.Fatalf("fallback bounds = %v, want %v", got, want)
	}
}

// TestReconcileCompositionLen locks the empty-composition fix: ebiten's
// Windows backend keeps reporting a stale composition after the
// unconfirmed string becomes empty (zero-length COMPSTR is dropped
// silently, cancel sends nothing), so an OS-side empty must clear the
// mirror that imeget() reads.
func TestReconcileCompositionLen(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp string
		n    int
		ok   bool
		want string
	}{
		// The bug: stale composition + OS empty -> clear.
		{"stale cleared", "か", 0, true, ""},
		// Active composition confirmed by the OS -> keep.
		{"active kept", "か", 6, true, "か"},
		// OS state unknown (non-Windows stub, failures) -> keep.
		{"unknown kept", "か", 0, false, "か"},
		// Already empty stays empty regardless of OS state.
		{"empty stays", "", 0, true, ""},
		{"empty unknown stays", "", 6, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reconcileCompositionLen(tc.comp, tc.n, tc.ok); got != tc.want {
				t.Fatalf("reconcileCompositionLen(%q, %d, %v) = %q, want %q",
					tc.comp, tc.n, tc.ok, got, tc.want)
			}
		})
	}
}

// TestImeClauseOf locks the clause cut: ebiten's CompositionSelection
// byte offsets (relative to the composition start) select the target
// clause (文節); dead ranges and bad cuts yield "".
func TestImeClauseOf(t *testing.T) {
	comp := "きょうはいい天気"
	// "いい" = bytes 12..18, runes 4..6 in the composition
	// (き0-3 ょ3-6 う6-9 は9-12 い12-15 い15-18 天18-21 気21-24).
	if got, rs, re := imeClauseOf(comp, 12, 18, true); got != "いい" || rs != 4 || re != 6 {
		t.Fatalf("clause = (%q,%d,%d), want (いい,4,6)", got, rs, re)
	}
	// Whole composition as the clause.
	if got, rs, re := imeClauseOf(comp, 0, len(comp), true); got != comp || rs != 0 || re != 8 {
		t.Fatalf("full = (%q,%d,%d), want full,0,8", got, rs, re)
	}
	// Repeated words stay distinguishable by position: the second
	// "い" alone is runes 5..6, not 4..5.
	if got, rs, re := imeClauseOf(comp, 15, 18, true); got != "い" || rs != 5 || re != 6 {
		t.Fatalf("second = (%q,%d,%d), want (い,5,6)", got, rs, re)
	}
	for _, tc := range []struct {
		name string
		cs   int
		ce   int
		ok   bool
	}{
		{"dead range", 0, 3, false},
		{"empty selection", 3, 3, true},
		{"negative start", -1, 3, true},
		{"reversed", 6, 3, true},
		{"past end", 0, len(comp) + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, rs, re := imeClauseOf(comp, tc.cs, tc.ce, tc.ok); got != "" || rs != 0 || re != 0 {
				t.Fatalf("imeClauseOf(%q, %d, %d, %v) = (%q,%d,%d), want empty",
					comp, tc.cs, tc.ce, tc.ok, got, rs, re)
			}
		})
	}
	// Mid-character cut is not valid UTF-8: no mojibake.
	if got, _, _ := imeClauseOf(comp, 0, 1, true); got != "" {
		t.Fatalf("split cut = %q, want empty", got)
	}
	if got, _, _ := imeClauseOf("", 0, 0, true); got != "" {
		t.Fatalf("empty comp = %q, want empty", got)
	}
}

// TestIMEClause locks the clause snapshot: text with its rune offsets
// into the imeget() string. Unfocused reads stay zero.
func TestIMEClause(t *testing.T) {
	b := mustNew(t)
	b.imeField.Focus()
	b.mu.Lock()
	b.imeComposing = "あいう"
	b.imeClause = "い"
	b.imeClauseStart, b.imeClauseEnd = 1, 2
	b.mu.Unlock()
	text, rs, re := b.IMEClause()
	if text != "い" || rs != 1 || re != 2 {
		t.Fatalf("clause = (%q,%d,%d), want (い,1,2)", text, rs, re)
	}
	b.imeField.Blur()
	if text, rs, re := b.IMEClause(); text != "" || rs != 0 || re != 0 {
		t.Fatalf("blurred = (%q,%d,%d), want zeros", text, rs, re)
	}
}

// TestIMEAnchorHeadless locks the anchor flag: imepos set/clear
// toggles automatic caret-following.
func TestIMEAnchorHeadless(t *testing.T) {
	b := mustNew(t)
	if _, _, ok := b.IMEAnchor(); ok {
		t.Fatal("default anchor should be automatic (!ok)")
	}
	b.IMESetAnchor(48, 96)
	if x, y, ok := b.IMEAnchor(); !ok || x != 48 || y != 96 {
		t.Fatalf("IMEAnchor = %d,%d,%v, want 48,96,true", x, y, ok)
	}
	b.IMEClearAnchor()
	if _, _, ok := b.IMEAnchor(); ok {
		t.Fatal("after clear anchor should be automatic (!ok)")
	}
	b.IMESetAnchor(8, 20)
	if x, y, ok := b.IMEAnchor(); !ok || x != 8 || y != 20 {
		t.Fatalf("IMEAnchor = (%d,%d,%v), want (8,20,true)", x, y, ok)
	}
}

// TestCaretPixelsHeadless locks the caret anchor: no editor reports
// !ok, a focused editor reports its caret position.
func TestCaretPixelsHeadless(t *testing.T) {
	b := mustNew(t)
	if _, _, ok := b.caretPixels(); ok {
		t.Fatal("no editor should report !ok")
	}
	done := make(chan struct{})
	go func() {
		_ = b.AddInput(5, 10, 20, 160, 36, "あxy")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	b.mu.Lock()
	b.widgets[5].edit.focused = true
	b.mu.Unlock()
	x, y, ok := b.caretPixels()
	if !ok || x <= 0 || y != 20 {
		t.Fatalf("caretPixels = %v,%v,%v", x, y, ok)
	}
}
// TestPlanEditFocus locks the box-focus ↔ IME-field coupling: this is
// the path that made conversion dead inside inputbox (nothing focused
// the field on a box click) and the guards that keep ime(0) sticky
// and a live conversion from being blurred by a stale click level.
func TestPlanEditFocus(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		press, hit, hadFocus, box, field bool
		off                              bool
		wantFocus, wantBlur, wantEnable  bool
	}{
		// Clicking into a box runs conversion out of the box.
		{"click in box", true, true, false, true, false, false, true, false, true},
		// A box is focused but the field is not: sticky re-focus.
		{"self heal", false, false, false, true, false, false, true, false, false},
		// Clicking into a box re-enables conversion after ime(0).
		{"click re-enables ime(0)", true, true, false, true, false, true, true, false, true},
		// ime(0) sticks while no box focus edge happens.
		{"ime(0) sticks", false, false, false, true, false, true, false, false, false},
		// Clicking outside the focused box finishes the entry.
		{"click outside blurs", true, false, true, false, true, false, false, true, false},
		// ime(0) keeps the field blurred: nothing to drop.
		{"ime(0) no blur", true, false, true, false, true, true, false, false, false},
		// Clicking outside with no box focused never touches the field
		// (immediate-mode ime(1) conversion survives a stray click).
		{"outside idle", true, false, false, false, true, false, false, false, false},
		// A frame without a press and no box focus leaves everything alone.
		{"no press", false, false, false, false, true, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := planEditFocus(tc.press, tc.hit, tc.hadFocus, tc.box, tc.field, tc.off)
			if p.focus != tc.wantFocus || p.blur != tc.wantBlur || p.enable != tc.wantEnable {
				t.Fatalf("plan = %+v, want focus=%v blur=%v enable=%v",
					p, tc.wantFocus, tc.wantBlur, tc.wantEnable)
			}
		})
	}
}

// TestReconcileCompositionIdleSkipsOS locks the idle fast path:
// without an ebiten-side composition the mirror is already empty,
// so no OS query is needed and "" is returned as-is.
func TestReconcileCompositionIdleSkipsOS(t *testing.T) {
	if got := reconcileComposition(""); got != "" {
		t.Fatalf("reconcileComposition(%q) = %q, want empty", "", got)
	}
}

// TestOSIMECompositionLenSmoke exercises the OS ground-truth query
// without a window: the test process owns no top-level windows, so the
// length must be unknown (never a spurious "empty" that would clear a
// live composition, never a spurious length). Non-Windows platforms
// use the stub with the same contract.
func TestOSIMECompositionLenSmoke(t *testing.T) {
	if hwnds := ourWindowHwnds(); len(hwnds) != 0 {
		t.Fatalf("ourWindowHwnds() = %v, want none in tests", hwnds)
	}
	if n, ok := osIMECompositionLen(); n != 0 || ok {
		t.Fatalf("osIMECompositionLen() = (%d, %v), want (0, false) without windows", n, ok)
	}
}
