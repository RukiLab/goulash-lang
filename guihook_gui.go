//go:build gui

package main

import (
	"gou/gui"
)

// GUI hooks: only compiled with -tags gui, so the default console build
// carries no Ebiten dependency.
func init() {
	newGUIBackend = func(w, h int) (Backend, error) { return gui.New(w, h) }
	runGUILoop = func(be Backend, run func()) int {
		wb, ok := be.(*gui.WindowBackend)
		if !ok {
			return 2
		}
		return gui.RunLoop(wb, run)
	}
	guiSetDone = func(be Backend) {
		if wb, ok := be.(*gui.WindowBackend); ok {
			wb.SetDone()
		}
	}
}
