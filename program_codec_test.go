// バイトコード直列化・exe連結のテスト。
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustMarshal(t *testing.T, src, mode string) []byte {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	vprog, err := Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	payload, err := MarshalVMProgram(vprog, mode)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return payload
}

func runUnmarshaled(t *testing.T, payload []byte) (string, string, error) {
	t.Helper()
	vprog, mode, err := UnmarshalVMProgram(payload)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var buf bytes.Buffer
	in := NewInterpWithIO(&buf, strings.NewReader(""))
	err = newVmachine(in).runMain(vprog)
	return buf.String(), mode, err
}

// TestCodecRoundtrip は直列化→復元→実行の一致を検証する。
func TestCodecRoundtrip(t *testing.T) {
	cases := []string{
		"x = 10 + 2 * 3\nmes(x)\nmes(\"s\" + 1.5)\n",
		"def fib(n) {\nif n < 2 {\nreturn n\n}\nreturn fib(n - 1) + fib(n - 2)\n}\nmes(fib(15))\n",
		"a = [1, [2, 3], \"x\", true]\nrepeat a as i, x {\nmes(i)\nmes(x)\n}\n",
		"switch 2 {\ncase 1 {\nmes(1)\n}\ncase 2, 3 {\nmes(23)\n}\ndefault {\nmes(\"d\")\n}\n}\n",
		"mes(1 / 0)\n",
		"mes(zzz)\n",
	}
	for _, src := range cases {
		payload := mustMarshal(t, src, "cli")
		// 構造同一：逆アセンブル結果が変わらないこと。
		prog, _ := Parse(src)
		vprog, _ := Compile(prog)
		restored, mode, rerr := func() (*VMProgram, string, error) {
			return UnmarshalVMProgram(payload)
		}()
		if rerr != nil {
			t.Fatalf("src %q: unmarshal: %v", src, rerr)
		}
		if mode != "cli" {
			t.Fatalf("mode lost: %q", mode)
		}
		if Disassemble(vprog) != Disassemble(restored) {
			t.Fatalf("src %q: disasm differs after roundtrip", src)
		}
		// 実行一致。
		wantOut, wantErr := runVMOnly(t, src)
		gotOut, _, gotErr := runUnmarshaled(t, payload)
		if gotOut != wantOut || errStr(gotErr) != errStr(wantErr) {
			t.Fatalf("src %q:\n direct=%q/%v\n restored=%q/%v", src, wantOut, wantErr, gotOut, gotErr)
		}
	}
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestCodecCorrupt は破損検出を検証する。
func TestCodecCorrupt(t *testing.T) {
	payload := mustMarshal(t, "mes(1)\n", "cli")
	bad := append([]byte(nil), payload...)
	bad[len(bad)/2] ^= 0xFF
	if _, _, err := UnmarshalVMProgram(bad); err == nil {
		t.Fatalf("corrupt payload accepted")
	}
	if _, _, err := UnmarshalVMProgram(payload[:10]); err == nil {
		t.Fatalf("truncated payload accepted")
	}
}

// TestBundleTrailer は exe 連結・抽出・入替えを検証する。
func TestBundleTrailer(t *testing.T) {
	dir := t.TempDir()
	fakeExe := filepath.Join(dir, "runtime.exe")
	if err := os.WriteFile(fakeExe, []byte("MZ-fake-exe-image"), 0755); err != nil {
		t.Fatal(err)
	}
	p1 := mustMarshal(t, "mes(1)\n", "cli")
	p2 := mustMarshal(t, "mes(2)\n", "gui")
	out := filepath.Join(dir, "app.exe")
	if err := AppendBundle(fakeExe, out, p1); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ExtractBundle(out)
	if err != nil || !ok || !bytes.Equal(got, p1) {
		t.Fatalf("extract p1: ok=%v err=%v", ok, err)
	}
	// 入替えビルド：連結済みを入力にしても多重化しない。
	if err := AppendBundle(out, out, p2); err != nil {
		t.Fatal(err)
	}
	got, ok, err = ExtractBundle(out)
	if err != nil || !ok || !bytes.Equal(got, p2) {
		t.Fatalf("extract p2 after rebuild: ok=%v err=%v", ok, err)
	}
	st, _ := os.Stat(out)
	if want := int64(len("MZ-fake-exe-image") + len(p2) + bundleTrailerLen); st.Size() != want {
		t.Fatalf("size=%d want=%d (double bundle?)", st.Size(), want)
	}
	// バンドルなし exe は ok=false。
	plain := filepath.Join(dir, "plain.exe")
	os.WriteFile(plain, []byte("MZ"), 0755)
	if _, ok, err := ExtractBundle(plain); err != nil || ok {
		t.Fatalf("plain exe detected as bundle: ok=%v err=%v", ok, err)
	}
}
