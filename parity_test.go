// tree/VM バックエンドの差分パリティテスト。
//
// 同一スクリプトを両バックエンドで実行し、標準出力・エラー文言
// （位置つき）・終了コードの完全一致を検証する。
package main

import (
	"bytes"
	"strings"
	"testing"
)

type parityResult struct {
	stdout   string
	errStr   string
	exitCode int
	hasExit  bool
}

// runParityOne は src を指定バックエンドで実行する。
func runParityOne(src, backend string) parityResult {
	var r parityResult
	prog, err := Parse(src)
	if err != nil {
		r.errStr = "parse: " + err.Error()
		return r
	}
	var buf bytes.Buffer
	in := NewInterpWithIO(&buf, strings.NewReader(""))
	if backend == "vm" {
		vprog, verr := Compile(prog)
		if verr != nil {
			r.errStr = "compile: " + verr.Error()
			r.stdout = buf.String()
			return r
		}
		err = newVmachine(in).runMain(vprog)
	} else {
		err = in.Run(prog)
	}
	r.stdout = buf.String()
	if err != nil {
		r.errStr = err.Error()
	}
	if code, ok := in.ExitCode(); ok {
		r.exitCode = code
		r.hasExit = true
	}
	return r
}

func checkParity(t *testing.T, src string) {
	t.Helper()
	a := runParityOne(src, "tree")
	b := runParityOne(src, "vm")
	if a.stdout != b.stdout || a.errStr != b.errStr || a.exitCode != b.exitCode || a.hasExit != b.hasExit {
		t.Fatalf("src %q:\n tree: out=%q err=%q exit=%d(%v)\n vm:   out=%q err=%q exit=%d(%v)",
			src, a.stdout, a.errStr, a.exitCode, a.hasExit, b.stdout, b.errStr, b.exitCode, b.hasExit)
	}
}

