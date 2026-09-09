package main

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", src, err)
	}
	return prog
}

func mustFailParse(t *testing.T, src string, substr string) {
	t.Helper()
	_, err := Parse(src)
	if err == nil {
		t.Fatalf("Parse(%q): expected error containing %q, got nil", src, substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("Parse(%q) error %q does not contain %q", src, err.Error(), substr)
	}
}

func TestParsePrecedence(t *testing.T) {
	prog := mustParse(t, "x = 10 + 2 * 3\n")
	as := prog.Stmts[0].(*AssignStmt)
	// (10 + (2 * 3))
	bin, ok := as.Value.(*BinaryExpr)
	if !ok || bin.Op != TokPlus {
		t.Fatalf("top op should be +, got %s", as.Value.String())
	}
	if _, ok := bin.R.(*BinaryExpr); !ok {
		t.Fatalf("right side should be 2*3, got %s", bin.R.String())
	}
}

func TestParseParens(t *testing.T) {
	prog := mustParse(t, "x = (10 + 2) * 3\n")
	as := prog.Stmts[0].(*AssignStmt)
	if as.Value.String() != "((10 + 2) * 3)" {
		t.Fatalf("got %s", as.Value.String())
	}
}

func TestParseElseIfChain(t *testing.T) {
	prog := mustParse(t, "if x > 10 {\nmes(1)\n} else if x > 5 {\nmes(2)\n} else {\nmes(3)\n}\n")
	ifs := prog.Stmts[0].(*IfStmt)
	chained, ok := ifs.Else.(*IfStmt)
	if !ok {
		t.Fatalf("else-if should chain, got %T", ifs.Else)
	}
	if _, ok := chained.Else.(*BlockStmt); !ok {
		t.Fatalf("final else should be block, got %T", chained.Else)
	}
}

func TestParseRepeatForms(t *testing.T) {
	prog := mustParse(t, "repeat 10 {\nmes(1)\n}\nrepeat 10 as i {\nmes(i)\n}\n")
	r1 := prog.Stmts[0].(*RepeatStmt)
	if r1.HasVar {
		t.Fatal("first repeat should have no counter")
	}
	r2 := prog.Stmts[1].(*RepeatStmt)
	if !r2.HasVar || r2.Var != "i" {
		t.Fatalf("second repeat should have counter i: %+v", r2)
	}
	prog = mustParse(t, "repeat a as i, x {\nmes(x)\n}\n")
	r3 := prog.Stmts[0].(*RepeatStmt)
	if !r3.HasVar || !r3.HasItem || r3.Var != "i" || r3.Item != "x" {
		t.Fatalf("index+item repeat: %+v", r3)
	}
	if got := r3.String(); got != "repeat a as i, x {\n  mes(x)\n}" {
		t.Fatalf("bad rendering: %q", got)
	}
}

func TestParseWhile(t *testing.T) {
	prog := mustParse(t, "while x < 10 {\nmes(x)\n}\n")
	w := prog.Stmts[0].(*WhileStmt)
	if w.Cond == nil || w.Body == nil || len(w.Body.Stmts) != 1 {
		t.Fatalf("bad while node: %+v", w)
	}
	if w.String() != "while (x < 10) {\n  mes(x)\n}" {
		t.Fatalf("bad while rendering: %q", w.String())
	}
}

func TestParseSwitch(t *testing.T) {
	prog := mustParse(t, "switch x {\ncase 1, 2 {\nmes(1)\n}\ndefault {\nmes(0)\n}\n}\n")
	sw := prog.Stmts[0].(*SwitchStmt)
	if len(sw.Cases) != 2 {
		t.Fatalf("want 2 cases, got %+v", sw)
	}
	if sw.Cases[0].Default || len(sw.Cases[0].Values) != 2 {
		t.Fatalf("bad first case: %+v", sw.Cases[0])
	}
	if !sw.Cases[1].Default {
		t.Fatalf("second case should be default: %+v", sw.Cases[1])
	}
	want := "switch x {\ncase 1, 2 {\n  mes(1)\n}\ndefault {\n  mes(0)\n}\n}"
	if sw.String() != want {
		t.Fatalf("bad switch rendering: %q", sw.String())
	}
}

func TestParseBitwisePrecedence(t *testing.T) {
	// Modernized order (Python/Rust-style): & | ^ all bind tighter
	// than ==; within bitwise | loosest, then ^, then &.
	// << >> tighter than relational, looser than +.
	for _, tc := range []struct{ src, want string }{
		{"mes(1 | 2 & 3)\n", "(1 | (2 & 3))"},
		{"mes(6 ^ 3 | 1)\n", "((6 ^ 3) | 1)"},
		{"mes(1 & 1 == 1)\n", "((1 & 1) == 1)"},
		{"mes(1 == 2 & 3)\n", "(1 == (2 & 3))"},
		{"mes(1 << 2 + 1)\n", "(1 << (2 + 1))"},
		{"mes(1 < 2 << 3)\n", "(1 < (2 << 3))"},
		{"mes(~5)\n", "(~5)"},
		{"mes(1 | 2 && 0)\n", "((1 | 2) && 0)"},
	} {
		prog := mustParse(t, tc.src)
		es := prog.Stmts[0].(*ExprStmt)
		call := es.X.(*CallExpr)
		if got := call.Args[0].String(); got != tc.want {
			t.Fatalf("src %q: got %s, want %s", tc.src, got, tc.want)
		}
	}
}

func TestParseCompoundAssign(t *testing.T) {
	// `x op= v` desugars to `x = (x op v)`.
	for _, tc := range []struct{ src, want string }{
		{"x += 1\n", "x = (x + 1)"},
		{"x -= 1\n", "x = (x - 1)"},
		{"x *= 2\n", "x = (x * 2)"},
		{"x /= 2\n", "x = (x / 2)"},
		{"x %= 2\n", "x = (x % 2)"},
		{"x &= 3\n", "x = (x & 3)"},
		{"x |= 3\n", "x = (x | 3)"},
		{"x ^= 3\n", "x = (x ^ 3)"},
		{"x <<= 1\n", "x = (x << 1)"},
		{"x >>= 1\n", "x = (x >> 1)"},
		{"a[0] += 5\n", "a[0] = (a[0] + 5)"},
	} {
		prog := mustParse(t, tc.src)
		if got := prog.Stmts[0].String(); got != tc.want {
			t.Fatalf("src %q: got %s, want %s", tc.src, got, tc.want)
		}
	}
	mustFailParse(t, "1 += 2\n", "代入できません")
	mustFailParse(t, "x += \n", "式が必要です")
}

func TestParseTry(t *testing.T) {
	prog := mustParse(t, "try {\nmes(1)\n} catch(e) {\nmes(e)\n}\n")
	ts := prog.Stmts[0].(*TryStmt)
	if ts.Var != "e" || len(ts.Body.Stmts) != 1 || len(ts.Catch.Stmts) != 1 {
		t.Fatalf("bad try node: %+v", ts)
	}
	if got := ts.String(); got != "try {\n  mes(1)\n} catch(e) {\n  mes(e)\n}" {
		t.Fatalf("bad try rendering: %q", got)
	}
	mustFailParse(t, "try {\nmes(1)\n}\n", "catch")
	mustFailParse(t, "try {\nmes(1)\n} catch() {\n}\n", "変数名")
}

func TestParseMultilineArray(t *testing.T) {
	prog := mustParse(t, "a = [\n[1, 2, 3],\n[4, 5, 6],\n]\n")
	as := prog.Stmts[0].(*AssignStmt)
	al := as.Value.(*ArrayLit)
	if len(al.Elems) != 2 {
		t.Fatalf("got %d elems", len(al.Elems))
	}
}

func TestParseMapDot(t *testing.T) {
	prog := mustParse(t, "m = {\"a\": 1}\nx = m.a\n")
	as := prog.Stmts[1].(*AssignStmt)
	fe, ok := as.Value.(*FieldExpr)
	if !ok || fe.Field != "a" {
		t.Fatalf("m.a should parse as field: %T", as.Value)
	}
	mustFailParse(t, "p = Person{\nname: \"A\",\n}\n", "予期しない '{'")
}

func TestParseMapLit(t *testing.T) {
	prog := mustParse(t, "m = {\"a\": 1, \"b\": x}\n")
	as := prog.Stmts[0].(*AssignStmt)
	if got := as.Value.String(); got != `{"a": 1, "b": x}` {
		t.Fatalf("got %s", got)
	}
	prog = mustParse(t, "m = {}\n")
	if got := prog.Stmts[0].(*AssignStmt).Value.String(); got != "{}" {
		t.Fatalf("got %s", got)
	}
	mustFailParse(t, "m = {\"a\" 1}\n", "キー : 値")
	mustFailParse(t, "m = {\"a\": 1\n", "'}'")
}

func TestParseConditionWithBraceVar(t *testing.T) {
	// `if x {` must be condition + block.
	prog := mustParse(t, "x = true\nif x {\nmes(1)\n}\n")
	ifs := prog.Stmts[1].(*IfStmt)
	if _, ok := ifs.Cond.(*VarExpr); !ok {
		t.Fatalf("cond: %T", ifs.Cond)
	}
}

func TestParseRejections(t *testing.T) {
	mustFailParse(t, "x = 1;\n", "セミコロン")
	mustFailParse(t, "enum Color\n", "不正な enum")
	mustFailParse(t, "enum { }\n", "不正な enum")
	mustFailParse(t, "enum C { A }\nenum C { A }\n", "既に定義")
	mustFailParse(t, "*label\n", "*")
	mustFailParse(t, "a = [1, 2\n", "']'")
	mustFailParse(t, "if x {\nmes(1)\n", "ブロックが閉じられていません")
	mustFailParse(t, "x = (1 + 2\n", "RPAREN")
}

func TestParseCallVsIndex(t *testing.T) {
	// a(0) parses as a call; the interpreter rejects it with a [...] hint.
	prog := mustParse(t, "a(0)\n")
	es := prog.Stmts[0].(*ExprStmt)
	if _, ok := es.X.(*CallExpr); !ok {
		t.Fatalf("a(0) should parse as call: %T", es.X)
	}
	prog = mustParse(t, "a[0][1]\n")
	es = prog.Stmts[0].(*ExprStmt)
	if _, ok := es.X.(*IndexExpr); !ok {
		t.Fatalf("a[0][1] should parse as index: %T", es.X)
	}
}
