package main

import (
	"errors"
	"strings"
	"testing"
)

func TestVersionWiring(t *testing.T) {
	// usage と REPL バナーが単一の版定数から組み立てられていること。
	if !strings.Contains(usage, "v"+goulashVersion) {
		t.Fatalf("usage %q lacks version v%s", usage, goulashVersion)
	}
}

func TestEnginePanicMsg(t *testing.T) {
	// Only the first line survives (no stack dump).
	got := enginePanicMsg("atlas: too big\n goroutine 23 [running]:\n ...")
	if got != "atlas: too big" {
		t.Fatalf("got %q", got)
	}
	// Non-string panics stringify.
	if got := enginePanicMsg(errors.New("glfw: boom")); got != "glfw: boom" {
		t.Fatalf("got %q", got)
	}
	// Empty/blank panics get a placeholder, never an empty error.
	for _, r := range []any{"", "  \n  "} {
		if got := enginePanicMsg(r); got == "" {
			t.Fatalf("empty panic rendered empty")
		}
	}
}