// parityCases は両バックエンドで一致すべきスクリプト集。
// 正常系・エラー系（文言＋位置）・終了コードを網羅する。
var parityCases = []string{
	// 基本・演算
	"let x = 10 + 2 * 3\nmes(x)\n",
	"mes((10 + 2) * 3)\n",
	"mes(10 % 3)\nmes(7 / 2)\nmes(7.0 / 2)\n",
	"mes(-5 + 8)\nmes(!true)\n",
	"mes(1 + 2.5)\nmes(10 - 3.5)\nmes(2 * 1.5)\n",
	"mes(1 / 0)\n",
	"mes(1.0 / 0.0)\n",
	"mes(1 % 0)\n",
	"mes(1.5 % 2)\n",
	"mes(1 + true)\n",
	"mes(true + false)\n",
	"mes(1 - true)\nmes(2 * \"a\")\n",
	"mes(1 & 3)\nmes(1 | 2)\nmes(1 ^ 3)\nmes(1 << 4)\nmes(256 >> 3)\n",
	"mes(1.5 & 1)\nmes(1 << -1)\nmes(1 << 64)\n",
	"mes(1 < 2)\nmes(\"a\" < \"b\")\nmes(1 == 1.0)\nmes(1 != 2)\n",
	"mes(1 < \"a\")\n",
	"mes([1,2] == [1,2])\n",
	"let a = [1,2]\nlet b = a\nmes(a == b)\n",
	"mes(-\"a\")\nmes(~1.5)\nmes(~7)\n",
	"def void_fn() {\nreturn\n}\nmes(1 == void_fn())\n",
	// 文字列結合・void
	"let name = \"Alice\"\nmes(\"Hello, \" + name)\n",
	"mes(\"n=\" + 42)\nmes(1 + \"a\" + 2)\n",
	"def void_fn() {\nreturn\n}\nmes(\"a\" + void_fn())\n",
	"def void_fn() {\nreturn\n}\nlet x = void_fn()\n",
	"def void_fn() {\nreturn\n}\nmes(void_fn(), \"x\")\n",
	// 条件・論理
	"if 1 < 2 && 2 < 3 || false {\nmes(\"y\")\n}\n",
	"if 1 {\nmes(1)\n}\n",
	"let x = 1 && true\n",
	"mes(!1)\n",
	"mes(true && false)\nmes(false || 1 < 2)\n",
	"def void_fn() {\nreturn\n}\nmes(true || void_fn())\nmes(false && void_fn())\n",
	"def void_fn() {\nreturn\n}\nmes(true && void_fn())\n",
	"let x = 3\nif x > 10 {\nmes(1)\n} else if x > 5 {\nmes(2)\n} else {\nmes(3)\n}\n",
	"let i = 0\nwhile i < 3 {\nmes(i)\ni += 1\n}\n",
	"while 1 {\nmes(\"x\")\n}\n",
	// repeat
	"repeat 3 as i {\nmes(i)\n}\n",
	"repeat 3 {\nmes(\"a\")\n}\n",
	"repeat 0 as i {\nmes(i)\n}\nmes(\"done\")\n",
	"repeat -1 {\nmes(1)\n}\n",
	"repeat \"s\" {\nmes(1)\n}\n",
	"repeat 3 as i, x {\nmes(i)\n}\n",
	"let a = [10, 20, 30]\nrepeat a as x {\nmes(x)\n}\n",
	"let a = [10, 20, 30]\nrepeat a as i, x {\nmes(i)\nmes(x)\n}\n",
	"let a = [1,2]\nrepeat a as x {\npush(a, 99)\nmes(x)\n}\nmes(length(a))\n",
	"repeat 3 as i {\ni = 99\n}\n",
	"let x = 99\nrepeat 3 as x {\nmes(x)\n}\nmes(x)\n",
	"repeat 2 as i {\nrepeat 2 as i {\nmes(i)\n}\n}\n",
	"repeat 5 as i {\nif i == 2 {\nbreak\n}\nmes(i)\n}\n",
	"repeat 5 as i {\nif i == 2 {\ncontinue\n}\nmes(i)\n}\n",
	// switch
	"let x = 2\nswitch x {\ncase 1 {\nmes(1)\n}\ncase 2, 3 {\nmes(23)\n}\ndefault {\nmes(\"d\")\n}\n}\n",
	"let x = 9\nswitch x {\ncase 1 {\nmes(1)\n}\ndefault {\nmes(\"d\")\n}\n}\n",
	"let x = 1\nswitch x {\ncase 1 {\nmes(1)\n}\n}\nmes(\"after\")\n",
	"def void_fn() {\nreturn\n}\nswitch void_fn() {\ncase 1 {\nmes(1)\n}\n}\n",
	"def void_fn() {\nreturn\n}\nswitch 1 {\ncase void_fn() {\nmes(1)\n}\n}\n",
	"let i = 0\nrepeat 3 as k {\nswitch k {\ncase 1 {\nbreak\n}\n}\nmes(k)\n}\n",
	"let i = 0\nrepeat 3 as k {\nswitch k {\ncase 1 {\ncontinue\n}\n}\nmes(k)\n}\n",
	"let n = 0\ndef f_inc() {\nn = n + 1\nreturn n\n}\nswitch f_inc() {\ncase f_inc() {\nmes(\"a\")\n}\ncase 2 {\nmes(\"b\")\n}\n}\nmes(n)\n",
	// 関数・スコープ
	"def add(a, b) {\nreturn a + b\n}\nmes(add(2, 3))\n",
	"mes(add(1, 2))\ndef add(a, b) {\nreturn a + b\n}\n",
	"def f(a) {\nreturn a\n}\nmes(f(1, 2))\n",
	"def f(a, b) {\nreturn a\n}\nmes(f(1))\n",
	"def f(a, a) {\nreturn a\n}\n",
	"def mes(x) {\nreturn x\n}\n",
	"def f() {\nreturn 1\n}\ndef f() {\nreturn 2\n}\n",
	"let g = 10\ndef f() {\ng = 20\n}\nf()\nmes(g)\n",
	"let g = 10\ndef f() {\nlet q = 30\n}\nf()\nmes(q)\n",
	"def f(x) {\nx = x + 1\nreturn x\n}\nmes(f(5))\n",
	"def fact(n) {\nif n <= 1 {\nreturn 1\n}\nreturn n * fact(n - 1)\n}\nmes(fact(10))\n",
	"def f() {\nreturn\n}\nmes(\"r\")\nf()\nmes(\"s\")\n",
	"def f() {\n}\nmes(\"void test\")\nlet x = f()\n",
	"def void_fn() {\nreturn\n}\ndef f() {\nreturn void_fn()\n}\nmes(f())\n",
	"let x = 5\nmes(x(1))\n",
	"let a = [1]\nmes(a(0))\n",
	"mes(nope(1))\n",
	"mes(1 + 2 * unknown_fn(3))\n",
	"let f = 5\ndef g() {\nreturn f\n}\nmes(g())\n",
	// let
	"let x = 1\nmes(x)\n",
	"let x = 1\nlet x = 2\nmes(x)\n",
	"let x = 1\ndef f() {\nlet x = 2\nmes(x)\n}\nf()\nmes(x)\n",
	"let x = 1\ndef f() {\nx = 2\n}\nf()\nmes(x)\n",
	"def f() {\nlet y = 3\n}\nf()\nmes(y)\n",
	"if true {\nlet z = 9\n}\nmes(z)\n",
	"repeat 3 as i {\nlet t = i * 2\nmes(t)\n}\n",
	"let mes = 1\n",
	// 配列
	"let a = [1, 2, 3]\nmes(a[1])\nmes(length(a))\n",
	"let a = [1]\nmes(a[5])\n",
	"let a = [1]\nmes(a[-1])\n",
	"let a = [1]\nmes(a[0.0])\n",
	"let a = [1]\na[0] = 99\nmes(a[0])\n",
	"let a = [1]\na[3] = 99\n",
	"let a = [[1, 2], [3, 4]]\nmes(a[1][0])\na[0][1] = 99\nmes(a[0][1])\n",
	"let a = [[1, 2], [3, 4]]\na[0][5] = 1\n",
	"let a = [[1, 2], 5]\na[0][1] = 9\n",
	"let a = [1, 2]\ndef f_idx() {\nreturn 0\n}\na[f_idx()] = 9\nmes(a[0])\n",
	"let b = [0]\nlet c = 0\ndef f_idx() {\nc = c + 1\nreturn 1\n}\nlet a = [7, 8]\na[f_idx()] += 10\nmes(a[1])\nmes(c)\n",
	"def void_fn() {\nreturn\n}\nlet a = [void_fn()]\n",
	"let x = 0\nx += 5\nmes(x)\n",
	"let x = 3\nx *= 4\nmes(x)\n",
	"y += 1\n",
	"let mes = 1\n",
	"let a = [1,2,3]\nlet i = 1\na[i] += 10\nmes(a[1])\n",
	// 未定義・トップレベル制御
	"mes(zzz_undefined)\n",
	"break\n",
	"continue\n",
	"return 1\n",
	"return\n",
	"while true {\nbreak\n}\nmes(\"ok\")\n",
	"repeat 3 as i {\nbreak\n}\nmes(\"ok\")\n",
	"def f() {\nwhile true {\nbreak\n}\nreturn 7\n}\nmes(f())\n",
	// 組込
	"mes(length([1,2,3]))\nmes(len(\"abc\"))\n",
	"mes()\n",
	"end(3)\nmes(\"no\")\n",
	"end()\n",
	"let x = 1\nend(0)\n",
	"assert(true)\nmes(\"ok\")\n",
	"assert(false, \"boom\")\n",
	"mes(int(\"3.9\"))\n",
	"mes(float(\"nan\"))\n",
	"mes(1e308 * 10)\n",
	"let a = [3, 1, 2]\nsort(a)\nmes(a[0])\n",
	"let s = strmid(\"hello\", 1, 3)\nmes(s)\n",
	"mes(strmid(\"hi\", 0, 99))\n",
	"mes(instr(\"hello\", \"l\", 99))\n",
	"def d2(a) {\nreturn a * 2\n}\nmes(d2(d2(21)))\n",
	"let i = 1\nlet s = \"a\" + i + [1][0]\nmes(s)\n",
	"let a = [1,2,3,4]\nlet s = 0\nrepeat a as x {\ns += x\n}\nmes(s)\n",
	"let x = 0\nrepeat 100 as i {\nx = x + i\n}\nmes(x)\n",
	// 単一加算ループ（REPINC 融合の対象形）。
	"let i = 0\nrepeat 5 {\ni += 1\n}\nmes(i)\n",
	"let i = 0\nrepeat 100000 {\ni += 1\n}\nmes(i)\n",
	"let i = 10\nrepeat 5 {\ni -= 3\n}\nmes(i)\n",
	"let i = 0\nrepeat 0 {\ni += 1\n}\nmes(i)\n",
	"let i = 0\nrepeat 1 {\ni += 1\n}\nmes(i)\n",
	"repeat 3 {\nzzz_repinc_undef += 1\n}\n",
	"let s = \"a\"\nrepeat 3 {\ns += 1\n}\nmes(s)\n",
	"let x = 1.5\nrepeat 3 {\nx += 2\n}\nmes(x)\n",
	"let b = true\nrepeat 2 {\nb += 1\n}\n",
	"let i = 9223372036854775800\nrepeat 3 {\ni += 5\n}\nmes(i)\n",
	"let i = 0\nrepeat 3 {\nrepeat 2 {\ni += 1\n}\n}\nmes(i)\n",
	"def f_repinc() {\nlet t = 0\nrepeat 5 {\nt += 1\n}\nreturn t\n}\nmes(f_repinc())\n",
	"let i = 0\nlet x = 2\nrepeat 3 {\ni += x\n}\nmes(i)\n",
	"repeat 2 as g_repinc {\nrepeat 3 {\ng_repinc += 1\n}\n}\n",
	"let i = 0\nrepeat 3 as k {\ni += 1\n}\nmes(i)\nmes(k)\n",
}

