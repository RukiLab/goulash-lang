package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runFile parses a script file (with #include splicing) and runs it,
// returning mes() output. Both parse-time and runtime errors surface.
func runFile(t *testing.T, path string) (string, error) {
	t.Helper()
	prog, err := ParseFile(path)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	err = in.Run(prog)
	return buf.String(), err
}

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestModeDirective(t *testing.T) {
	dir := t.TempDir()
	modeOf := func(name, src string) string {
		t.Helper()
		writeFile(t, filepath.Join(dir, name), src)
		_, mode, err := CombineFileMode(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return mode
	}
	// Omitted means gui.
	if m := modeOf("a.gsh", "mes(\"hi\")\n"); m != "gui" {
		t.Fatalf("default = %q, want gui", m)
	}
	if m := modeOf("b.gsh", "#mode cli\nmes(\"hi\")\n"); m != "cli" {
		t.Fatalf("cli = %q", m)
	}
	if m := modeOf("c.gsh", "#mode gui\nmes(\"hi\")\n"); m != "gui" {
		t.Fatalf("gui = %q", m)
	}
	// Last active one wins.
	if m := modeOf("d.gsh", "#mode cli\n#mode gui\n"); m != "gui" {
		t.Fatalf("last-wins = %q", m)
	}
	// Inactive branches do not count.
	if m := modeOf("e.gsh", "#ifdef NOPE\n#mode cli\n#endif\n"); m != "gui" {
		t.Fatalf("inactive = %q", m)
	}
	writeFile(t, filepath.Join(dir, "lib.gsh"), "#mode cli\n")
	if m := modeOf("f.gsh", "#include \"lib.gsh\"\n"); m != "cli" {
		t.Fatalf("include = %q", m)
	}
	// Bad values are an error; the unknown-directive hint lists #mode.
	writeFile(t, filepath.Join(dir, "x.gsh"), "#mode bogus\n")
	if _, _, err := CombineFileMode(filepath.Join(dir, "x.gsh")); err == nil ||
		!strings.Contains(err.Error(), "#mode cli") {
		t.Fatalf("bad mode err = %v", err)
	}
	if _, err := CombineSource("", "#bogus\n", ""); err == nil ||
		!strings.Contains(err.Error(), "#mode") {
		t.Fatalf("unknown directive hint = %v", err)
	}
}

func TestIncludeBasic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lib.gsh"), "def double(x) {\nreturn x * 2\n}\nanswer = 20\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"lib.gsh\"\nmes(double(21))\nmes(answer + 1)\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "42\n21\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIncludeNestedDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sub", "util.gsh"), "def inc(x) {\nreturn x + 1\n}\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"sub/util.gsh\"\nmes(inc(41))\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "42\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIncludeOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "base.gsh"), "def f() {\nreturn 7\n}\n")
	writeFile(t, filepath.Join(dir, "mid.gsh"), "#include \"base.gsh\"\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"base.gsh\"\n#include \"mid.gsh\"\nmes(f())\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("shared library must splice once, got: %v", err)
	}
	if got != "7\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIncludeCircular(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.gsh"), "#include \"b.gsh\"\n")
	writeFile(t, filepath.Join(dir, "b.gsh"), "#include \"a.gsh\"\n")
	if _, err := ParseFile(filepath.Join(dir, "a.gsh")); err == nil || !strings.Contains(err.Error(), "循環") {
		t.Fatalf("want circular error, got %v", err)
	}
}

func TestIncludeMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"nope.gsh\"\n")
	if _, err := ParseFile(filepath.Join(dir, "main.gsh")); err == nil || !strings.Contains(err.Error(), "開けません") {
		t.Fatalf("want open error, got %v", err)
	}
}

func TestIncludeMalformed(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		"#include foo\n",
		"#define x\n",
		"#include \"unclosed\n",
		"#include \"\" \n",
		"#include \"a.gsh\" trailing\n",
	} {
		writeFile(t, filepath.Join(dir, "main.gsh"), src)
		if _, err := ParseFile(filepath.Join(dir, "main.gsh")); err == nil {
			t.Fatalf("src %q: want directive error, got nil", src)
		}
	}
	// Inline # is not a directive: the lexer rejects it.
	writeFile(t, filepath.Join(dir, "main.gsh"), "x = 1 #include \"a.gsh\"\n")
	if _, err := ParseFile(filepath.Join(dir, "main.gsh")); err == nil || !strings.Contains(err.Error(), "不正な文字") {
		t.Fatalf("want illegal character error, got %v", err)
	}
}

