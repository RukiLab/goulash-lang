package gui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// mustNew builds a backend or skips when no system font is available
// (font-less CI containers); GUI text needs an OS font since no font
// file is bundled.
func mustNew(t *testing.T) *WindowBackend {
	t.Helper()
	b, err := New(640, 480)
	if err != nil {
		t.Skipf("system font unavailable: %v", err)
	}
	return b
}

func TestSystemFontLoads(t *testing.T) {
	src, p, err := systemFontSource()
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	if src == nil || p == "" {
		t.Fatal("want cached source and path")
	}
	face, err := loadFace(16)
	if err != nil || face == nil {
		t.Fatalf("loadFace: %v", err)
	}
	if FontPath() != p {
		t.Fatalf("FontPath = %q, want %q", FontPath(), p)
	}
}

func TestSetAreaTextHeadless(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		defer close(done)
		addErr = b.AddArea(30, 0, 0, 200, 100, "one\ntwo")
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddArea never completed")
	}
	if addErr != nil {
		t.Fatalf("AddArea: %v", addErr)
	}
	set := make(chan struct{})
	var setErr error
	go func() {
		defer close(set)
		setErr = b.SetAreaText(30, "three\nfour")
	}()
	if !pumpUntil(b, set, 10*time.Second) {
		t.Fatal("SetAreaText never completed")
	}
	if setErr != nil {
		t.Fatalf("SetAreaText: %v", setErr)
	}
	if s, _ := b.AreaText(30); s != "three\nfour" {
		t.Fatalf("AreaText(30) = %q, want three\\nfour", s)
	}
	if err := b.SetAreaText(99, "x"); err == nil {
		t.Fatal("SetAreaText unknown should error")
	}
}

func TestWrapDialogText(t *testing.T) {
	face, err := loadFace(16)
	if err != nil {
		t.Skipf("no system font: %v", err)
	}
	if got := wrapDialogText("short", face, 500); len(got) != 1 || got[0] != "short" {
		t.Fatalf("short = %q", got)
	}
	long := "あいうえおかきくけこさしすせそたちつてとなにぬねの"
	lines := wrapDialogText(long, face, 200)
	if len(lines) < 2 {
		t.Fatalf("long should wrap, got %q", lines)
	}
	for _, ln := range lines {
		if w, _ := text.Measure(ln, face, 0); w > 201 {
			t.Fatalf("line %q too wide: %v", ln, w)
		}
	}
	if strings.Join(lines, "") != long {
		t.Fatalf("wrap lost text: %q", lines)
	}
	multi := wrapDialogText("a b c\nd e", face, 500)
	if len(multi) != 2 || multi[0] != "a b c" || multi[1] != "d e" {
		t.Fatalf("explicit breaks = %q", multi)
	}
}

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

func TestCharStep(t *testing.T) {
	var st charRep
	if got := charStep(&st, "", 0); got != "" {
		t.Fatalf("idle = %q, want empty", got)
	}
	// New text fires at once.
	if got := charStep(&st, "a", 0); got != "a" {
		t.Fatalf("new = %q, want a", got)
	}
	// Same-tick re-poll stays quiet (no double report).
	if got := charStep(&st, "a", 0); got != "" {
		t.Fatalf("dup = %q, want empty", got)
	}
	// OS auto-repeat arrivals on later ticks pass through untouched.
	if got := charStep(&st, "a", 1); got != "a" {
		t.Fatalf("repeat = %q, want a", got)
	}
	if got := charStep(&st, "a", 2); got != "a" {
		t.Fatalf("repeat2 = %q, want a", got)
	}
	// Different text always fires at once, even mid-tick.
	if got := charStep(&st, "b", 2); got != "b" {
		t.Fatalf("switch = %q, want b", got)
	}
	if got := charStep(&st, "b", 2); got != "" {
		t.Fatalf("switch dup = %q, want empty", got)
	}
	// Gaps stay quiet; later arrivals still fire.
	if got := charStep(&st, "", 3); got != "" {
		t.Fatalf("gap = %q, want empty", got)
	}
	if got := charStep(&st, "a", 1000); got != "a" {
		t.Fatalf("late = %q, want a", got)
	}
}

// TestCtrlStep checks control-key repeat without a game loop:
// fire on press, quiet during the delay, refire on the interval,
// release resets to fire-at-once.
func TestCtrlStep(t *testing.T) {
	var st ctrlState
	if ctrlStep(&st, false, 0) {
		t.Fatal("idle should not fire")
	}
	if !ctrlStep(&st, true, 0) {
		t.Fatal("press should fire")
	}
	if ctrlStep(&st, true, 10) {
		t.Fatal("hold below delay should not fire")
	}
	if !ctrlStep(&st, true, repDelay) {
		t.Fatal("delay should refire")
	}
	if !ctrlStep(&st, true, repDelay+repInterval) {
		t.Fatal("interval should refire")
	}
	if ctrlStep(&st, true, repDelay+repInterval+1) {
		t.Fatal("off-beat hold should not fire")
	}
	if ctrlStep(&st, false, repDelay+repInterval+2) {
		t.Fatal("release should not fire")
	}
	if !ctrlStep(&st, true, 1000) {
		t.Fatal("re-press should fire at once")
	}
	if len(ctrlKeys) != 6 {
		t.Fatalf("ctrlKeys = %d entries, want 6", len(ctrlKeys))
	}
	want := map[string]bool{"\x08": true, "\x09": true, "\r": true, "\x1b": true, "\x7f": true}
	for _, ck := range ctrlKeys {
		if !want[ck.ch] {
			t.Fatalf("unexpected control char %q", ck.ch)
		}
	}
}

