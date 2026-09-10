package main

import (
	"bytes"
	"strings"
	"testing"
)

// runSrc executes src and returns mes() output.
// Both parse-time and runtime errors are returned as error (not fatal) so
// rejection tests can cover either phase.
func runSrc(t *testing.T, src string) (string, error) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	in := NewInterp(&buf)
	err = in.Run(prog)
	return buf.String(), err
}

func mustOut(t *testing.T, src, want string) {
	t.Helper()
	got, err := runSrc(t, src)
	if err != nil {
		t.Fatalf("src %q: runtime error: %v", src, err)
	}
	if got != want {
		t.Fatalf("src %q:\n got %q\nwant %q", src, got, want)
	}
}

func mustErr(t *testing.T, src, substr string) {
	t.Helper()
	_, err := runSrc(t, src)
	if err == nil {
		t.Fatalf("src %q: expected error containing %q, got nil", src, substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("src %q: error %q does not contain %q", src, err.Error(), substr)
	}
}

func TestArithmetic(t *testing.T) {
	mustOut(t, "x = 10 + 2 * 3\nmes(x)\n", "16\n")
	mustOut(t, "mes((10 + 2) * 3)\n", "36\n")
	mustOut(t, "mes(10 % 3)\n", "1\n")
	mustOut(t, "mes(7 / 2)\nmes(7.0 / 2)\n", "3\n3.5\n")
	mustOut(t, "mes(-5 + 8)\nmes(!true)\n", "3\nfalse\n")
	mustOut(t, "mes(1 + 2.5)\n", "3.5\n")
	mustErr(t, "mes(1 / 0)\n", "0 による除算")
	mustErr(t, "mes(1 % 0)\n", "0 による剰余")
	mustErr(t, "mes(1.5 % 2)\n", "整数が必要です")
	mustErr(t, "mes(1 + true)\n", "加算できません")
}

func TestStrings(t *testing.T) {
	mustOut(t, "name = \"Alice\"\nmes(\"Hello, \" + name)\n", "Hello, Alice\n")
	mustOut(t, "mes(\"n=\" + 42)\n", "n=42\n")
	mustOut(t, "mes(\"a\" < \"b\")\n", "true\n")
}

func TestStrictConditions(t *testing.T) {
	mustOut(t, "if 1 < 2 && 2 < 3 || false {\nmes(\"y\")\n}\n", "y\n")
	mustErr(t, "if 1 {\nmes(1)\n}\n", "bool 型")
	mustErr(t, "if \"s\" {\nmes(1)\n}\n", "bool 型")
	mustErr(t, "x = 1 && true\n", "bool 型")
	mustErr(t, "mes(!1)\n", "bool 型")
}

func TestIfElse(t *testing.T) {
	src := "x = 7\nif x > 10 {\nmes(\"large\")\n} else if x > 5 {\nmes(\"middle\")\n} else {\nmes(\"small\")\n}\n"
	mustOut(t, src, "middle\n")
	mustOut(t, "x = 3\nif x > 10 {\nmes(1)\n} else {\nmes(2)\n}\n", "2\n")
}

func TestRepeatCounter(t *testing.T) {
	mustOut(t, "repeat 3 as i {\nmes(i)\n}\n", "0\n1\n2\n")
	mustOut(t, "s = 0\nrepeat 10 as i {\ns = s + i\n}\nmes(s)\n", "45\n")
	mustErr(t, "repeat 2 as i {\nmes(i)\n}\nmes(i)\n", "未定義の変数")
	mustErr(t, "repeat 2 as i {\ni = 100\n}\n", "読み取り専用")
	mustErr(t, "repeat 2 as i {\ndef i() {\n}\n}\n", "読み取り専用")
	mustErr(t, "repeat true {\nmes(1)\n}\n", "整数または配列")
	mustErr(t, "repeat -1 {\nmes(1)\n}\n", "0 以上")
}

func TestBreakContinue(t *testing.T) {
	mustOut(t, "s = -1\nrepeat 100 as i {\nif i == 10 {\nbreak\n}\ns = i\n}\nmes(s)\n", "9\n")
	mustOut(t, "repeat 10 as i {\nif i % 2 == 0 {\ncontinue\n}\nmes(i)\n}\n", "1\n3\n5\n7\n9\n")
	mustErr(t, "break\n", "ループ外の break")
	mustErr(t, "continue\n", "ループ外の continue")
	mustErr(t, "repeat 2 as i {\nmes(i)\n}\nmes(i)\n", "未定義の変数")
}

func TestWhile(t *testing.T) {
	mustOut(t, "i = 0\nwhile i < 3 {\nmes(i)\ni = i + 1\n}\n", "0\n1\n2\n")
	mustOut(t, "while false {\nmes(1)\n}\nmes(0)\n", "0\n")
	mustOut(t, "i = 0\ns = 0\nwhile true {\nif i >= 5 {\nbreak\n}\ns = s + i\ni = i + 1\n}\nmes(s)\n", "10\n")
	mustOut(t, "i = 0\nwhile i < 10 {\ni = i + 1\nif i % 2 == 0 {\ncontinue\n}\nmes(i)\n}\n", "1\n3\n5\n7\n9\n")
	mustOut(t, "i = 0\nwhile i < 3 {\nj = 0\nwhile j < 2 {\nmes(i * 10 + j)\nj = j + 1\n}\ni = i + 1\n}\n", "0\n1\n10\n11\n20\n21\n")
	mustOut(t, "i = 0\nwhile i < 5 {\ni = i + 1\nif i == 2 {\nbreak\n}\n}\nmes(i)\n", "2\n")
	mustErr(t, "while 1 {\nmes(1)\n}\n", "条件式は bool 型")
	mustErr(t, "while \"x\" {\n}\n", "条件式は bool 型")
}

func TestSwitch(t *testing.T) {
	mustOut(t, "x = 2\nswitch x {\ncase 1 {\nmes(\"one\")\n}\ncase 2 {\nmes(\"two\")\n}\ndefault {\nmes(\"other\")\n}\n}\n", "two\n")
	mustOut(t, "x = 2\nswitch x {\ncase 1, 2, 3 {\nmes(\"few\")\n}\ndefault {\nmes(\"many\")\n}\n}\n", "few\n")
	mustOut(t, "x = 9\nswitch x {\ncase 1 {\nmes(\"one\")\n}\n}\nmes(\"done\")\n", "done\n")
	mustOut(t, "x = 9\nswitch x {\ncase 1 {\nmes(\"one\")\n}\ndefault {\nmes(\"other\")\n}\n}\n", "other\n")
	// First match wins; no fallthrough.
	mustOut(t, "x = 1\nswitch x {\ncase 1 {\nmes(\"a\")\n}\ncase 1 {\nmes(\"b\")\n}\n}\n", "a\n")
	mustOut(t, "x = 1\nswitch x {\ncase 1 {\nmes(\"a\")\nbreak\nmes(\"unreached\")\n}\ndefault {\nmes(\"d\")\n}\n}\nmes(\"after\")\n", "a\nafter\n")
	// break in a switch inside a loop ends the switch, not the loop.
	mustOut(t, "i = 0\nrepeat 3 {\nswitch i {\ncase 1 {\nbreak\n}\ndefault {\nmes(i)\n}\n}\ni = i + 1\n}\n", "0\n2\n")
	// continue passes through the switch to the loop.
	mustOut(t, "repeat 3 as i {\nswitch i {\ncase 1 {\ncontinue\n}\ndefault {\nmes(i)\n}\n}\n}\n", "0\n2\n")
	// break inside repeat inside case ends the repeat.
	mustOut(t, "x = 1\nswitch x {\ncase 1 {\ns = -1\nrepeat 10 as i {\nif i == 2 {\nbreak\n}\ns = i\n}\nmes(s)\n}\n}\n", "1\n")
	// Scrutinee evaluated once.
	mustOut(t, "n = 0\ndef next() {\nn = n + 1\nreturn n\n}\nswitch next() {\ncase 1 {\nmes(\"one\")\n}\ndefault {\nmes(\"other\")\n}\n}\nmes(n)\n", "one\n1\n")
	// Type coercion and mismatch.
	mustOut(t, "x = 1.0\nswitch x {\ncase 1 {\nmes(\"int\")\n}\ndefault {\nmes(\"other\")\n}\n}\n", "int\n")
	mustOut(t, "x = 1\nswitch x {\ncase \"1\" {\nmes(\"str\")\n}\ndefault {\nmes(\"other\")\n}\n}\n", "other\n")
	mustOut(t, "s = \"b\"\nswitch s {\ncase \"a\" {\nmes(1)\n}\ncase \"b\" {\nmes(2)\n}\n}\n", "2\n")
	// Nested switch.
	mustOut(t, "x = 1\ny = 2\nswitch x {\ncase 1 {\nswitch y {\ncase 2 {\nmes(\"1-2\")\n}\ndefault {\nmes(\"1-?\")\n}\n}\n}\ndefault {\nmes(\"outer\")\n}\n}\n", "1-2\n")
	mustErr(t, "switch 1 {\ndefault {\n}\ndefault {\n}\n}\n", "default が重複")
	mustErr(t, "switch 1 {\nmes(1)\n}\n", "case または default が必要")
}

func TestBitwise(t *testing.T) {
	mustOut(t, "mes(6 & 3)\nmes(6 | 3)\nmes(6 ^ 3)\n", "2\n7\n5\n")
	mustOut(t, "mes(~0)\nmes(~5)\nmes(1 << 4)\nmes(256 >> 3)\nmes(-8 >> 2)\n", "-1\n-6\n16\n32\n-2\n")
	mustOut(t, "mes(1 | 2 & 3)\nmes(1 << 2 + 1)\nmes(8 >> 1 + 1)\n", "3\n8\n2\n")
	mustOut(t, "mes(255 & 15)\nmes(1 << 0)\nmes(0 >> 5)\n", "15\n1\n0\n")
	// Modernized precedence: & | ^ bind tighter than == (unlike C).
	mustOut(t, "mes(1 & 1 == 1)\nmes(2 | 1 == 3)\nmes(0 & 0 == 0)\n", "true\ntrue\ntrue\n")
	mustErr(t, "mes(1.5 & 1)\n", "整数が必要です")
	mustErr(t, "mes(\"a\" | 1)\n", "整数が必要です")
	mustErr(t, "mes(1 ^ 2.0)\n", "整数が必要です")
	mustErr(t, "mes(1 << 2.5)\n", "整数が必要です")
	mustErr(t, "mes(1 << -1)\n", "シフト数は 0 から 63")
	mustErr(t, "mes(1 << 64)\n", "シフト数は 0 から 63")
	mustErr(t, "mes(1 >> -2)\n", "シフト数は 0 から 63")
	mustErr(t, "mes(~1.5)\n", "整数のみ")
	mustErr(t, "mes(~\"a\")\n", "整数のみ")
}

func TestCompoundAssign(t *testing.T) {
	mustOut(t, "x = 10\nx += 5\nmes(x)\nx -= 3\nmes(x)\nx *= 2\nmes(x)\nx /= 4\nmes(x)\nx %= 5\nmes(x)\n", "15\n12\n24\n6\n1\n")
	mustOut(t, "x = 6\nx &= 3\nmes(x)\nx |= 8\nmes(x)\nx ^= 7\nmes(x)\nx <<= 2\nmes(x)\nx >>= 1\nmes(x)\n", "2\n10\n13\n52\n26\n")
	mustOut(t, "s = \"a\"\ns += \"b\"\nmes(s)\nf = 1.5\nf += 1\nmes(f)\n", "ab\n2.5\n")
	mustOut(t, "a = [1, 2]\na[0] += 10\na[1] *= 3\nmes(a)\n", "[11, 6]\n")
	mustOut(t, "p = {\"n\": 1}\np.n += 41\nmes(p.n)\n", "42\n")
	mustErr(t, "x = 1\nx += true\n", "加算できません")
	mustErr(t, "x = 1.5\nx &= 1\n", "整数が必要です")
	mustErr(t, "x = 1\nx /= 0\n", "0 による除算")
}

func TestTryCatch(t *testing.T) {
	mustOut(t, "try {\nmes(1 / 0)\n}\ncatch(e) {\nmes(\"caught\")\n}\n", "caught\n")
	mustOut(t, "try {\nmes(nosuchvar)\n}\ncatch(e) {\nmes(e)\n}\n", "2:5: 未定義の変数 \"nosuchvar\" です\n")
	mustOut(t, "try {\nmes(\"ok\")\n}\ncatch(e) {\nmes(\"bad\")\n}\n", "ok\n")
	mustOut(t, "try {\nthrow(\"boom\")\n}\ncatch(e) {\nmes(e)\n}\n", "2:6: boom\n")
	// Nested try: inner catch handles, outer never fires.
	mustOut(t, "try {\ntry {\nthrow(\"in\")\n}\ncatch(a) {\nmes(a)\n}\n}\ncatch(b) {\nmes(\"outer\")\n}\n", "3:6: in\n")
	// Uncaught errors still abort.
	mustErr(t, "try {\nthrow(\"x\")\n}\ncatch(e) {\nthrow(\"y\")\n}\n", "y")
	mustErr(t, "throw(\"late\")\n", "late")
	// Control flow passes through: break/return are not caught.
	mustOut(t, "repeat 3 as i {\ntry {\nif i == 1 {\nbreak\n}\n}\ncatch(e) {\nmes(\"bad\")\n}\nmes(i)\n}\n", "0\n")
	mustOut(t, "def f() {\ntry {\nreturn 7\n}\ncatch(e) {\nreturn 8\n}\n}\nmes(f())\n", "7\n")
	// The catch variable is scoped to the catch body.
	mustErr(t, "try {\nthrow(\"x\")\n}\ncatch(e) {\n}\nmes(e)\n", "未定義の変数")
}

func TestRepeatArray(t *testing.T) {
	mustOut(t, "repeat [10, 20, 30] as x {\nmes(x)\n}\n", "10\n20\n30\n")
	mustOut(t, "repeat [\"a\", \"b\"] as i, x {\nmes(str(i) + \":\" + x)\n}\n", "0:a\n1:b\n")
	mustOut(t, "repeat [] as x {\nmes(\"bad\")\n}\nmes(\"ok\")\n", "ok\n")
	mustOut(t, "repeat [1, 2, 3] {\nmes(\"n\")\n}\n", "n\nn\nn\n")
	// break/continue work; total shows early exit.
	mustOut(t, "total = 0\nrepeat [1, 2, 3, 4] as x {\nif x == 3 {\nbreak\n}\ntotal += x\n}\nmes(total)\n", "3\n")
	mustOut(t, "repeat [1, 2, 3] as x {\nif x == 2 {\ncontinue\n}\nmes(x)\n}\n", "1\n3\n")
	// Element binding is a writable copy; the array is untouched and
	// the variable is invisible after the loop.
	mustOut(t, "a = [1]\nrepeat a as x {\nx = 9\n}\nmes(a)\n", "[1]\n")
	mustErr(t, "repeat [1] as x {\n}\nmes(x)\n", "未定義の変数")
	// Index is read-only; outer bindings are restored afterwards.
	mustOut(t, "i = 100\nx = 200\nrepeat [7, 8] as i, x {\n}\nmes(str(i) + \",\" + str(x))\n", "100,200\n")
	mustErr(t, "repeat [1] as i, x {\ni = 5\n}\n", "読み取り専用")
	mustErr(t, "repeat 5 as i, x {\n}\n", "1 つまで")
	mustErr(t, "repeat \"s\" {\n}\n", "整数または配列")
	mustErr(t, "repeat 1.5 as x {\n}\n", "整数または配列")
	// Length is fixed at loop start: push inside never extends it.
	mustOut(t, "a = [1]\nrepeat a as x {\npush(a, 2)\nmes(x)\n}\nmes(length(a))\n", "1\n2\n")
}

func TestMap(t *testing.T) {
	mustOut(t, "m = {\"a\": 1, \"b\": 2}\nmes(m)\nmes(m[\"a\"])\n", "{\"a\": 1, \"b\": 2}\n1\n")
	mustOut(t, "m = {}\nmes(m)\nmes(has(m, \"missing\"))\n", "{}\nfalse\n")
	mustErr(t, "m = {}\nmes(m[\"missing\"])\n", "キー")
	mustOut(t, "mes({\"a\": 1, \"a\": 2})\n", "{\"a\": 2}\n")
	mustOut(t, "m = {\"a\": 1}\nm[\"b\"] = 2\nm[\"a\"] = 10\nmes(m)\n", "{\"a\": 10, \"b\": 2}\n")
	mustOut(t, "m = {\"a\": {\"b\": 5}}\nmes(m[\"a\"][\"b\"])\nm[\"a\"][\"b\"] = 6\nmes(m[\"a\"][\"b\"])\n", "5\n6\n")
	mustOut(t, "m = {}\nm[\"k\"] = 1\nmes(m)\n", "{\"k\": 1}\n")
	mustOut(t, "a = [{\"x\": 1}]\nmes(a[0][\"x\"])\na[0][\"x\"] = 2\nmes(a[0][\"x\"])\n", "1\n2\n")
	mustOut(t, "m = {\"b\": 2, \"a\": 1}\nmes(keys(m))\nmes(has(m, \"a\"))\nmes(has(m, \"z\"))\n", "[b, a]\ntrue\nfalse\n")
	mustOut(t, "m = {\"a\": 1, \"b\": 2}\ndel(m, \"a\")\nmes(m)\nmes(keys(m))\ndel(m, \"zz\")\nmes(m)\n", "{\"b\": 2}\n[b]\n{\"b\": 2}\n")
	mustOut(t, "mes({\"a\": 1} == {\"a\": 1})\nmes({\"a\": 1} == {\"a\": 2})\nmes(vartype({}))\n", "true\nfalse\nmap\n")
	mustOut(t, "m = {\"a\": 1}\nn = m\nn[\"a\"] = 9\nmes(m)\n", "{\"a\": 9}\n")
	mustErr(t, "m = {1: \"a\"}\n", "キーは文字列")
	mustErr(t, "m = {\"a\" 1}\n", "キー : 値")
	mustErr(t, "m = {\"a\": 1\n", "'}'")
	mustErr(t, "m = {\"a\": 1}\nmes(m[0])\n", "キーは文字列")
	mustOut(t, "m = {\"a\": 1}\nmes(m.a)\nm.a = 2\nm.b = 3\nmes(m)\n", "1\n{\"a\": 2, \"b\": 3}\n")
	mustErr(t, "m = {}\nmes(m.nope)\n", "キー")
	mustErr(t, "x = 1\nmes(x.name)\n", "map のみ対応")
	mustErr(t, "x = 1\nx.name = 2\n", "map ではありません")
	mustErr(t, "m = [1]\nm[\"a\"] = 2\n", "配列と map")
	mustErr(t, "m = {\"a\": 1}\nm[\"a\"][\"b\"] = 2\n", "配列と map")
	mustErr(t, "mes(keys([1]))\n", "map である必要があります")
	mustErr(t, "x = 1\nmes(x[0])\n", "配列と map")
}

func TestRadixLiterals(t *testing.T) {
	mustOut(t, "mes(0xFF)\nmes(0b101)\nmes(0o17)\n", "255\n5\n15\n")
	mustOut(t, "mes(0XAB + 0B1 + 0O7)\n", "179\n")
	mustOut(t, "mes(0xFF & 0xF)\nmes(0b1000 >> 2)\nmes(0o10 | 1)\n", "15\n2\n9\n")
	mustOut(t, "x = 0x10\nmes(x * 2)\n", "32\n")
	mustErr(t, "mes(0x)\n", "不正な16進数リテラル")
	mustErr(t, "mes(0b2)\n", "不正な2進数リテラル")
	mustErr(t, "mes(0o8)\n", "不正な8進数リテラル")
}

func TestArrays(t *testing.T) {
	mustOut(t, "a = [1, 2, 3]\nmes(a[0])\nmes(a[2])\n", "1\n3\n")
	mustOut(t, "a = [10, \"hello\", true]\nmes(a)\n", "[10, hello, true]\n")
	mustOut(t, "a = [[1, 2, 3], [4, 5, 6]]\nmes(a[0][1])\nmes(a[1][2])\n", "2\n6\n")
	mustOut(t, "a = dim(1, 2)\na[0][1] = 123\nmes(a[0][1])\nmes(a)\n", "123\n[[0, 123]]\n")
	mustOut(t, "a = [1]\na[0] = [10, 20, 30]\nmes(a[0][2])\n", "30\n")
	mustOut(t, "a = dim(3)\na[0] = 100\na[2] = 300\nmes(a)\nmes(length(a))\n", "[100, 0, 300]\n3\n")
	mustOut(t, "a = dim(0)\nmes(a)\nmes(length(a))\n", "[]\n0\n")
	mustOut(t, "a = [1]\npush(a, 2, 3)\nmes(a)\n", "[1, 2, 3]\n")
	mustErr(t, "a = [1]\nmes(a[5])\n", "範囲外")
	mustErr(t, "a = [1]\na[1] = 2\n", "範囲外")
	mustErr(t, "a = []\na[0] = 1\n", "範囲外")
	mustErr(t, "nosuch[0] = 1\n", "未定義の変数")
	mustErr(t, "a = [1]\nmes(a[0.5])\n", "整数である必要があります")
	mustErr(t, "a = [1]\nmes(a[-1])\n", "0 以上")
	mustErr(t, "a = [1]\na(0)\n", "[...]")
	mustErr(t, "x = 1\nmes(x[0])\n", "配列と map")
	mustErr(t, "dim()\n", "1 個")
	mustErr(t, "dim(-1)\n", "0 以上")
	mustErr(t, "dim(\"x\")\n", "整数である必要があります")
}

func TestFunctions(t *testing.T) {
	mustOut(t, "def hello() {\nmes(\"Hello\")\n}\nhello()\n", "Hello\n")
	mustOut(t, "def add(a, b) {\nreturn a + b\n}\nmes(add(10, 20))\n", "30\n")
	mustOut(t, "def f() {\nreturn 1\nreturn 2\n}\nmes(f())\n", "1\n")
	mustOut(t, "def f() {\n}\nmes(f())\n", "null\n")
	mustOut(t, "def fact(n) {\nif n <= 1 {\nreturn 1\n}\nreturn n * fact(n - 1)\n}\nmes(fact(5))\n", "120\n")
	mustErr(t, "hello()\n", "未定義の関数")
	mustErr(t, "def f(a) {\n}\nf(1, 2)\n", "1 個必要")
	mustOut(t, "def greet(name, mark=\"!\") {\nreturn name + mark\n}\nmes(greet(\"hi\"))\nmes(greet(\"hi\", \"?\"))\n", "hi!\nhi?\n")
	mustOut(t, "n = 10\ndef f(a, b=n*2) {\nreturn b\n}\nmes(f(1))\nn = 100\nmes(f(1))\nmes(f(1, 5))\n", "20\n200\n5\n")
	mustErr(t, "def f(a) {\n}\nf()\n", "1 個必要")
	mustErr(t, "def f(a=1, b) {\n}\n", "デフォルト引数の後に必須引数")
	mustErr(t, "x = 5\nx()\n", "関数ではありません")
}

func TestMes(t *testing.T) {
	mustOut(t, "mes(\"Hello\")\n", "Hello\n")
	mustOut(t, "x = 10\nmes(x + 20)\n", "30\n")
	mustOut(t, "x = 1\ny = 2\nmes(\"x =\", x, \"y =\", y)\n", "x = 1 y = 2\n")
	mustOut(t, "mes()\n", "\n")
	mustOut(t, "mes([1, [2, 3]])\n", "[1, [2, 3]]\n")
}

func TestMapDot(t *testing.T) {
	mustOut(t, "p = {\"name\": \"Alice\", \"age\": 20}\nmes(p.name)\nmes(p.age)\n", "Alice\n20\n")
	mustOut(t, "p = {\"name\": \"A\", \"age\": 1}\np.name = \"Bob\"\np.age = 25\nmes(p)\n", "{\"name\": Bob, \"age\": 25}\n")
	mustOut(t, "people = [{\"name\": \"A\"}, {\"name\": \"B\"}]\nmes(people[0].name)\nmes(people[1].name)\n", "A\nB\n")
	mustOut(t, "data = [[{\"name\": \"X\"}]]\nmes(data[0][0].name)\n", "X\n")
	mustOut(t, "m = {}\nm.new = 1\nmes(m.new)\n", "1\n")
	mustErr(t, "m = {}\nmes(m.nope)\n", "キー")
	mustErr(t, "x = 1\nmes(x.name)\n", "map のみ対応")
	mustErr(t, "m = {\"a\": 1}\nmes(m.a.b)\n", "map のみ対応")
}

func TestCommentsAndNewlines(t *testing.T) {
	mustOut(t, "// hello\n/* multi\nline */\nmes(1)\nmes(2)\n", "1\n2\n")
	mustErr(t, "mes(1);\n", "セミコロン")
}

func TestFormerKeywordsAreIdents(t *testing.T) {
	// goto/gosub/elif carry no special meaning; they are ordinary names.
	mustOut(t, "goto = 5\nmes(goto)\n", "5\n")
	mustOut(t, "gosub = 6\nmes(gosub)\n", "6\n")
	mustOut(t, "elif = 7\nmes(elif)\n", "7\n")
	if _, err := Parse("*label\n"); err == nil {
		t.Fatal("labels should be rejected")
	}
	// Bare enum declares sequential constants; `enum` alone stays usable.
	mustOut(t, "enum Color { Red, Green = 5, Blue }\nmes(Color_Red)\nmes(Color_Green)\nmes(Color_Blue)\n", "0\n5\n6\n")
	mustOut(t, "enum { A, B }\nmes(A)\nmes(B)\n", "0\n1\n")
	mustOut(t, "enum = 5\nmes(enum)\n", "5\n")
	mustOut(t, "m = {\"Red\": 7}\nmes(m.Red)\nenum C { Red, }\nmes(C_Red)\nmes(m.Red)\n", "7\n0\n7\n")
	mustErr(t, "mes(X_Q)\nenum X { Q }\n", "未定義の変数")
}

func TestSample19Golden(t *testing.T) {
	// Via ParseFile so preprocessor lines (#mode cli) are honored.
	prog, err := ParseFile("examples/sample19.gsh")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if err := NewInterp(&buf).Run(prog); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "Hello, Alice\nHello, Bob\nodd\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}