func TestParityInline(t *testing.T) {
	for _, src := range parityCases {
		checkParity(t, src)
	}
}

// TestParityFiles は CLI 作例ファイルの一致を検証する（GUI・対話・長時間は除外）。
func TestParityFiles(t *testing.T) {
	files := []string{
		"examples/hello.gsh",
		"examples/builtins.gsh",
		"examples/edge.gsh",
		"examples/sample19.gsh",
		"examples/define.gsh",
		"examples/include_demo.gsh",
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			a := runParityFile(t, f, "tree")
			b := runParityFile(t, f, "vm")
			if a.stdout != b.stdout || a.errStr != b.errStr || a.exitCode != b.exitCode || a.hasExit != b.hasExit {
				t.Fatalf("file %s:\n tree: out=%q err=%q exit=%d(%v)\n vm:   out=%q err=%q exit=%d(%v)",
					f, a.stdout, a.errStr, a.exitCode, a.hasExit, b.stdout, b.errStr, b.exitCode, b.hasExit)
			}
		})
	}
}

func runParityFile(t *testing.T, file, backend string) parityResult {
	t.Helper()
	var r parityResult
	prog, err := ParseFile(file)
	if err != nil {
		r.errStr = "parse: " + err.Error()
		return r
	}
	var buf bytes.Buffer
	in := NewInterpWithIO(&buf, strings.NewReader(""))
	in.SetScriptDir(scriptDirOf(file))
	if backend == "vm" {
		vprog, verr := Compile(prog)
		if verr != nil {
			r.errStr = "compile: " + verr.Error()
			r.stdout = buf.String()
			return r
		}
		err = newVmachine(in).runMain(vprog)
	} else {
		err = in.Run(prog)
	}
	r.stdout = buf.String()
	if err != nil {
		r.errStr = err.Error()
	}
	if code, ok := in.ExitCode(); ok {
		r.exitCode = code
		r.hasExit = true
	}
	return r
}