// TestImeDiff checks the field mirror: appends pass through,
// in-field deletions become tail drops, identical text is a no-op.
func TestImeDiff(t *testing.T) {
	if d, a := imeDiff("", "あ"); d != 0 || a != "あ" {
		t.Fatalf("type = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あ", "あい"); d != 0 || a != "い" {
		t.Fatalf("append = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あい", "あ"); d != 1 || a != "" {
		t.Fatalf("backspace = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あいう", "あ"); d != 2 || a != "" {
		t.Fatalf("multi-drop = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あい", "あう"); d != 1 || a != "う" {
		t.Fatalf("replace = (%d,%q)", d, a)
	}
	if d, a := imeDiff("あ", "あ"); d != 0 || a != "" {
		t.Fatalf("idle = (%d,%q)", d, a)
	}
	// Post-delete commits are not swallowed (the stale-mark bug).
	if d, a := imeDiff("あ", "あい"); d != 0 || a != "い" {
		t.Fatalf("retype = (%d,%q)", d, a)
	}
}

// TestDropLastLocked checks tail truncation across the consumer
// chain: pending first, then the focused editor.
func TestDropLastLocked(t *testing.T) {
	b := mustNew(t)
	b.mu.Lock()
	b.imePending = "abcde"
	b.dropLastLocked(2)
	if b.imePending != "abc" {
		t.Fatalf("pending = %q, want abc", b.imePending)
	}
	b.imePending = "ab"
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[3] = &widgetEntry{id: 3, kind: wInput, selIdx: -1,
		edit: &inputState{text: []rune("XY"), caret: 2, focused: true}}
	b.dropLastLocked(3)
	if b.imePending != "" || string(b.widgets[3].edit.text) != "X" {
		t.Fatalf("edit = %q/%q", b.imePending, string(b.widgets[3].edit.text))
	}
	b.widgets[3].edit.focused = false
	b.widgets[3].edit.text = []rune("hello")
	b.widgets[3].edit.caret = 5
	b.imePending = ""
	b.dropLastLocked(2)
	e := b.widgets[3].edit
	if string(e.text) != "hello" || e.caret != 5 {
		t.Fatalf("unfocused edit = %q/%d (must be untouched)", string(e.text), e.caret)
	}
	b.mu.Unlock()
}

func TestReadImmediateHeadless(t *testing.T) {
	b := mustNew(t)
	// Headless: no loop, nothing typed.
	if got := b.ReadImmediate(); got != "" {
		t.Fatalf("ReadImmediate = %q, want empty", got)
	}
	// While a focused inputbox owns the stream, input stays silent.
	b.mu.Lock()
	if b.widgets == nil {
		b.widgets = map[int]*widgetEntry{}
	}
	b.widgets[4] = &widgetEntry{id: 4, kind: wInput, selIdx: -1,
		edit: &inputState{focused: true}}
	b.mu.Unlock()
	if got := b.ReadImmediate(); got != "" {
		t.Fatalf("ReadImmediate during edit = %q, want empty", got)
	}
	// Drained IME commits flow through input().
	b.mu.Lock()
	b.widgets[4].edit.focused = false
	b.imePending = "あ"
	b.mu.Unlock()
	if got := b.ReadImmediate(); got != "あ" {
		t.Fatalf("ReadImmediate pending = %q, want あ", got)
	}
	b.mu.Lock()
	if b.imePending != "" {
		t.Fatalf("pending not drained: %q", b.imePending)
	}
	b.mu.Unlock()
}

func TestKeyForExtended(t *testing.T) {
	for _, c := range []int{8, 9, 16, 17, 18, 33, 34, 35, 36, 45, 46, 112, 123, 186, 192, 219, 222} {
		if _, ok := keyFor(c); !ok {
			t.Fatalf("keyFor(%d) should be known", c)
		}
	}
	if _, ok := keyFor(999); ok {
		t.Fatal("keyFor(999) should be unknown")
	}
	if _, ok := keyFor(124); ok {
		t.Fatal("keyFor(124) should be unknown")
	}
}

func TestSetFontFileHeadless(t *testing.T) {
	b := mustNew(t)
	base := FontPath()
	if base == "" {
		t.Skip("no system font")
	}
	// Bare base-name resolution hits the same file.
	name := base
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	got, err := resolveFontSpec(name)
	if err != nil {
		t.Fatalf("resolveFontSpec(%q): %v", name, err)
	}
	if !strings.EqualFold(got, base) {
		t.Fatalf("resolved = %q, want %q", got, base)
	}
	if err := b.SetFontFile(name, 24); err != nil {
		t.Fatalf("SetFontFile: %v", err)
	}
	if b.FontSize() != 24 {
		t.Fatalf("FontSize = %d, want 24", b.FontSize())
	}
	if b.FontFile() == "" {
		t.Fatal("FontFile should be set after SetFontFile")
	}
	if err := b.SetFontFile("no-such-font-xyz-123", 16); err == nil {
		t.Fatal("bogus font should error")
	}
	if err := b.SetFontFile(name, 7); err == nil {
		t.Fatal("size 7 should error")
	}
}

// TestFontPerUserDir covers bare-name resolution in the per-user font
// directory (Windows default for non-admin installs). The scan only
// needs the file name, so a dummy file suffices (no parsing involved).
func TestFontPerUserDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("per-user Fonts dir is a Windows concept")
	}
	dir := filepath.Join(t.TempDir(), "Microsoft", "Windows", "Fonts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	const name = "TestDummyPlemolJP-Regular.ttf"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	got, err := resolveFontSpec(name)
	if err != nil {
		t.Fatalf("resolveFontSpec(%q): %v", name, err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, name)) {
		t.Fatalf("resolved = %q, want under per-user Fonts", got)
	}
}

// TestFontPrefixMatch covers extension-less prefix resolution: a bare
// family name finds its files without an extension, preferring the
// Regular weight over alphabetical order; exact base names still win,
// and unknown names (with or without extension) still error.
func TestFontPrefixMatch(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("per-user Fonts dir is a Windows concept")
	}
	dir := filepath.Join(t.TempDir(), "Microsoft", "Windows", "Fonts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"ZZZPrefixTwo.ttf", "ZZZPrefixOne.ttf", "ZZZPrefix-Regular.ttf",
		"ZZZExact.ttf", "ZZZExact-Bold.ttf",
		"ZZZAlphaTwo.ttf", "ZZZAlphaOne.ttf",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LOCALAPPDATA", filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	// Prefix, case-insensitive, Regular preferred over alphabetical first.
	got, err := resolveFontSpec("zzzprefix")
	if err != nil {
		t.Fatalf("resolveFontSpec(prefix): %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "ZZZPrefix-Regular.ttf")) {
		t.Fatalf("prefix resolved = %q, want the Regular face", got)
	}
	// Alphabetical fallback without a Regular face.
	got, err = resolveFontSpec("zzzalpha")
	if err != nil {
		t.Fatalf("resolveFontSpec(alpha): %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "ZZZAlphaOne.ttf")) {
		t.Fatalf("alpha resolved = %q, want first alphabetically", got)
	}
	// Exact base name still beats prefix matches.
	got, err = resolveFontSpec("ZZZExact")
	if err != nil {
		t.Fatalf("resolveFontSpec(exact): %v", err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "ZZZExact.ttf")) {
		t.Fatalf("exact resolved = %q, want the exact file", got)
	}
	// Unknown names still error, with or without an extension.
	if _, err := resolveFontSpec("ZZZNoSuchFont"); err == nil {
		t.Fatal("unknown prefix should error")
	}
	if _, err := resolveFontSpec("ZZZNoSuchFont.ttf"); err == nil {
		t.Fatal("unknown filename should error")
	}
}

func TestPrintlnState(t *testing.T) {
	b := mustNew(t)
	b.Println("Hello", 0)
	b.Println("World", 0)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.segs) != 2 {
		t.Fatalf("segs: %+v", b.segs)
	}
	if b.segs[0].s != "Hello" || b.segs[1].s != "World" {
		t.Fatalf("segs: %+v", b.segs)
	}
	if b.curY != 2 {
		t.Fatalf("curY = %d, want 2", b.curY)
	}
}

// TestPrintlnHonorsPos covers pos() + mes()/print() positioning:
// a bare mes() starts at the text cursor, and pos() flushes a pending
// print() partial first (matching the immediate CUI terminal).
func TestPrintlnHonorsPos(t *testing.T) {
	b := mustNew(t)
	cw, lh := b.charW, b.lineH
	at := func(cx, cy int) (int, int) { return int(float64(cx) * cw), int(float64(cy) * lh) }
	b.Println("plain", 0)
	x, y := at(5, 2)
	b.MoveTo(x, y)
	b.Println("hi", 0)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.segs) != 2 {
		t.Fatalf("segs: %+v", b.segs)
	}
	if b.segs[0].x != 0 || b.segs[0].y != 0 || b.segs[0].s != "plain" {
		t.Fatalf("segs[0]: %+v", b.segs[0])
	}
	if b.segs[1].x != 5 || b.segs[1].y != 2 || b.segs[1].s != "hi" {
		t.Fatalf("segs[1]: %+v", b.segs[1])
	}
}

// TestPrintlnMultilinePos checks continuation lines restart at column 0.
func TestPrintlnMultilinePos(t *testing.T) {
	b := mustNew(t)
	cw, lh := b.charW, b.lineH
	b.MoveTo(int(3*cw), int(4*lh))
	b.Println("a\nb", 0)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.segs) != 2 {
		t.Fatalf("segs: %+v", b.segs)
	}
	if b.segs[0].x != 3 || b.segs[0].y != 4 || b.segs[0].s != "a" {
		t.Fatalf("segs[0]: %+v", b.segs[0])
	}
	if b.segs[1].x != 0 || b.segs[1].y != 5 || b.segs[1].s != "b" {
		t.Fatalf("segs[1]: %+v", b.segs[1])
	}
}

// TestMoveToFlushesPartial locks the print/pos/print composition:
// the first fragment stays where print() started it.
func TestMoveToFlushesPartial(t *testing.T) {
	b := mustNew(t)
	cw, lh := b.charW, b.lineH
	b.Print("ab", 0)
	b.MoveTo(int(4*cw), int(lh))
	b.Print("cd", 0)
	b.Println("ef", 0)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.segs) != 2 {
		t.Fatalf("segs: %+v", b.segs)
	}
	if b.segs[0].x != 0 || b.segs[0].s != "ab" {
		t.Fatalf("segs[0]: %+v", b.segs[0])
	}
	if b.segs[1].x != 4 || b.segs[1].s != "cdef" {
		t.Fatalf("segs[1]: %+v", b.segs[1])
	}
}

// TestTextRowTop locks the mes/print row origin: text/v2 puts the
// region top at the GeoM origin, so row tops sit exactly on the cell
// grid (a stray +ascent once pushed every line ~one row down).
func TestTextRowTop(t *testing.T) {
	if got := textRowTop(0, 0, 20); got != 0 {
		t.Fatalf("row0 = %v, want 0", got)
	}
	if got := textRowTop(2, 0, 20); got != 40 {
		t.Fatalf("row2 = %v, want 40", got)
	}
	if got := textRowTop(5, 3, 20); got != 40 {
		t.Fatalf("scrolled = %v, want 40", got)
	}
}

// TestPrintNewlineSplit covers print() with embedded newlines: complete
// lines become segments, only the trailing chunk stays partial.
func TestPrintNewlineSplit(t *testing.T) {
	b := mustNew(t)
	b.Print("a\nb\nc", 0)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.segs) != 2 || b.segs[0].s != "a" || b.segs[0].y != 0 || b.segs[1].s != "b" || b.segs[1].y != 1 {
		t.Fatalf("segs: %+v", b.segs)
	}
	if b.partial != "c" || b.partX != 0 || b.curY != 2 || b.curX != 1 {
		t.Fatalf("partial=%q partX=%d curY=%d curX=%d", b.partial, b.partX, b.curY, b.curX)
	}
}

// TestPrintNewlineJoinsPartial checks print/print/mes composition across
// an embedded newline.
func TestPrintNewlineJoinsPartial(t *testing.T) {
	b := mustNew(t)
	b.Print("ab", 0)
	b.Print("c\nd", 0)
	b.Println("e", 0)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.segs) != 2 || b.segs[0].s != "abc" || b.segs[1].s != "de" || b.segs[1].y != 1 {
		t.Fatalf("segs: %+v", b.segs)
	}
}

