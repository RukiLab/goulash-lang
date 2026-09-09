package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runIO executes src with given stdin, capturing stdout and logmes output.
func runIO(t *testing.T, src, stdin string) (string, string, *Interp, error) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		return "", "", nil, err
	}
	var out, errout bytes.Buffer
	in := NewInterpWithIO(&out, strings.NewReader(stdin))
	in.errOut = &errout
	err = in.Run(prog)
	return out.String(), errout.String(), in, err
}

func mustOutIO(t *testing.T, src, stdin, want string) {
	t.Helper()
	got, _, _, err := runIO(t, src, stdin)
	if err != nil {
		t.Fatalf("src %q: runtime error: %v", src, err)
	}
	if got != want {
		t.Fatalf("src %q:\n got %q\nwant %q", src, got, want)
	}
}

func mustErrIO(t *testing.T, src, substr string) {
	t.Helper()
	_, _, _, err := runIO(t, src, "")
	if err == nil {
		t.Fatalf("src %q: expected error containing %q, got nil", src, substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("src %q: error %q does not contain %q", src, err.Error(), substr)
	}
}

func TestBuiltinRegistryComplete(t *testing.T) {
	want := []string{
		"mes", "print", "cls", "color", "title", "pos", "input",
		"int", "float", "str", "vartype", "length",
		"abs", "sqrt", "sin", "cos", "tan", "atan", "exp", "log", "pow", "limit",
		"rnd", "randomize",
		"strlen", "strmid", "instr", "strtrim", "split", "strf", "getpath",
		"gettime", "sleep", "await", "tick", "nanotime", "end", "assert", "throw", "logmes", "exec", "args",
		"getenv", "setenv", "open", "clipboard_get", "clipboard_set",
		"dlgopen", "dlgsave", "httpget",
		"exist", "dirlist", "delete", "mkdir", "chdir", "bcopy", "bload", "bsave",
		"getcwd", "direxe", "homedir", "tmpdir",
		"notemax", "noteget", "noteadd", "notedel", "noteload", "notesave",
		"peek", "wpeek", "lpeek", "poke", "wpoke", "lpoke",
		"push", "pop", "join", "dim",
		"sort", "reverse", "insert", "remove", "slice", "find",
		"keys", "has", "del",
		"replace", "upper", "lower",
		"lextokens", "strwidth",
	}
	got := map[string]bool{}
	for _, n := range builtinNames() {
		got[n] = true
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("missing builtin %q", n)
		}
		delete(got, n)
	}
	// GUI-only words (builtin_gui.go, -tags gui) are valid extras.
	for _, n := range guiBuiltinNames {
		delete(got, n)
	}
	for n := range got {
		t.Errorf("unexpected builtin %q (add to test or remove)", n)
	}
}

func TestBuiltinRedefineGuard(t *testing.T) {
	mustErrIO(t, "def sin(x) {\nreturn x\n}\n", "組み込み関数")
	mustErrIO(t, "def mes() {\n}\n", "組み込み関数")
}

func TestConversions(t *testing.T) {
	mustOutIO(t, "mes(int(3.9))\nmes(int(\" 12 \"))\nmes(int(true))\nmes(int(\"3.7\"))\n", "", "3\n12\n1\n3\n")
	mustOutIO(t, "mes(float(3))\nmes(float(\"2.5\"))\nmes(float(false))\n", "", "3\n2.5\n0\n")
	mustOutIO(t, "mes(str(42))\nmes(str([1, 2]))\nmes(vartype(1))\nmes(vartype(1.5))\nmes(vartype(\"s\"))\nmes(vartype(true))\nmes(vartype([1]))\n", "", "42\n[1, 2]\nint\nfloat\nstring\nbool\narray\n")
	mustErrIO(t, "mes(int(\"abc\"))\n", "変換できません")
	mustErrIO(t, "mes(int([1]))\n", "変換できません")
	mustErrIO(t, "mes(float(\"x\"))\n", "変換できません")
	mustErrIO(t, "mes(int())\n", "1 個必要")
	mustErrIO(t, "mes(int(1, 2))\n", "1 個必要")
}