func TestIncludeErrorPosition(t *testing.T) {
	dir := t.TempDir()
	// Runtime error inside the library reports the library file+line.
	writeFile(t, filepath.Join(dir, "lib.gsh"), "def boom() {\nreturn nosuchvar\n}\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"lib.gsh\"\nmes(boom())\n")
	_, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err == nil || !strings.Contains(err.Error(), "lib.gsh:2:") {
		t.Fatalf("want lib.gsh:2: position, got %v", err)
	}
	// Syntax errors likewise.
	writeFile(t, filepath.Join(dir, "bad.gsh"), "if {\n}\n")
	writeFile(t, filepath.Join(dir, "main2.gsh"), "#include \"bad.gsh\"\n")
	if _, err := ParseFile(filepath.Join(dir, "main2.gsh")); err == nil || !strings.Contains(err.Error(), "bad.gsh:1:") {
		t.Fatalf("want bad.gsh:1: position, got %v", err)
	}
	// Errors in the main file keep working.
	writeFile(t, filepath.Join(dir, "main3.gsh"), "mes(1)\nif {\n}\n")
	if _, err := ParseFile(filepath.Join(dir, "main3.gsh")); err == nil || !strings.Contains(err.Error(), "main3.gsh:2:") {
		t.Fatalf("want main3.gsh:2: position, got %v", err)
	}
}

func TestIncludeArrayAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "types.gsh"), "point = [3, 4]\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#include \"types.gsh\"\nmes(point[0])\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "3\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDefineBasic(t *testing.T) {
	got, err := runSrcPP(t, "#define MAX 0x10\nmes(MAX)\nmes(MAX + 1)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "16\n17\n" {
		t.Fatalf("got %q", got)
	}
	// String, float, bool and negative constants.
	got, err = runSrcPP(t, "#define S \"hi\"\n#define F 2.5\n#define B true\n#define N -3\nmes(S)\nmes(F)\nmes(B)\nmes(N)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "hi\n2.5\ntrue\n-3\n" {
		t.Fatalf("got %q", got)
	}
	// A use on an earlier line than the definition is not substituted.
	if _, err := runSrcPP(t, "mes(A)\n#define A 1\n"); err == nil {
		t.Fatal("forward define use should fail")
	}
}

func TestDefineErrors(t *testing.T) {
	for _, src := range []string{
		"#define M 2 * 3\n",          // expressions are not constants
		"#define M\n",                // missing value
		"#define M x\n",              // identifier is not a literal
		"#define 1X 2\n",             // bad name
		"#define X 1\n#define X 2\n", // redefinition
	} {
		if _, err := runParsePP(src); err == nil {
			t.Fatalf("src %q: want define error, got nil", src)
		}
	}
}

func TestEnumBasic(t *testing.T) {
	got, err := runSrcPP(t, "enum Color { Red, Green, Blue }\nmes(Color_Red)\nmes(Color_Green)\nmes(Color_Blue)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "0\n1\n2\n" {
		t.Fatalf("got %q", got)
	}
	// Anonymous enums define bare names; explicit values reset the counter.
	got, err = runSrcPP(t, "enum { A, B = 5, C }\nmes(A)\nmes(B)\nmes(C)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "0\n5\n6\n" {
		t.Fatalf("got %q", got)
	}
	// Multi-line enums and negative values.
	got, err = runSrcPP(t, "enum Dir {\nNorth,\nSouth = -2,\nEast\n}\nmes(Dir_North)\nmes(Dir_South)\nmes(Dir_East)\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "0\n-2\n-1\n" {
		t.Fatalf("got %q", got)
	}
	// Inactive regions never reach the parser.
	got, err = runSrcPP(t, "#ifdef MISSING\nenum Skip { X }\n#endif\nmes(\"ok\")\n")
	if err != nil || got != "ok\n" {
		t.Fatalf("inactive enum: got %q, %v", got, err)
	}
}

func TestEnumErrors(t *testing.T) {
	for _, src := range []string{
		"enum Color\nmes(1)\n",              // missing brace
		"enum Color { Red\nmes(1)\n",        // unclosed
		"enum { }\nmes(1)\n",                // empty
		"enum { 1A }\nmes(1)\n",             // bad member name
		"enum { A = x }\nmes(1)\n",          // non-integer value
		"enum { A, A }\nmes(1)\n",           // duplicate member
		"#define A 1\nenum { A }\nmes(1)\n", // #define names cannot be members
		"#enum Color { Red }\nmes(1)\n",     // #enum directive is rejected
	} {
		if _, err := runParsePP(src); err == nil {
			t.Fatalf("src %q: want enum error, got nil", src)
		}
	}
}