// TestPrintTrailingNewline leaves a clean empty partial on the next row.
func TestPrintTrailingNewline(t *testing.T) {
	b := mustNew(t)
	b.Print("a\n", 0)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.segs) != 1 || b.segs[0].s != "a" {
		t.Fatalf("segs: %+v", b.segs)
	}
	if b.partial != "" || b.curY != 1 || b.curX != 0 {
		t.Fatalf("partial=%q curY=%d curX=%d", b.partial, b.curY, b.curX)
	}
}

// TestPrintNewlineStyleChange keeps per-fragment styles across the split.
func TestPrintNewlineStyleChange(t *testing.T) {
	b := mustNew(t)
	b.Print("a", StyleBold)
	b.Print("b\nc", 0)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.segs) != 2 {
		t.Fatalf("segs: %+v", b.segs)
	}
	if b.segs[0].s != "a" || b.segs[0].st != StyleBold {
		t.Fatalf("segs[0]: %+v", b.segs[0])
	}
	if b.segs[1].s != "b" || b.segs[1].st != 0 {
		t.Fatalf("segs[1]: %+v", b.segs[1])
	}
	if b.partial != "c" || b.partSt != 0 {
		t.Fatalf("partial=%q partSt=%v", b.partial, b.partSt)
	}
}

func TestPrintStyles(t *testing.T) {
	b := mustNew(t)
	b.Println("a", StyleBold)
	b.Print("b", StyleItalic)
	// Style change mid-line flushes the partial: "b" keeps italic.
	b.Println("c", 0)
	// Same-style print() accumulates one partial.
	b.Print("x", StyleBold)
	b.Print("y", StyleBold)
	b.Println("z", StyleBold)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.segs) != 4 {
		t.Fatalf("segs: %+v", b.segs)
	}
	if b.segs[0].st != StyleBold || b.segs[1].st != StyleItalic || b.segs[2].st != 0 {
		t.Fatalf("styles: %+v", b.segs)
	}
	if b.segs[0].s != "a" || b.segs[1].s != "b" || b.segs[2].s != "c" {
		t.Fatalf("texts: %+v", b.segs)
	}
	if b.segs[3].s != "xyz" || b.segs[3].st != StyleBold {
		t.Fatalf("segs[3]: %+v", b.segs[3])
	}
}

// TestWidgetRegistryHeadless drives AddButton with a manual pump
// (no game loop): creation path is pure Go until Draw/Update run.
func TestWidgetRegistryHeadless(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		addErr = b.AddButton(11, "T", 10, 420, 100, 36, 0)
		close(done)
	}()
	// Pump until done: the helper goroutine may need scheduling time,
	// so poll with a deadline instead of a fixed iteration count.
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done:
			goto pumped
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("AddButton never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped:
	if addErr != nil {
		t.Fatalf("AddButton: %v", addErr)
	}
	if !b.HasWidget(11) {
		t.Fatal("button 11 missing after headless add")
	}
}

// TestIMEStateHeadless covers focus/query/composition mirrors
// without pumping (no game loop needed for state).
func TestIMEStateHeadless(t *testing.T) {
	b := mustNew(t)
	// Another backend's focus must not leak: start clean.
	b.IMESet(false)
	if got := b.IMEState(); got != 0 {
		t.Fatalf("IMEState = %d, want 0", got)
	}
	if got := b.IMESet(true); got != 1 {
		t.Fatalf("IMESet(true) = %d, want 1", got)
	}
	if got := b.IMEState(); got != 1 {
		t.Fatalf("IMEState = %d, want 1", got)
	}
	if got := b.IMEComposition(); got != "" {
		t.Fatalf("IMEComposition = %q, want empty", got)
	}
	if got := b.IMESet(false); got != 0 {
		t.Fatalf("IMESet(false) = %d, want 0", got)
	}
}

// TestReadImmediateIdleHeadless: with no loop running nothing is
// typed, so input reports "" and arms no session.
func TestReadImmediateIdleHeadless(t *testing.T) {
	b := mustNew(t)
	if got := b.ReadImmediate(); got != "" {
		t.Fatalf("ReadImmediate = %q, want empty", got)
	}
	if b.charSt != (charRep{}) {
		t.Fatal("charSt should be idle")
	}
}

