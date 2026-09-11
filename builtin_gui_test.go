package main

import "testing"

// TestGuiBuiltinsRegistered asserts the GUI words exist in gui builds.
func TestGuiBuiltinsRegistered(t *testing.T) {
	for _, n := range guiBuiltinNames {
		if !isBuiltin(n) {
			t.Errorf("gui builtin %q not registered", n)
		}
	}
}
