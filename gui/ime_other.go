//go:build !windows

package gui

// osIMECompositionLen reports the OS-side unconfirmed string length.
// Non-Windows backends notify empty compositions through ebiten's
// channel, so no OS ground truth is needed: unknown keeps the
// ebiten-side value.
func osIMECompositionLen() (n int, ok bool) {
	return 0, false
}

// ourWindowHwnds returns this process's visible top-level windows.
// Only the Windows build enumerates real windows; elsewhere there is
// nothing to discover (test hook for the OS-query smoke test).
func ourWindowHwnds() []uintptr {
	return nil
}