func TestMath(t *testing.T) {
	mustOutIO(t, "mes(abs(-5))\nmes(abs(5.5))\nmes(abs(0))\n", "", "5\n5.5\n0\n")
	mustOutIO(t, "mes(sqrt(16))\nmes(sin(0))\nmes(cos(0))\nmes(pow(2, 10))\nmes(exp(0))\nmes(log(1))\n", "", "4\n0\n1\n1024\n1\n0\n")
	mustOutIO(t, "mes(limit(5, 0, 10))\nmes(limit(-3, 0, 10))\nmes(limit(99, 0, 10))\nmes(limit(5.5, 0, 10))\n", "", "5\n0\n10\n5.5\n")
	mustOutIO(t, "mes(atan(0))\nmes(tan(0))\n", "", "0\n0\n")
	mustErrIO(t, "mes(sqrt(-1))\n", "平方根")
	mustErrIO(t, "mes(log(0))\n", "正の数")
	mustErrIO(t, "mes(abs(\"x\"))\n", "数値である必要があります")
	mustErrIO(t, "mes(limit(1, 9, 2))\n", "下限")
}

func TestRandom(t *testing.T) {
	got, _, _, err := runIO(t, "randomize(42)\na = rnd(100)\nrandomize(42)\nb = rnd(100)\nmes(a == b)\nmes(a >= 0 && a < 100)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "true\ntrue\n" {
		t.Fatalf("got %q", got)
	}
	mustErrIO(t, "mes(rnd(0))\n", "正の上限")
	mustErrIO(t, "mes(rnd(\"x\"))\n", "整数である必要があります")
	mustOutIO(t, "randomize()\nmes(rnd(10) >= 0 && rnd(10) < 10)\n", "", "true\n")
}

func TestBuiltinStrings(t *testing.T) {
	mustOutIO(t, "mes(strlen(\"hello\"))\nmes(strlen(\"あいう\"))\nmes(strlen(\"\"))\n", "", "5\n3\n0\n")
	mustOutIO(t, "mes(strmid(\"abcdef\", 1, 3))\nmes(strmid(\"あいうえお\", 1, 2))\nmes(strmid(\"abcdef\", -2, 2))\nmes(strmid(\"abc\", 1, 99))\n", "", "bcd\nいう\nef\nbc\n")
	mustOutIO(t, "mes(instr(\"hello\", \"ll\"))\nmes(instr(\"hello\", 3, \"l\"))\nmes(instr(\"hello\", \"z\"))\nmes(instr(\"hello\", 99, \"h\"))\n", "", "2\n3\n-1\n-1\n")
	mustOutIO(t, "mes(\"[\" + strtrim(\"  hi  \") + \"]\")\nmes(strtrim(\"xxhiix\", \"x\"))\nmes(strtrim(\"xxhi\", \"x\", 1))\nmes(strtrim(\"hixx\", \"x\", 2))\n", "", "[hi]\nhii\nhi\nhi\n")
	mustOutIO(t, "a = split(\"a,b,c\", \",\")\nmes(a[1])\nmes(length(a))\n", "", "b\n3\n")
	mustOutIO(t, "mes(strf(\"%02d\", 5))\nmes(strf(\"%s=%d\", \"n\", 7))\n", "", "05\nn=7\n")
	mustErrIO(t, "mes(strmid(\"abc\", 9, 1))\n", "範囲外")
	mustErrIO(t, "mes(strmid(\"abc\", 0, -1))\n", "0 以上")
	mustErrIO(t, "mes(split(\"a\", \"\"))\n", "空にすることはできません")
	mustErrIO(t, "mes(strtrim(\"a\", \"x\", 5))\n", "mode は")
}