func TestIfdef(t *testing.T) {
	got, err := runSrcPP(t, "#define FOO 1\n#ifdef FOO\nmes(\"yes\")\n#else\nmes(\"no\")\n#endif\n")
	if err != nil || got != "yes\n" {
		t.Fatalf("ifdef taken: got %q, %v", got, err)
	}
	got, err = runSrcPP(t, "#ifdef MISSING\nmes(\"yes\")\n#else\nmes(\"no\")\n#endif\n")
	if err != nil || got != "no\n" {
		t.Fatalf("ifdef skipped: got %q, %v", got, err)
	}
	got, err = runSrcPP(t, "#ifndef MISSING\nmes(\"n\")\n#endif\n#ifdef MISSING\nmes(\"y\")\n#endif\n")
	if err != nil || got != "n\n" {
		t.Fatalf("ifndef: got %q, %v", got, err)
	}
	// Nesting.
	got, err = runSrcPP(t, "#define A 1\n#ifdef A\n#ifdef B\nmes(\"ab\")\n#else\nmes(\"a\")\n#endif\n#else\nmes(\"none\")\n#endif\n")
	if err != nil || got != "a\n" {
		t.Fatalf("nested: got %q, %v", got, err)
	}
	// Inactive regions are fully skipped: missing includes and bad
	// defines inside them must not error.
	got, err = runSrcPP(t, "#ifdef MISSING\n#include \"no-such-file.gsh\"\n#define BAD 1 +\nmes(\"x\")\n#else\nmes(\"ok\")\n#endif\n")
	if err != nil || got != "ok\n" {
		t.Fatalf("inactive skip: got %q, %v", got, err)
	}
}

func TestConditionalErrors(t *testing.T) {
	for _, src := range []string{
		"#else\n", "#endif\n", // stray
		"#ifdef A\n#else\n#else\n#endif\n", // duplicate else
		"#ifdef A\nmes(1)\n",               // unclosed
		"#ifdef A\n",                       // unclosed (empty body)
		"#ifdef\n", "#ifdef A B\n",         // malformed
		"#else x\n", "#endif x\n", // trailing garbage
	} {
		if _, err := runParsePP(src); err == nil {
			t.Fatalf("src %q: want conditional error, got nil", src)
		}
	}
}

func TestErrorDirective(t *testing.T) {
	if _, err := runParsePP("#error 使えません\n"); err == nil {
		t.Fatal("active #error should fail")
	} else if !strings.Contains(err.Error(), "使えません") {
		t.Fatalf("want custom message, got %v", err)
	}
	// Inactive #error is ignored.
	got, err := runSrcPP(t, "#ifdef MISSING\n#error boom\n#endif\nmes(\"ok\")\n")
	if err != nil || got != "ok\n" {
		t.Fatalf("inactive #error: got %q, %v", got, err)
	}
}

func TestDefineAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	// Library constant visible to the main file (defined before include).
	writeFile(t, filepath.Join(dir, "lib.gsh"), "mes(LIBVAL)\n")
	writeFile(t, filepath.Join(dir, "main.gsh"), "#define LIBVAL 42\n#include \"lib.gsh\"\n")
	got, err := runFile(t, filepath.Join(dir, "main.gsh"))
	if err != nil || got != "42\n" {
		t.Fatalf("lib use: got %q, %v", got, err)
	}
	// Main-file define visible inside the library (defined at include).
	writeFile(t, filepath.Join(dir, "lib2.gsh"), "mes(VAL2 * 2)\n")
	writeFile(t, filepath.Join(dir, "main2.gsh"), "#define VAL2 21\n#include \"lib2.gsh\"\n")
	got, err = runFile(t, filepath.Join(dir, "main2.gsh"))
	if err != nil || got != "42\n" {
		t.Fatalf("lib2 use: got %q, %v", got, err)
	}
	// Line order: a define after the include must not rewrite the library.
	writeFile(t, filepath.Join(dir, "lib3.gsh"), "mes(LATE)\n")
	writeFile(t, filepath.Join(dir, "main3.gsh"), "#include \"lib3.gsh\"\n#define LATE 1\n")
	if _, err := runFile(t, filepath.Join(dir, "main3.gsh")); err == nil {
		t.Fatal("late define must not reach the library")
	}
}

func runSrcPP(t *testing.T, src string) (string, error) {
	t.Helper()
	prog, err := runParsePP(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	err = in.Run(prog)
	return buf.String(), err
}

func runParsePP(src string) (*Program, error) {
	toks, err := CombineSource("", src, "")
	if err != nil {
		return nil, err
	}
	return ParseTokens(toks)
}

func TestCombineSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lib.gsh"), "v = 42\n")
	toks, err := CombineSource("", "#include \""+filepath.Join(dir, "lib.gsh")+"\"\nmes(v)\n", "")
	if err != nil {
		t.Fatalf("combine: %v", err)
	}
	prog, err := ParseTokens(toks)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	if err := in.Run(prog); err != nil {
		t.Fatalf("run: %v", err)
	}
	if buf.String() != "42\n" {
		t.Fatalf("got %q", buf.String())
	}
	// Display "" keeps legacy positions (no file prefix).
	if _, err := CombineSource("", "#bogus\n", ""); err == nil || !strings.Contains(err.Error(), "不明なディレクティブ") {
		t.Fatalf("want directive error, got %v", err)
	}
}
