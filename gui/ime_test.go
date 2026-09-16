package gui

import "testing"

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