func TestGetpath(t *testing.T) {
	mustOutIO(t, "p = \"C:\\\\a\\\\b.exe\"\nmes(getpath(p, 0))\nmes(getpath(p, 1))\nmes(getpath(p, 2))\nmes(getpath(p, 8))\nmes(getpath(p, 9))\nmes(getpath(p, 32))\nmes(getpath(p, 16))\n", "",
		"C:\\a\\b.exe\nC:\\a\\b\n.exe\nb.exe\nb\nC:\\a\\\nc:\\a\\b.exe\n")
	mustOutIO(t, "mes(getpath(\"/x/y.txt\", 8))\nmes(getpath(\"/x/y.txt\", 32))\n", "", "y.txt\n/x/\n")
}

func TestGettimeRanges(t *testing.T) {
	got, _, _, err := runIO(t, "mes(gettime(1) >= 1 && gettime(1) <= 12)\nmes(gettime(2) >= 0 && gettime(2) <= 6)\nmes(gettime(4) >= 0 && gettime(4) <= 23)\nmes(gettime(6) >= 0 && gettime(6) <= 59)\nmes(gettime(7) >= 0 && gettime(7) <= 999)\nmes(gettime(0) >= 2026)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "true\ntrue\ntrue\ntrue\ntrue\ntrue\n" {
		t.Fatalf("got %q", got)
	}
	mustErrIO(t, "mes(gettime(8))\n", "0 から 7")
}

func TestSleepEndAssertLogmes(t *testing.T) {
	mustOutIO(t, "sleep(0)\nsleep(5)\nmes(\"ok\")\n", "", "ok\n")
	mustErrIO(t, "sleep(-1)\n", "0 以上")
	mustOutIO(t, "await(0)\nawait()\nmes(tick() >= 0)\n", "", "true\n")
	mustErrIO(t, "await(-1)\n", "0 以上")
	mustOutIO(t, "a = nanotime()\nmes(a > 0 && nanotime() >= a)\n", "", "true\n")
	_, _, in, err := runIO(t, "mes(\"hi\")\nend(3)\nmes(\"bye\")\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if code, ok := in.ExitCode(); !ok || code != 3 {
		t.Fatalf("exit code: %d, %v", code, ok)
	}
	mustOutIO(t, "assert(true)\nassert(1 < 2, \"math broke\")\nmes(\"ok\")\n", "", "ok\n")
	mustErrIO(t, "assert(false)\n", "assertion failed")
	mustErrIO(t, "assert(false, \"custom\")\n", "custom")
	mustErrIO(t, "assert(1)\n", "bool 型")
	_, errout, _, err := runIO(t, "logmes(\"dbg\", 42)\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if errout != "dbg 42\n" {
		t.Fatalf("logmes got %q", errout)
	}
}

func TestArgs(t *testing.T) {
	// No arguments: empty array.
	mustOutIO(t, "mes(args())\nmes(length(args()))\n", "", "[]\n0\n")
	// With arguments passed through SetArgs (as run does).
	prog, err := Parse("mes(args())\nmes(args()[1])\n")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	in := NewInterpWithIO(&out, strings.NewReader(""))
	in.SetArgs([]string{"--hard", "lv=3"})
	if err := in.Run(prog); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[--hard, lv=3]\nlv=3\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestEnv(t *testing.T) {
	mustOutIO(t, "setenv(\"GOU_TEST\", \"42\")\nmes(getenv(\"GOU_TEST\"))\nmes(getenv(\"GOU_NO_SUCH_VAR_XYZ\"))\n", "", "42\nnull\n")
	mustErrIO(t, "getenv(1)\n", "文字列である必要があります")
	mustErrIO(t, "setenv(\"a\")\n", "2 個必要")
	mustErrIO(t, "open(1)\n", "文字列である必要があります")
	mustErrIO(t, "open(\"\")\n", "空にすることはできません")
	mustErrIO(t, "dlgopen(1)\n", "文字列である必要があります")
	mustErrIO(t, "httpget(1)\n", "文字列である必要があります")
	mustOutIO(t, "clipboard_set(\"gsh\")\nmes(clipboard_get())\n", "", "gsh\n")
	mustErrIO(t, "clipboard_set(1)\n", "文字列である必要があります")
}

func TestHttpget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			fmt.Fprint(w, "hello")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	mustOutIO(t, fmt.Sprintf("mes(httpget(%q))\n", srv.URL+"/ok"), "", "hello\n")
	mustErrIO(t, fmt.Sprintf("mes(httpget(%q))\n", srv.URL+"/missing"), "404")
	// Save form writes the body and returns its size.
	dir := t.TempDir()
	join := func(n string) string { return filepath.ToSlash(filepath.Join(dir, n)) }
	mustOutIO(t, fmt.Sprintf("mes(httpget(%q, %q))\n", srv.URL+"/ok", join("out.txt")), "", "5\n")
	data, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("saved body = %q, %v", data, err)
	}
	// Unreachable host is an error, not a hang (client timeout).
	mustErrIO(t, "mes(httpget(\"http://127.0.0.1:1/nope\"))\n", "httpget")
}

func TestExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		mustOutIO(t, "mes(exec(\"cmd\", \"/c\", \"exit\", \"3\"))\n", "", "3\n")
	} else {
		mustOutIO(t, "mes(exec(\"sh\", \"-c\", \"exit 3\"))\n", "", "3\n")
	}
	mustErrIO(t, "mes(exec(\"no-such-command-xyz\"))\n", "exec：")
}

func TestConsoleIO(t *testing.T) {
	mustOutIO(t, "print(\"a\", 1)\nprint(\"b\")\nmes(\"c\")\n", "", "a 1bc\n")
	mustOutIO(t, "cls()\n", "", "\x1b[2J\x1b[H")
	mustOutIO(t, "color(255, 0, 0)\ncolor()\n", "", "\x1b[38;2;255;0;0m\x1b[0m")
	mustOutIO(t, "pos(3, 4)\n", "", "\x1b[5;4H")
	mustOutIO(t, "pos(-1, 0)\n", "", "\x1b[1;0H")
	mustOutIO(t, "pos(-5, -3)\n", "", "\x1b[-2;-4H")
	mustOutIO(t, "title(\"T\")\n", "", "\x1b]0;T\x07")
	mustErrIO(t, "color(1, 2)\n", "0 個または 3 個")
	mustErrIO(t, "color(300, 0, 0)\n", "0 から 255")
}

func TestInput(t *testing.T) {
	mustOutIO(t, "s = input()\nmes(s)\n", "hello\n", "hello\n")
	mustOutIO(t, "s = input(\"name? \")\nmes(s)\n", "bob\n", "name? bob\n")
	mustOutIO(t, "mes(input())\n", "", "null\n") // EOF
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	// Forward slashes: the script language has no backslash escapes besides
	// \" \\ \n \t \r, and Windows accepts forward slashes.
	join := func(n string) string { return filepath.ToSlash(filepath.Join(dir, n)) }

	mustOutIO(t, "mes(exist(\""+join("nope")+"\"))\n", "", "false\n")
	mustErrIO(t, "delete(\""+join("nope")+"\")\n", "delete：")

	// notesave/noteload + exist roundtrip.
	mustOutIO(t, "notesave(\""+join("t.txt")+"\", \"a\\nb\")\nmes(exist(\""+join("t.txt")+"\"))\nmes(noteload(\""+join("t.txt")+"\"))\n", "",
		"true\na\nb\n")

	// bsave/bload roundtrip with size clip.
	mustOutIO(t, "bsave(\""+join("b.bin")+"\", [65, 66, 67])\na = bload(\""+join("b.bin")+"\")\nmes(a)\nmes(length(a))\nbsave(\""+join("c.bin")+"\", [1, 2, 3], 2)\nmes(length(bload(\""+join("c.bin")+"\")))\n", "",
		"[65, 66, 67]\n3\n2\n")
	mustErrIO(t, "bsave(\""+join("x.bin")+"\", [256])\n", "0 から 255")
	mustErrIO(t, "mes(bload(\""+join("missing.bin")+"\"))\n", "bload：")

	// mkdir/delete/bcopy.
	mustOutIO(t, "mkdir(\""+join("sub")+"\")\nmes(exist(\""+join("sub")+"\"))\n", "", "true\n")
	mustOutIO(t, "bcopy(\""+join("t.txt")+"\", \""+join("t2.txt")+"\")\nmes(exist(\""+join("t2.txt")+"\"))\ndelete(\""+join("t2.txt")+"\")\nmes(exist(\""+join("t2.txt")+"\"))\n", "",
		"true\nfalse\n")

	// dirlist + chdir (restore cwd afterwards).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("z.gsh", []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	mustOutIO(t, "a = dirlist(\"*.gsh\")\nmes(a)\n", "", "[z.gsh]\n")
}