// TestPicLoadFirst is a regression test: PicLoad as the very first
// image op must not panic on a nil targets map (global picload).
func TestPicLoadFirst(t *testing.T) {
	b := mustNew(t)
	if b.targets == nil || b.bufs == nil {
		t.Fatal("image maps must be initialized by New")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	done := make(chan struct{})
	var id int
	var loadErr error
	go func() {
		id, loadErr = b.PicLoad(p)
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done:
			goto pumped
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("PicLoad never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped:
	if loadErr != nil {
		t.Fatalf("PicLoad: %v", loadErr)
	}
	if id <= 0 {
		t.Fatalf("id = %d, want > 0", id)
	}
	if b.targets[id] == nil {
		t.Fatal("uploaded image missing from targets")
	}
}

// TestButtonImageHeadless covers the icon path: unknown buffer errors
// without a loop, and a picload buffer attaches headless with a pump.
func TestButtonImageHeadless(t *testing.T) {
	b := mustNew(t)
	if err := b.AddButton(11, "T", 10, 420, 100, 36, 99); err == nil {
		t.Fatal("unknown image buffer should error")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "icon.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	done := make(chan struct{})
	var imgID int
	var loadErr error
	go func() {
		imgID, loadErr = b.PicLoad(p)
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done:
			goto pumped
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("PicLoad never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped:
	if loadErr != nil {
		t.Fatalf("PicLoad: %v", loadErr)
	}
	done2 := make(chan struct{})
	var addErr error
	go func() {
		addErr = b.AddButton(12, "Icon", 120, 420, 100, 36, imgID)
		close(done2)
	}()
	deadline = time.Now().Add(10 * time.Second)
	for {
		b.drainQueue()
		select {
		case <-done2:
			goto pumped2
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("AddButton never completed")
		}
		time.Sleep(time.Millisecond)
	}
pumped2:
	if addErr != nil {
		t.Fatalf("AddButton with image: %v", addErr)
	}
	if !b.HasWidget(12) {
		t.Fatal("image button 12 missing after headless add")
	}
}

// TestButtonImageStatesHeadless validates the 4 image slots
// (normal/hover/pressed/disabled) without a game loop.
func TestButtonImageStatesHeadless(t *testing.T) {
	b := mustNew(t)
	if err := b.AddButton(21, "T", 10, 10, 100, 36, 1, 2, 3, 4, 5); err == nil {
		t.Fatal("5 images should error")
	}
	if err := b.AddButton(21, "T", 10, 10, 100, 36, 99); err == nil {
		t.Fatal("unknown buffer should error")
	}
	if err := b.AddButton(21, "T", 10, 10, 100, 36, 0, 99); err == nil {
		t.Fatal("unknown hover buffer should error")
	}
}

// renderGame performs all pixel probes in a single RunGame: Ebiten allows
// only one game loop per process, and draw commands batch a frame behind,
// so each stage draws for two frames and samples on the next one.
// pxRes carries one GetPixel round-trip result.
type pxRes struct {
	c   [4]int
	err error
}

type renderGame struct {
	b                              *WindowBackend
	face                           *text.GoTextFace
	frames                         int
	fillLit, textLit, backLit      int
	rectLit, circleLit, anyLit     int
	minX, minY, maxX, maxY         int
	keyIdle, mouseX, mouseY        int
	clickedVal                     bool
	probedInput                    bool
	filled, texted, backed, shaped bool
	done                           bool
	wLaunched, wPhase2Launched     bool
	wChecked1, wChecked2           bool
	wPhase1, wPhase2               chan struct{}
	saveLaunched, saveChecked      bool
	savePath                       string
	saveDone                       chan struct{}
	saveErr                        error
	gzChecked                      bool
	pfChecked                      bool
	pxLaunched, pxChecked          bool
	pxDone                         chan pxRes
	edLaunched, edMeasured         bool
	edRow, edFrames, edDistinct    int
	edHashes                       []uint64
	wFail                          string
	wText                          string
	wErrs                          []string
}

func (g *renderGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	// Pump the backend like the real loop does (drains the draw queue).
	_ = g.b.Update()
	return nil
}

func (g *renderGame) Draw(screen *ebiten.Image) {
	g.frames++
	switch {
	case g.frames <= 2:
		screen.Fill(color.NRGBA{0xFF, 0, 0, 0xFF})
		if g.frames == 2 {
			if r, _, _, _ := screen.At(100, 100).RGBA(); r > 0 {
				g.fillLit = 1
			}
			g.filled = true
		}
	case g.frames <= 4:
		screen.Fill(color.Black)
		g.drawDirectText(screen)
		if g.frames == 4 {
			g.textLit = countLit(screen, 0, 0, 160, 40)
			g.texted = true
		}
	case g.frames <= 6:
		g.b.Draw(screen)
		if g.frames == 6 {
			g.backLit = countLit(screen, 0, 0, 320, 40)
			g.backed = true
		}
	case g.frames == 7:
		// Enqueue like a script would, then drain synchronously:
		// ebiten does not guarantee 1:1 Update/Draw pairing, so a test
		// must not depend on the next Update having run.
		g.b.FillRect(300, 100, 40, 30, [4]int{255, 0, 0, 255})
		g.b.Circle(100, 300, 20, true, [4]int{0, 255, 0, 255})
		g.b.drainQueue()
		g.b.Draw(screen)
	case g.frames >= 8:
		g.b.Draw(screen)
		if r, _, _, _ := screen.At(310, 110).RGBA(); r > 0x8000 {
			g.rectLit = 1
		}
		if _, gg, _, _ := screen.At(100, 300).RGBA(); gg > 0x8000 {
			g.circleLit = 1
		}
		g.anyLit = countLit(screen, 0, 30, 640, 480)
		g.minX, g.minY, g.maxX, g.maxY = 9999, 9999, -1, -1
		for y := 0; y < 480; y++ {
			for x := 0; x < 640; x++ {
				r, gg, bl, _ := screen.At(x, y).RGBA()
				if r > 0 || gg > 0 || bl > 0 {
					if x < g.minX {
						g.minX = x
					}
					if x > g.maxX {
						g.maxX = x
					}
					if y < g.minY {
						g.minY = y
					}
					if y > g.maxY {
						g.maxY = y
					}
				}
			}
		}
		g.shaped = true
		// In-loop input queries (unsafe outside the loop).
		// Idle arrows stand in for the removed stick() probe.
		if left, ok := g.b.KeyDown(37); ok && left {
			g.keyIdle = 1
		}
		if up, ok := g.b.KeyDown(38); ok && up {
			g.keyIdle = 1
		}
		g.mouseX, g.mouseY = g.b.MousePos()
		g.clickedVal = g.b.Clicked()
		g.probedInput = true
		// Widget stage: blocking backend calls run on a helper goroutine
		// (they wait for Update, so never call them on this thread).
		// Phase 1 creates; phase 2 removes after game-thread checks.
		if !g.wLaunched {
			g.wLaunched = true
			g.wPhase1 = make(chan struct{})
			go g.makeWidgets()
		}
		select {
		case <-g.wPhase1:
			// Closed channels stay ready: check exactly once.
			if !g.wChecked1 {
				g.wChecked1 = true
				g.checkWidgetsPhase1()
				if g.wFail == "" && !g.wPhase2Launched {
					g.wPhase2Launched = true
					g.wPhase2 = make(chan struct{})
					go g.removeWidgets()
				}
			}
		default:
		}
		select {
		case <-g.wPhase2:
			if !g.wChecked2 {
				g.wChecked2 = true
				g.checkWidgetsPhase2()
			}
		default:
		}
		// Save stage: snapshot buffer 0 to PNG on a helper goroutine
		// (SavePNG waits for Update, so never call it on this thread).
		if g.wChecked2 && g.wFail == "" && !g.saveChecked {
			if !g.saveLaunched {
				g.saveLaunched = true
				g.saveDone = make(chan struct{})
				go func() {
					if err := g.b.SavePNG(g.savePath, 0); err != nil {
						g.saveErr = err
					}
					close(g.saveDone)
				}()
			}
			select {
			case <-g.saveDone:
				g.saveChecked = true
				g.checkSaved()
			default:
			}
		}
		// Zoom stage: 2x blit on the game thread, then resample.
		// Runs after the save snapshot so buffer 0 stays pristine for it.
		if g.saveChecked && g.wFail == "" && !g.gzChecked {
			g.gzChecked = true
			g.checkZoom(screen)
		}
		// Paint stage: white square then flood-fill, then resample.
		if g.gzChecked && g.wFail == "" && !g.pfChecked {
			g.pfChecked = true
			g.checkPaint(screen)
		}
		// Pixel stage: pget round-trip (SetPixel like a script, read
		// back via GetPixel on a helper goroutine — it waits for
		// Update, so never call it on this thread). Runs after paint
		// so earlier fills cannot clobber the probe pixel.
		if g.pfChecked && g.wFail == "" && !g.pxChecked {
			if !g.pxLaunched {
				g.pxLaunched = true
				g.b.SetPixel(500, 200, [4]int{255, 0, 0, 255})
				g.pxDone = make(chan pxRes, 1)
				go func() {
					c, err := g.b.GetPixel(500, 200)
					g.pxDone <- pxRes{c, err}
				}()
			}
			select {
			case r := <-g.pxDone:
				g.pxChecked = true
				if r.err != nil {
					g.wFail = "pget: " + r.err.Error()
				} else if r.c != [4]int{255, 0, 0, 255} {
					g.wFail = fmt.Sprintf("pget roundtrip = %v, want opaque red", r.c)
				}
			default:
			}
		}
		// Editor stage: hold an active ReadLine prompt and hash its row
		// every frame. A steady prompt yields exactly one distinct hash.
		if g.pfChecked && g.wFail == "" {
			if !g.edLaunched {
				g.edLaunched = true
				go func() { _, _ = g.b.ReadLine("name? ") }()
				g.b.mu.Lock()
				g.edRow = g.b.curY
				g.b.mu.Unlock()
			} else if g.edFrames < 90 {
				g.edHashes = append(g.edHashes, hashRow(screen, g.edRow))
				g.edFrames++
			} else if !g.edMeasured {
				g.edMeasured = true
				seen := map[uint64]bool{}
				for _, h := range g.edHashes {
					seen[h] = true
				}
				g.edDistinct = len(seen)
				g.done = true
			}
		}
		if g.frames > 1200 && !g.done {
			// Preserve the first failure: overwriting would mask it.
			if g.wFail == "" {
				g.wFail = "widget stage timed out"
			}
			g.done = true
		}
	}
}

// hashRow hashes a text row band (every 4th x, every 2nd y) via FNV-1a.
func hashRow(screen *ebiten.Image, row int) uint64 {
	const basis, prime = uint64(14695981039346656037), uint64(1099511628211)
	h := basis
	y0 := row * 20
	for y := y0; y < y0+20; y += 2 {
		for x := 0; x < 640; x += 4 {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			h ^= uint64(r>>8) + 1
			h *= prime
			h ^= uint64(gg>>8) + 1
			h *= prime
			h ^= uint64(bl>>8) + 1
			h *= prime
		}
	}
	return h
}

// makeWidgets exercises widget creation like a script would.
func (g *renderGame) makeWidgets() {
	defer close(g.wPhase1)
	if err := g.b.AddButton(11, "T", 10, 420, 100, 36, 0); err != nil {
		g.wErrs = append(g.wErrs, "button: "+err.Error())
	}
	if err := g.b.AddInput(12, 120, 420, 160, 36, "init"); err != nil {
		g.wErrs = append(g.wErrs, "input: "+err.Error())
	}
	if err := g.b.AddList(13, 290, 380, 150, 80, []string{"a", "b"}); err != nil {
		g.wErrs = append(g.wErrs, "list: "+err.Error())
	}
	t, err := g.b.InputText(12)
	if err != nil {
		g.wErrs = append(g.wErrs, "gettext: "+err.Error())
	}
	g.wText = t
	if err := g.b.AddCheck(21, "Agree", 10, 300, 200, 32, true); err != nil {
		g.wErrs = append(g.wErrs, "chkbox: "+err.Error())
	}
	if err := g.b.AddCombo(22, 220, 300, 200, 36, []string{"x", "y"}, 1); err != nil {
		g.wErrs = append(g.wErrs, "combox: "+err.Error())
	}
	if err := g.b.AddArea(23, 430, 300, 190, 60, "l1"); err != nil {
		g.wErrs = append(g.wErrs, "mesbox: "+err.Error())
	}
}

// checkSaved decodes the pngsave snapshot (game thread; pure Go decode)
// and verifies buffer 0 round-tripped: size plus the stage-7 shapes.
func (g *renderGame) checkSaved() {
	if g.saveErr != nil {
		g.wFail = "pngsave: " + g.saveErr.Error()
		return
	}
	f, err := os.Open(g.savePath)
	if err != nil {
		g.wFail = "pngsave missing: " + err.Error()
		return
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		g.wFail = "pngsave decode: " + err.Error()
		return
	}
	if w, h := img.Bounds().Dx(), img.Bounds().Dy(); w != 640 || h != 480 {
		g.wFail = fmt.Sprintf("pngsave size = %dx%d, want 640x480", w, h)
		return
	}
	r, _, _, _ := img.At(310, 110).RGBA()
	if r < 0x8000 {
		g.wFail = "pngsave lost the red rect"
		return
	}
	_, gg, _, _ := img.At(100, 300).RGBA()
	if gg < 0x8000 {
		g.wFail = "pngsave lost the green circle"
	}
}

// checkZoom draws a 20px red square on buffer 1, blits it 2x onto
// buffer 0, redraws, and verifies scaled pixels (game thread).
// Destination avoids widget rects (ui draws over the canvas).
func (g *renderGame) checkZoom(screen *ebiten.Image) {
	b := g.b
	b.SelectTarget(1)
	b.FillRect(0, 0, 20, 20, [4]int{255, 0, 0, 255})
	b.drainQueue()
	b.SelectTarget(0)
	if err := b.BlitScaled(1, 0, 0, 20, 20, 2, 2, 460, 200); err != nil {
		g.wFail = "gzoom: " + err.Error()
		return
	}
	b.drainQueue()
	b.Draw(screen)
	if r, _, _, _ := screen.At(470, 210).RGBA(); r < 0x8000 {
		g.wFail = "gzoom pixel not scaled"
		return
	}
	if r, gg, bl, _ := screen.At(455, 195).RGBA(); r > 0 || gg > 0 || bl > 0 {
		g.wFail = "gzoom leaked outside the target rect"
	}
}

// checkPaint fills a white square red and verifies fill plus no leak
// (game thread). The square avoids widget rects (ui draws over canvas).
func (g *renderGame) checkPaint(screen *ebiten.Image) {
	b := g.b
	b.FillRect(460, 360, 40, 40, [4]int{255, 255, 255, 255})
	b.drainQueue()
	if err := b.FloodFill(480, 380, [4]int{255, 0, 0, 255}); err != nil {
		g.wFail = "paint: " + err.Error()
		return
	}
	b.drainQueue()
	b.Draw(screen)
	if r, gg, bl, _ := screen.At(480, 380).RGBA(); r < 0x8000 || gg > 0 || bl > 0 {
		g.wFail = "paint fill missing"
		return
	}
	if r, gg, bl, _ := screen.At(455, 395).RGBA(); r > 0 || gg > 0 || bl > 0 {
		g.wFail = "paint leaked outside the region"
	}
}

// removeWidgets runs after phase-1 checks.
func (g *renderGame) removeWidgets() {
	defer close(g.wPhase2)
	if err := g.b.RemoveWidget(11); err != nil {
		g.wErrs = append(g.wErrs, "remove: "+err.Error())
	}
}

// checkWidgetsPhase1 runs on the game thread (mutex-only calls safe here).
func (g *renderGame) checkWidgetsPhase1() {
	if len(g.wErrs) > 0 {
		g.wFail = g.wErrs[0]
		return
	}
	if !g.b.HasWidget(11) || !g.b.HasWidget(12) || !g.b.HasWidget(13) {
		g.b.mu.Lock()
		keys := make([]int, 0)
		kinds := map[int]int{}
		for k, v := range g.b.widgets {
			keys = append(keys, k)
			kinds[k] = int(v.kind)
		}
		nkids := 0
		var rects int
		if g.b.root != nil {
			nkids = len(g.b.root.Children())
		}
		if g.b.fixLayout != nil {
			rects = len(g.b.fixLayout.rects)
		}
		g.b.mu.Unlock()
		g.wFail = fmt.Sprintf("widgets should exist (keys=%v kinds=%v kids=%d rects=%d errs=%v text=%q)",
			keys, kinds, nkids, rects, g.wErrs, g.wText)
		return
	}
	if g.wText != "init" {
		g.wFail = "gettext mismatch: " + g.wText
		return
	}
	if !g.b.HasWidget(21) || !g.b.HasWidget(22) || !g.b.HasWidget(23) {
		g.wFail = "form widgets should exist"
		return
	}
	if p, err := g.b.Checked(21); err != nil || !p {
		g.wFail = "fresh checkbox must be checked"
		return
	}
	if idx, err := g.b.SelectedIndex(22); err != nil || idx != 1 {
		g.wFail = "fresh combo selection must be 1"
		return
	}
	if s, err := g.b.AreaText(23); err != nil || s != "l1" {
		g.wFail = "mesbox text mismatch: " + s
		return
	}
	if p, err := g.b.Pressed(11); err != nil || p {
		g.wFail = "fresh button must be unpressed"
		return
	}
	if _, err := g.b.Pressed(12); err == nil {
		g.wFail = "pressed on inputbox should error"
		return
	}
	idx, err := g.b.SelectedIndex(13)
	if err != nil || idx != -1 {
		g.wFail = "fresh list selection must be -1"
		return
	}
}

// checkWidgetsPhase2 runs on the game thread after removal.
func (g *renderGame) checkWidgetsPhase2() {
	if len(g.wErrs) > 0 && g.wFail == "" {
		g.wFail = g.wErrs[0]
		return
	}
	if g.b.HasWidget(11) {
		g.wFail = "button 11 should be removed"
		return
	}
	if _, err := g.b.Pressed(11); err == nil {
		g.wFail = "pressed on removed button should error"
		return
	}
	if _, err := g.b.SelectedIndex(99); err == nil {
		g.wFail = "selected on unknown id should error"
		return
	}
}

func (g *renderGame) drawDirectText(screen *ebiten.Image) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(0, g.face.Metrics().HAscent)
	op.ColorScale.Reset()
	op.ColorScale.ScaleWithColor(color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})
	text.Draw(screen, "Hello", g.face, op)
}

func countLit(screen *ebiten.Image, x0, y0, x1, y1 int) int {
	lit := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, gg, bl, _ := screen.At(x, y).RGBA()
			if r > 0 || gg > 0 || bl > 0 {
				lit++
			}
		}
	}
	return lit
}

func (g *renderGame) Layout(outsideWidth, outsideHeight int) (int, int) {
	return 640, 480
}

// pumpUntil drains the draw queue until done closes (headless stand-in
// for the game loop's Update).
func pumpUntil(b *WindowBackend, done chan struct{}, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		b.drainQueue()
		select {
		case <-done:
			b.drainQueue()
			return true
		default:
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}

// TestInputTextNoStall: gettext must not block on queue drain. A blocking
// read stalls cls/mes redraws mid-frame, which flickered the
// mes("input="+gettext(id)) line in the widgets demo.
func TestInputTextNoStall(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		addErr = b.AddInput(12, 120, 420, 160, 36, "にほんご")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	if addErr != nil {
		t.Fatalf("AddInput: %v", addErr)
	}
	// No drain from here on: if InputText still enqueues, this times out.
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := b.InputText(12)
		ch <- res{s, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("InputText: %v", r.err)
		}
		if r.s != "にほんご" {
			t.Fatalf("InputText = %q, want %q", r.s, "にほんご")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("InputText blocked waiting for queue drain (flicker hazard)")
	}
}

// TestInputTextJapaneseRefresh: game-thread edits surface immediately
// (the custom editor state is the source of truth; no mirror needed).
func TestInputTextJapaneseRefresh(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	go func() {
		_ = b.AddInput(12, 120, 420, 160, 36, "にほんご")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	set := make(chan struct{})
	b.enqueue(func() {
		b.mu.Lock()
		e := b.widgets[12]
		e.edit.text = []rune("日本語テスト")
		e.edit.caret = len(e.edit.text)
		b.mu.Unlock()
		close(set)
	})
	if !pumpUntil(b, set, 10*time.Second) {
		t.Fatal("edit never applied")
	}
	s, err := b.InputText(12)
	if err != nil {
		t.Fatalf("InputText: %v", err)
	}
	if s != "日本語テスト" {
		t.Fatalf("InputText = %q, want %q", s, "日本語テスト")
	}
	if _, err := b.InputText(99); err == nil {
		t.Fatal("InputText on unknown id should error")
	}
}

// TestInputEditOps checks caret editing ops without a game loop.
func TestInputEditOps(t *testing.T) {
	e := &inputState{text: []rune("abc"), caret: 3}
	e.insert([]rune("で"))
	if string(e.text) != "abcで" || e.caret != 4 {
		t.Fatalf("insert = %q/%d", string(e.text), e.caret)
	}
	e.moveLeft()
	e.moveLeft()
	e.backspace()
	if string(e.text) != "acで" || e.caret != 1 {
		t.Fatalf("backspace = %q/%d", string(e.text), e.caret)
	}
	e.del()
	if string(e.text) != "aで" || e.caret != 1 {
		t.Fatalf("del = %q/%d", string(e.text), e.caret)
	}
	e.moveRight()
	e.moveRight()
	e.moveRight() // clamps at the end
	if e.caret != 2 {
		t.Fatalf("caret = %d, want 2", e.caret)
	}
	e.backspace()
	e.backspace()
	e.backspace() // clamps at the start
	if string(e.text) != "" || e.caret != 0 {
		t.Fatalf("clear = %q/%d", string(e.text), e.caret)
	}
}

// TestInputRepFire checks held-key repeat gating without a game loop.
func TestInputRepFire(t *testing.T) {
	e := &inputState{}
	if e.repFire(ebiten.KeyBackspace, false, false, 0) {
		t.Fatal("released should not fire")
	}
	if !e.repFire(ebiten.KeyBackspace, true, true, 0) {
		t.Fatal("just-pressed should fire")
	}
	if e.repFire(ebiten.KeyBackspace, false, true, 10) {
		t.Fatal("hold below delay should not fire")
	}
	if !e.repFire(ebiten.KeyBackspace, false, true, repDelay) {
		t.Fatal("delay should refire")
	}
	if !e.repFire(ebiten.KeyBackspace, false, true, repDelay+repInterval) {
		t.Fatal("interval should refire")
	}
	if e.repFire(ebiten.KeyBackspace, false, false, repDelay+repInterval+1) {
		t.Fatal("release should not fire")
	}
	if !e.repFire(ebiten.KeyBackspace, true, true, 1000) {
		t.Fatal("re-press should fire at once")
	}
}

// TestInputFocusModel checks click focus/blur without a game loop.
func TestInputFocusModel(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	go func() {
		_ = b.AddInput(1, 0, 0, 100, 30, "a")
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddInput never completed")
	}
	b.mu.Lock()
	e := b.widgets[1]
	if e == nil || e.edit == nil {
		b.mu.Unlock()
		t.Fatal("entry missing")
	}
	e.edit.focused = true
	if got := b.focusedLocked(); got != e.edit {
		b.mu.Unlock()
		t.Fatal("focusedLocked should find the editor")
	}
	b.blurEditsLocked()
	if b.focusedLocked() != nil {
		b.mu.Unlock()
		t.Fatal("blur should clear focus")
	}
	b.mu.Unlock()
	if got, err := b.InputText(1); err != nil || got != "a" {
		t.Fatalf("InputText = %q, %v", got, err)
	}
}

// repeat runs (go test -count=N) skip instead of panicking.
// TestListEntryColorsComplete: every list state needs a background.
// A nil Pressed/Hover background renders transparent, which hid the
// highlight while the mouse button was held (selected() had flipped,
// but blue only appeared on release).
func TestListEntryColorsComplete(t *testing.T) {
	c := listEntryColors()
	type named struct {
		name string
		v    interface {
			RGBA() (uint32, uint32, uint32, uint32)
		}
	}
	nils := []string{}
	for _, e := range []named{
		{"Unselected", c.Unselected},
		{"Selected", c.Selected},
		{"DisabledUnselected", c.DisabledUnselected},
		{"DisabledSelected", c.DisabledSelected},
		{"SelectingBackground", c.SelectingBackground},
		{"SelectedBackground", c.SelectedBackground},
		{"FocusedBackground", c.FocusedBackground},
		{"SelectingFocusedBackground", c.SelectingFocusedBackground},
		{"SelectedFocusedBackground", c.SelectedFocusedBackground},
		{"DisabledSelectedBackground", c.DisabledSelectedBackground},
	} {
		if e.v == nil {
			nils = append(nils, e.name)
		}
	}
	if len(nils) > 0 {
		t.Fatalf("nil entry colors (transparent states): %v", nils)
	}
}

// TestSavePNGErrors covers pngsave validation without a game loop
// (all paths return before touching the loop).
func TestSavePNGErrors(t *testing.T) {
	b := mustNew(t)
	for _, tc := range []struct {
		path string
		id   int
	}{
		{"x.png", -1},
		{"x.png", 99},
		{"", 0},
		{"x.png", 3}, // never allocated
	} {
		if err := b.SavePNG(tc.path, tc.id); err == nil {
			t.Fatalf("SavePNG(%q, %d) should error", tc.path, tc.id)
		}
	}
}

// TestBlitScaledErrors covers gzoom validation without a game loop
// (all paths return before enqueueing).
func TestBlitScaledErrors(t *testing.T) {
	b := mustNew(t)
	for _, tc := range []struct {
		src, w, h int
		zx, zy    float64
	}{
		{99, 10, 10, 1, 1}, // unknown buffer
		{0, 0, 10, 1, 1},
		{0, 10, -1, 1, 1},
		{0, 10, 10, 0, 1},
		{0, 10, 10, 1, -2},
	} {
		if err := b.BlitScaled(tc.src, 0, 0, tc.w, tc.h, tc.zx, tc.zy, 0, 0); err == nil {
			t.Fatalf("BlitScaled(%+v) should error", tc)
		}
	}
}

// TestFlood fills a square without leaking through the background.
func TestFlood(t *testing.T) {
	w, h := 8, 8
	px := make([]byte, 4*w*h)
	set := func(x, y int, c [4]byte) {
		i := (y*w + x) * 4
		px[i], px[i+1], px[i+2], px[i+3] = c[0], c[1], c[2], c[3]
	}
	at := func(x, y int) [4]byte {
		i := (y*w + x) * 4
		return [4]byte{px[i], px[i+1], px[i+2], px[i+3]}
	}
	white := [4]byte{0xFF, 0xFF, 0xFF, 0xFF}
	red := color.NRGBA{0xFF, 0, 0, 0xFF}
	for y := 2; y < 6; y++ {
		for x := 2; x < 6; x++ {
			set(x, y, white)
		}
	}
	if !flood(px, w, h, 3, 3, red) {
		t.Fatal("fill should report a change")
	}
	for _, p := range [][2]int{{2, 2}, {3, 3}, {5, 5}, {2, 5}} {
		if got := at(p[0], p[1]); got != [4]byte{0xFF, 0, 0, 0xFF} {
			t.Fatalf("pixel %v = %v, want red", p, got)
		}
	}
	for _, p := range [][2]int{{1, 1}, {6, 3}, {0, 7}, {3, 6}} {
		if got := at(p[0], p[1]); got != [4]byte{} {
			t.Fatalf("pixel %v = %v, want transparent", p, got)
		}
	}
	if flood(px, w, h, 3, 3, red) {
		t.Fatal("seed already in fill color should be a no-op")
	}
}

// TestFloodEnclosed keeps the fill inside a 1px border.
func TestFloodEnclosed(t *testing.T) {
	w, h := 8, 8
	px := make([]byte, 4*w*h)
	set := func(x, y int, c [4]byte) {
		i := (y*w + x) * 4
		px[i], px[i+1], px[i+2], px[i+3] = c[0], c[1], c[2], c[3]
	}
	at := func(x, y int) [4]byte {
		i := (y*w + x) * 4
		return [4]byte{px[i], px[i+1], px[i+2], px[i+3]}
	}
	black := [4]byte{0, 0, 0, 0xFF}
	white := [4]byte{0xFF, 0xFF, 0xFF, 0xFF}
	for x := 2; x < 6; x++ {
		set(x, 2, black)
		set(x, 5, black)
		set(2, x, black)
		set(5, x, black)
	}
	set(3, 3, white)
	set(4, 4, white)
	red := color.NRGBA{0xFF, 0, 0, 0xFF}
	if !flood(px, w, h, 3, 3, red) {
		t.Fatal("fill should report a change")
	}
	if got := at(3, 3); got != [4]byte{0xFF, 0, 0, 0xFF} {
		t.Fatalf("interior = %v, want red", got)
	}
	if got := at(4, 4); got != white {
		t.Fatalf("disconnected interior = %v, want white", got)
	}
	for _, p := range [][2]int{{2, 3}, {3, 2}, {5, 4}} {
		if got := at(p[0], p[1]); got != black {
			t.Fatalf("border %v = %v, want black", p, got)
		}
	}
	if got := at(1, 1); got != [4]byte{} {
		t.Fatalf("exterior = %v, want transparent", got)
	}
}

// TestFloodFillBounds covers paint seed validation without a game loop.
func TestFloodFillBounds(t *testing.T) {
	b := mustNew(t)
	if err := b.FloodFill(-1, 0, [4]int{255, 0, 0, 255}); err == nil {
		t.Fatal("negative seed should error")
	}
	if err := b.FloodFill(0, 480, [4]int{255, 0, 0, 255}); err == nil {
		t.Fatal("out-of-range seed should error")
	}
	// A valid seed only enqueues (applied by Update), so no loop needed.
	if err := b.FloodFill(10, 10, [4]int{255, 0, 0, 255}); err != nil {
		t.Fatalf("valid seed: %v", err)
	}
}

// TestGetPixelBounds covers pget coordinate validation without a game
// loop (bounds are checked before any game-thread readback).
func TestGetPixelBounds(t *testing.T) {
	b := mustNew(t)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {640, 0}, {0, 480}, {1000000, 1000000}} {
		if _, err := b.GetPixel(p[0], p[1]); err == nil {
			t.Fatalf("GetPixel%v should error", p)
		}
	}
}

// NOTE: GetPixel round-trip needs a running game loop (ebiten forbids
// readback before it starts), so it is covered by the pget stage in
// TestGuiRender below instead of a headless test.

// TestSetFontSize covers font() state without a game loop
// (face reload is pure Go; no images involved).
func TestSetFontSize(t *testing.T) {
	b := mustNew(t)
	if b.charW != 8 || b.lineH != 20 || b.fontSize != 16 {
		t.Fatalf("defaults = %v/%v/%v, want 8/20/16", b.charW, b.lineH, b.fontSize)
	}
	if err := b.SetFontSize(32); err != nil {
		t.Fatalf("SetFontSize(32): %v", err)
	}
	if b.charW != 16 || b.lineH != 40 || b.fontSize != 32 {
		t.Fatalf("scaled = %v/%v/%v, want 16/40/32", b.charW, b.lineH, b.fontSize)
	}
	if b.uiFace() == nil {
		t.Fatal("uiFace should follow the resized face")
	}
	b.MoveTo(10, 5)
	b.mu.Lock()
	gx, gy := b.gx, b.gy
	b.mu.Unlock()
	if gx != 10 || gy != 5 {
		t.Fatalf("cursor = (%d,%d), want (10,5)", gx, gy)
	}
	for _, size := range []int{-1, 0, 7, 65, 100} {
		if err := b.SetFontSize(size); err == nil {
			t.Fatalf("SetFontSize(%d) should error", size)
		}
	}
}

// TestFontDrawSmoke renders resized text headless (no readback:
// ReadPixels panics outside the loop, plain Draw does not).
func TestFontDrawSmoke(t *testing.T) {
	b := mustNew(t)
	if err := b.SetFontSize(32); err != nil {
		t.Fatal(err)
	}
	b.Println("Hello, GUI! あいうえお", 0)
	b.Draw(ebiten.NewImage(640, 480))
}

// TestFormWidgetsHeadless drives chkbox/combox/mesbox with a manual pump
// (no game loop): creation mirrors plus Update-time refreshes.
func TestFormWidgetsHeadless(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	var addErr error
	go func() {
		defer close(done)
		if err := b.AddCheck(21, "Agree", 10, 300, 0, 0, false); err != nil {
			addErr = err
			return
		}
		if err := b.AddCombo(22, 220, 300, 200, 36, []string{"x", "y"}, 0); err != nil {
			addErr = err
			return
		}
		if err := b.AddArea(23, 430, 300, 190, 60, "l1\nl2"); err != nil {
			addErr = err
			return
		}
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("form widget creation never completed")
	}
	if addErr != nil {
		t.Fatalf("add: %v", addErr)
	}
	// Creation-time mirrors.
	if p, err := b.Checked(21); err != nil || p {
		t.Fatalf("Checked(21) = %v, %v; want false", p, err)
	}
	if idx, err := b.SelectedIndex(22); err != nil || idx != 0 {
		t.Fatalf("SelectedIndex(22) = %v, %v; want 0", idx, err)
	}
	if s, err := b.AreaText(23); err != nil || s != "l1\nl2" {
		t.Fatalf("AreaText(23) = %q, %v", s, err)
	}
	// Mutate like the game thread would, then refresh mirrors.
	set := make(chan struct{})
	b.enqueue(func() {
		b.mu.Lock()
		e21, e22, e23 := b.widgets[21], b.widgets[22], b.widgets[23]
		b.mu.Unlock()
		e21.check.SetState(widget.WidgetChecked)
		e22.combo.SetSelectedEntry("y")
		e23.area.SetText("changed")
		close(set)
	})
	if !pumpUntil(b, set, 10*time.Second) {
		t.Fatal("mutation never applied")
	}
	b.syncWidgetCache() // what Update does every tick
	if p, _ := b.Checked(21); !p {
		t.Fatal("Checked(21) should be true after SetState")
	}
	if idx, _ := b.SelectedIndex(22); idx != 1 {
		t.Fatalf("SelectedIndex(22) = %d, want 1", idx)
	}
	if s, _ := b.AreaText(23); s != "changed" {
		t.Fatalf("AreaText(23) = %q, want changed", s)
	}
	// Non-positive w/h auto-sizes from the font and label (pumped:
	// successful adds block on runOnLoop like every other widget).
	auto := make(chan struct{})
	var autoErr error
	go func() {
		defer close(auto)
		autoErr = b.AddCheck(24, "x", 0, 0, 0, 0, false)
	}()
	if !pumpUntil(b, auto, 10*time.Second) {
		t.Fatal("auto-size chkbox never completed")
	}
	if autoErr != nil {
		t.Fatalf("auto-size chkbox: %v", autoErr)
	}
	// placeChild records the rect synchronously (SetLocation itself
	// runs on the next real-loop layout).
	b.mu.Lock()
	r, ok := b.fixLayout.rects[b.widgets[24].child]
	b.mu.Unlock()
	if !ok || r.Dx() <= 0 || r.Dy() <= 0 {
		t.Fatalf("auto-size chkbox rect = %v, want positive size", r)
	}
	// Validation (all return before touching the loop).
	if err := b.AddCombo(24, 0, 0, 100, 30, []string{"a"}, 5); err == nil {
		t.Fatal("combox out-of-range sel should error")
	}
	if err := b.AddArea(24, 0, 0, 100, 0, ""); err == nil {
		t.Fatal("mesbox zero height should error")
	}
	if _, err := b.Checked(99); err == nil {
		t.Fatal("checked unknown should error")
	}
	if _, err := b.AreaText(21); err == nil {
		t.Fatal("getstr on a checkbox should error")
	}
	if _, err := b.SelectedIndex(21); err == nil {
		t.Fatal("selected on a checkbox should error")
	}
	if err := b.SetEnabled(99, true); err == nil {
		t.Fatal("objprm unknown should error")
	}
	// objprm applies on the loop (pumped here manually).
	en := make(chan struct{})
	var enErr error
	go func() {
		defer close(en)
		enErr = b.SetEnabled(21, false)
	}()
	if !pumpUntil(b, en, 10*time.Second) {
		t.Fatal("SetEnabled never applied")
	}
	if enErr != nil {
		t.Fatalf("SetEnabled: %v", enErr)
	}
	b.mu.Lock()
	dis := b.widgets[21].child.GetWidget().Disabled
	b.mu.Unlock()
	if !dis {
		t.Fatal("widget 21 should be disabled")
	}
}

// TestCheckImageSized guards the checkbox-label regression: solid-color
// nine slices report MinSize 0, and ebitenui draws the labeled box at
// checkboxPreferredSize, so the box rendered 0x0 (label text only).
func TestCheckImageSized(t *testing.T) {
	b := mustNew(t)
	img := b.checkImage()
	faces := map[string]interface{ MinSize() (int, int) }{
		"Unchecked":         img.Unchecked,
		"UncheckedHovered":  img.UncheckedHovered,
		"UncheckedDisabled": img.UncheckedDisabled,
		"Checked":           img.Checked,
		"CheckedHovered":    img.CheckedHovered,
		"CheckedDisabled":   img.CheckedDisabled,
	}
	for name, f := range faces {
		if f == nil {
			t.Fatalf("%s face is nil", name)
		}
		if w, h := f.MinSize(); w < 12 || h < 12 {
			t.Fatalf("%s MinSize = %dx%d, want >= 12 (invisible box)", name, w, h)
		}
	}
	// Art content: unchecked is solid body, checked carries the glyph.
	body := color.NRGBA{0x22, 0x26, 0x2E, 0xFF}
	mark := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	plain := checkArtRGBA(20, body, false, mark)
	if got := plain.NRGBAAt(0, 0); got != body {
		t.Fatalf("unchecked corner = %v, want body %v", got, body)
	}
	if got := plain.NRGBAAt(10, 10); got != body {
		t.Fatalf("unchecked center = %v, want body %v", got, body)
	}
	ticked := checkArtRGBA(20, body, true, mark)
	if got := ticked.NRGBAAt(9, 14); got != mark {
		t.Fatalf("checked glyph joint = %v, want mark %v", got, mark)
	}
	// Headless render smoke: labeled checkbox Draw must not panic
	// (plain Draw works without a game loop; readback does not).
	done := make(chan struct{})
	go func() {
		_ = b.AddCheck(21, "Agree", 10, 300, 200, 32, false)
		close(done)
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddCheck never completed")
	}
	b.Draw(ebiten.NewImage(640, 480))
}

// TestComboClickRepro drives the click-time combo path headless:
// open the dropdown, Update, Draw, select an entry, Update again.
// This reproduces the combox click panic without a window.
func TestComboClickRepro(t *testing.T) {
	b := mustNew(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := b.AddCombo(22, 220, 300, 200, 36, []string{"x", "y", "z"}, 0); err != nil {
			t.Errorf("AddCombo: %v", err)
		}
	}()
	if !pumpUntil(b, done, 10*time.Second) {
		t.Fatal("AddCombo never completed")
	}
	open := make(chan struct{})
	go func() {
		defer close(open)
		b.runOnLoop(func() {
			b.widgets[22].combo.SetContentVisible(true)
		})
	}()
	if !pumpUntil(b, open, 10*time.Second) {
		t.Fatal("SetContentVisible never applied")
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update with open combo: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
	sel := make(chan struct{})
	go func() {
		defer close(sel)
		b.runOnLoop(func() {
			b.widgets[22].combo.SetSelectedEntry("y")
		})
	}()
	if !pumpUntil(b, sel, 10*time.Second) {
		t.Fatal("SetSelectedEntry never applied")
	}
	if err := b.Update(); err != nil {
		t.Fatalf("Update after select: %v", err)
	}
	b.Draw(ebiten.NewImage(640, 480))
	if idx, err := b.SelectedIndex(22); err != nil || idx != 1 {
		t.Fatalf("SelectedIndex(22) = %d, %v; want 1", idx, err)
	}
	// Edge paths: empty and duplicate items must survive open+select too.
	for _, tc := range []struct {
		id    int
		items []string
		sel   string
		want  int
	}{
		{31, []string{}, "", -1},
		{32, []string{"a", "a", "b"}, "a", 0},
	} {
		d := make(chan struct{})
		id, items := tc.id, tc.items
		go func() {
			defer close(d)
			_ = b.AddCombo(id, 220, 300, 200, 36, items, -1)
		}()
		if !pumpUntil(b, d, 10*time.Second) {
			t.Fatalf("AddCombo(%d) never completed", id)
		}
		o := make(chan struct{})
		go func() {
			defer close(o)
			b.runOnLoop(func() {
				b.widgets[id].combo.SetContentVisible(true)
			})
		}()
		if !pumpUntil(b, o, 10*time.Second) {
			t.Fatalf("SetContentVisible(%d) never applied", id)
		}
		if err := b.Update(); err != nil {
			t.Fatalf("Update with open combo %d: %v", id, err)
		}
		b.Draw(ebiten.NewImage(640, 480))
		if tc.sel != "" {
			s := make(chan struct{})
			go func() {
				defer close(s)
				b.runOnLoop(func() {
					b.widgets[id].combo.SetSelectedEntry(tc.sel)
				})
			}()
			if !pumpUntil(b, s, 10*time.Second) {
				t.Fatalf("SetSelectedEntry(%d) never applied", id)
			}
			if err := b.Update(); err != nil {
				t.Fatalf("Update after select %d: %v", id, err)
			}
		}
		if idx, err := b.SelectedIndex(id); err != nil || idx != tc.want {
			t.Fatalf("SelectedIndex(%d) = %d, %v; want %d", id, idx, err, tc.want)
		}
	}
}

// TestAwaitTick drives the frame counter without a game loop:
// advanceTick is what Update calls, so awaiting it headless matches
// the in-loop behavior.
func TestAwaitTick(t *testing.T) {
	b := mustNew(t)
	if b.Tick() != 0 {
		t.Fatalf("initial tick = %d, want 0", b.Tick())
	}
	b.Await(0)
	b.Await(-5) // no-ops, must return immediately
	done := make(chan struct{})
	go func() {
		b.Await(2)
		close(done)
	}()
	// Generous gaps: wakeups re-register within microseconds.
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		b.advanceTick()
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Await(2) never returned after 3 ticks")
	}
	if b.Tick() != 3 {
		t.Fatalf("tick = %d, want 3", b.Tick())
	}
	// Late waiter with ticks already past returns at once.
	later := make(chan struct{})
	go func() {
		b.Await(1)
		close(later)
	}()
	time.Sleep(50 * time.Millisecond) // let the waiter register
	b.advanceTick()
	select {
	case <-later:
	case <-time.After(10 * time.Second):
		t.Fatal("Await(1) never returned")
	}
}

// TestRotateTransform checks grotate geometry without a game loop:
// the region center lands on the cursor at any angle.
func TestRotateTransform(t *testing.T) {
	m := rotateTransform(20, 10, 1, 1, 0, 100, 50)
	if x, y := m.Apply(10, 5); x != 100 || y != 50 {
		t.Fatalf("center = (%v,%v), want (100,50)", x, y)
	}
	if x, y := m.Apply(0, 0); x != 90 || y != 45 {
		t.Fatalf("corner = (%v,%v), want (90,45)", x, y)
	}
	// 90 degrees: right-middle (20,5) rotates to bottom-middle.
	m = rotateTransform(20, 10, 1, 1, 90, 100, 50)
	if x, y := m.Apply(20, 5); diff(x, 100) || diff(y, 60) {
		t.Fatalf("rotated = (%v,%v), want (100,60)", x, y)
	}
	// Zoom composes: center still lands on the cursor.
	m = rotateTransform(20, 10, 2, 3, 45, 7, 9)
	if x, y := m.Apply(10, 5); diff(x, 7) || diff(y, 9) {
		t.Fatalf("scaled center = (%v,%v), want (7,9)", x, y)
	}
}

func diff(a, b float64) bool {
	d := a - b
	return d > 1e-9 || d < -1e-9
}

// TestBlitRotateErrors covers grotate validation without a game loop.
func TestBlitRotateErrors(t *testing.T) {
	b := mustNew(t)
	for _, tc := range []struct {
		src, w, h int
		zx, zy    float64
	}{
		{99, 10, 10, 1, 1}, // unknown buffer
		{0, 0, 10, 1, 1},
		{0, 10, -1, 1, 1},
		{0, 10, 10, 0, 1},
		{0, 10, 10, 1, -2},
	} {
		if err := b.BlitRotate(tc.src, 0, 0, tc.w, tc.h, 30, tc.zx, tc.zy, 0, 0); err == nil {
			t.Fatalf("BlitRotate(%+v) should error", tc)
		}
	}
}

// TestAlphaState covers galpha without a game loop.
func TestAlphaState(t *testing.T) {
	b := mustNew(t)
	if b.alphaScale() != 1 {
		t.Fatalf("default alpha scale = %v, want 1", b.alphaScale())
	}
	if err := b.SetAlpha(128); err != nil {
		t.Fatalf("SetAlpha(128): %v", err)
	}
	if s := b.alphaScale(); s < 0.5 || s > 0.51 {
		t.Fatalf("alpha scale = %v, want ~0.502", s)
	}
	for _, a := range []int{-1, 256, 1000} {
		if err := b.SetAlpha(a); err == nil {
			t.Fatalf("SetAlpha(%d) should error", a)
		}
	}
	if err := b.SetAlpha(255); err != nil {
		t.Fatalf("SetAlpha(255): %v", err)
	}
}

// TestMouseWheelConsume checks the wheel latch: whole detents are
// reported once, fractions carry over to the next call.
func TestMouseWheelConsume(t *testing.T) {
	b := mustNew(t)
	if got := b.MouseWheel(); got != 0 {
		t.Fatalf("idle wheel = %d, want 0", got)
	}
	b.mu.Lock()
	b.wheelAccum = 2.7
	b.mu.Unlock()
	if got := b.MouseWheel(); got != 2 {
		t.Fatalf("wheel = %d, want 2", got)
	}
	if got := b.MouseWheel(); got != 0 {
		t.Fatalf("second read = %d, want 0 (0.7 carries)", got)
	}
	b.mu.Lock()
	b.wheelAccum += 0.5
	b.mu.Unlock()
	if got := b.MouseWheel(); got != 1 {
		t.Fatalf("carried wheel = %d, want 1", got)
	}
	b.mu.Lock()
	b.wheelAccum = -1.7
	b.mu.Unlock()
	if got := b.MouseWheel(); got != -1 {
		t.Fatalf("negative wheel = %d, want -1", got)
	}
}

// loopRan guards the one-loop-per-process Ebiten limit:
// repeat runs (go test -count=N) skip instead of panicking.
var loopRan = false

// TestGuiRender verifies end-to-end pixel output: environment readback,
// direct text, and the backend path (catches "black screen" regressions).
func TestGuiRender(t *testing.T) {
	if loopRan {
		t.Skip("ebiten allows a single game loop per process")
	}
	loopRan = true
	b := mustNew(t)
	b.Println("Hello, GUI! あいうえお", 0)
	ebiten.SetWindowSize(640, 480)
	ebiten.SetWindowTitle("Goulash test")
	g := &renderGame{b: b, face: b.face, savePath: filepath.Join(t.TempDir(), "snap.png")}
	// NOTE: window teardown on some machines reports
	// "glfw: DestroyWindow failed"; sampling results are what matter.
	_ = ebiten.RunGame(g)
	t.Logf("frames=%d filled=%v texted=%v backed=%v", g.frames, g.filled, g.texted, g.backed)
	if !g.filled || g.fillLit == 0 {
		t.Fatal("ReadPixels sees nothing even for Fill: environment issue")
	}
	if !g.texted || g.textLit == 0 {
		t.Fatal("direct text.Draw renders nothing")
	}
	if !g.backed {
		t.Fatal("backend stage never ran")
	}
	t.Logf("fill=%d text=%d backend=%d rect=%d circle=%d any=%d shaped=%v bbox=(%d,%d)-(%d,%d)", g.fillLit, g.textLit, g.backLit, g.rectLit, g.circleLit, g.anyLit, g.shaped, g.minX, g.minY, g.maxX, g.maxY)
	if g.backLit == 0 {
		t.Fatal("no lit pixels in the first text row: black screen")
	}
	if !g.shaped || g.rectLit == 0 {
		t.Fatal("FillRect pixel not visible")
	}
	if g.circleLit == 0 {
		t.Fatal("Circle pixel not visible")
	}
	if !g.probedInput {
		t.Fatal("input stage never ran")
	}
	if !g.pxChecked {
		t.Fatal("pget stage never ran")
	}
	t.Logf("arrows=%d mouse=(%d,%d) clicked=%v", g.keyIdle, g.mouseX, g.mouseY, g.clickedVal)
	if g.keyIdle != 0 {
		t.Fatalf("idle arrows = %d, want 0", g.keyIdle)
	}
	if g.clickedVal {
		t.Fatal("idle clicked = true, want false")
	}
	// NOTE: the real cursor can sit anywhere on the desktop, even outside
	// the window, so mouse position is logged but not range-checked.
	if g.wFail != "" {
		t.Fatalf("widget stage: %s", g.wFail)
	}
	t.Log("widget stage ok")
	if !g.edMeasured {
		t.Fatal("editor stage never ran")
	}
	t.Logf("editor row=%d frames=%d distinct=%d", g.edRow, g.edFrames, g.edDistinct)
	if g.edDistinct != 1 {
		t.Fatalf("input() prompt flickers: %d distinct frames", g.edDistinct)
	}
}