func TestDirs(t *testing.T) {
	mustOutIO(t, "mes(strlen(getcwd()) > 0)\nmes(strlen(direxe()) > 0)\nmes(strlen(homedir()) > 0)\nmes(strlen(tmpdir()) > 0)\n", "", "true\ntrue\ntrue\ntrue\n")
	mustOutIO(t, "mes(exist(getcwd()))\n", "", "true\n")
}

func TestNotes(t *testing.T) {
	mustOutIO(t, "mes(notemax(\"a\\nb\\nc\"))\nmes(notemax(\"\"))\n", "", "3\n0\n")
	mustOutIO(t, "mes(noteget(\"a\\nb\", 0))\nmes(noteget(\"a\\nb\", 1))\n", "", "a\nb\n")
	mustOutIO(t, "mes(noteadd(\"a\\nb\", 1, \"B\"))\nmes(noteadd(\"a\", 1, \"b\"))\n", "", "a\nB\na\nb\n")
	mustOutIO(t, "mes(notedel(\"a\\nb\\nc\", 1))\n", "", "a\nc\n")
	mustErrIO(t, "mes(noteget(\"a\", 5))\n", "範囲外")
	mustErrIO(t, "mes(notedel(\"a\", 2))\n", "範囲外")
	mustErrIO(t, "mes(noteadd(\"a\", 9, \"x\"))\n", "範囲外")
}

func TestPeekPoke(t *testing.T) {
	mustOutIO(t, "a = [0, 0, 0, 0]\npoke(a, 0, 65)\nmes(peek(a, 0))\nwpoke(a, 1, 16961)\nmes(wpeek(a, 1))\nlpoke(a, 0, 16909060)\nmes(lpeek(a, 0))\nmes(a)\n", "",
		"65\n16961\n16909060\n[4, 3, 2, 1]\n")
	mustErrIO(t, "mes(peek([1], 5))\n", "範囲外")
	mustErrIO(t, "mes(peek([1], -1))\n", "範囲外")
	mustErrIO(t, "poke([0], 0, 256)\n", "0 から 255")
	mustErrIO(t, "mes(peek([\"x\"], 0))\n", "バイト")
	mustErrIO(t, "mes(lpeek([1, 2], 0))\n", "範囲外")
}

func TestPushPopJoin(t *testing.T) {
	mustOutIO(t, "a = []\nmes(push(a, 1))\nmes(push(a, 2, 3))\nmes(a)\n", "", "1\n3\n[1, 2, 3]\n")
	mustOutIO(t, "a = [1, 2, 3]\nmes(pop(a))\nmes(a)\n", "", "3\n[1, 2]\n")
	mustOutIO(t, "mes(join([1, 2, 3]))\nmes(join([\"a\", \"b\"], \"-\"))\nmes(join([], \",\"))\n", "", "1,2,3\na-b\n\n")
	// Shared reference: push through an alias is visible.
	mustOutIO(t, "a = [1]\nb = a\npush(b, 2)\nmes(a)\n", "", "[1, 2]\n")
	mustErrIO(t, "mes(pop([]))\n", "配列が空")
	mustErrIO(t, "push(1, 2)\n", "配列である必要があります")
	mustErrIO(t, "mes(join([1], 2))\n", "文字列である必要があります")
}

func TestArrayExtras(t *testing.T) {
	mustOutIO(t, "a = [3, 1, 2]\nmes(sort(a))\n", "", "[1, 2, 3]\n")
	mustOutIO(t, "a = [\"b\", \"a\"]\nsort(a)\nmes(a)\n", "", "[a, b]\n")
	mustOutIO(t, "a = [1, 2, 3]\nreverse(a)\nmes(a)\n", "", "[3, 2, 1]\n")
	mustOutIO(t, "a = [1, 3]\nmes(insert(a, 1, 2))\nmes(a)\n", "", "3\n[1, 2, 3]\n")
	mustOutIO(t, "a = [1, 2, 3, 4]\nmes(remove(a, 1, 2))\nmes(a)\n", "", "[2, 3]\n[1, 4]\n")
	mustOutIO(t, "a = [1, 2, 3]\nmes(remove(a, 0))\nmes(a)\n", "", "[1]\n[2, 3]\n")
	mustOutIO(t, "a = [1, 2, 3, 4]\nmes(slice(a, 1, 3))\nmes(slice(a, -2))\nmes(slice(a, 0, -1))\n", "", "[2, 3]\n[3, 4]\n[1, 2, 3]\n")
	mustOutIO(t, "a = [\"x\", \"y\"]\nmes(find(a, \"y\"))\nmes(find(a, \"z\"))\nmes(find(a, 1))\n", "", "1\n-1\n-1\n")
	// Chaining: sort returns the array itself.
	mustOutIO(t, "mes(join(sort([3, 1, 2]), \"-\"))\n", "", "1-2-3\n")
	mustErrIO(t, "sort([1, \"a\"])\n", "すべて整数かすべて文字列")
	mustErrIO(t, "insert([1], 5, 2)\n", "範囲外")
	mustErrIO(t, "remove([1], 0, 2)\n", "範囲外")
	mustErrIO(t, "slice([1, 2], 2, 1)\n", "範囲外")
	mustErrIO(t, "sort(1)\n", "配列である必要があります")
}

func TestStringExtras(t *testing.T) {
	mustOutIO(t, "mes(replace(\"aaa\", \"a\", \"b\"))\nmes(replace(\"hello\", \"z\", \"y\"))\n", "", "bbb\nhello\n")
	mustOutIO(t, "mes(upper(\"abc-あ\"))\nmes(lower(\"ABC-ア\"))\n", "", "ABC-あ\nabc-ア\n")
	mustErrIO(t, "replace(\"a\", \"\", \"b\")\n", "空にすることはできません")
	mustErrIO(t, "upper(1)\n", "文字列である必要があります")
}

func TestVartypeMapFunc(t *testing.T) {
	mustOutIO(t, "def f() {\n}\nmes(vartype({\"a\": 1}))\nmes(vartype(f))\n", "", "map\nfunction\n")
}

func TestLexTokens(t *testing.T) {
	mustOutIO(t, "t = lextokens(\"x = 1\")\nmes(length(t))\nmes(t[0].type)\nmes(t[0].text)\nmes(t[1].text)\nmes(t[2].text)\nmes(t[0].line)\nmes(t[0].col)\n", "", "3\nIDENT\nx\n=\n1\n1\n1\n")
	mustOutIO(t, "t = lextokens(\"if x {\\n}\")\nmes(t[0].type)\nmes(t[2].type)\nmes(t[3].type)\n", "", "IF\nLBRACE\nNEWLINE\n")
	mustErrIO(t, "lextokens(\"'a'\")\n", "シングルクォート")
	mustErrIO(t, "lextokens(1)\n", "文字列である必要があります")
}

func TestStrWidth(t *testing.T) {
	mustOutIO(t, "mes(strwidth(\"abc\"))\nmes(strwidth(\"あいう\"))\nmes(strwidth(\"\"))\n", "", "3\n3\n0\n")
	mustErrIO(t, "strwidth(1)\n", "文字列である必要があります")
}
