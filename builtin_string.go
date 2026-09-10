// P2 builtins: string operations.
//
// Differences from HSP (documented, modernized):
//   - Lengths and indexes count characters (runes), not bytes, so Japanese
//     text behaves as one character per glyph.
//   - instr returns the absolute index (HSP returns the index relative to
//     the start position).
//   - split/getpath/strf return values instead of writing to out-params.
package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func init() {
	// strlen(s): character count.
	register("strlen", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("strlen", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Int(int64(len([]rune(s)))), nil
	})

	// strmid(s, start, count): substring by character index (0-based).
	// Negative start counts from the end (as in HSP).
	register("strmid", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("strmid", args, 0, at)
		if err != nil {
			return Null(), err
		}
		start, err := needInt("strmid", args, 1, at)
		if err != nil {
			return Null(), err
		}
		count, err := needInt("strmid", args, 2, at)
		if err != nil {
			return Null(), err
		}
		if count < 0 {
			return Null(), argErr("strmid", 2, at, "count は 0 以上である必要があります。%d が指定されました", count)
		}
		r := []rune(s)
		if start < 0 {
			start += int64(len(r))
		}
		if start < 0 || start > int64(len(r)) {
			return Null(), argErr("strmid", 1, at, "start %d は範囲外です（長さ %d）", args[1].I, len(r))
		}
		end := start + count
		if end > int64(len(r)) {
			end = int64(len(r))
		}
		return Str(string(r[start:end])), nil
	})

	// instr(s, sub) / instr(s, start, sub): absolute index of sub at or
	// after start, or -1 when absent.
	register("instr", 2, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("instr", args, 0, at)
		if err != nil {
			return Null(), err
		}
		var start int64
		subIdx := 1
		if len(args) == 3 {
			start, err = needInt("instr", args, 1, at)
			if err != nil {
				return Null(), err
			}
			subIdx = 2
		}
		sub, err := needString("instr", args, subIdx, at)
		if err != nil {
			return Null(), err
		}
		r := []rune(s)
		if start < 0 {
			start = 0
		}
		if start > int64(len(r)) {
			return Int(-1), nil
		}
		rel := strings.Index(string(r[start:]), sub)
		if rel < 0 {
			return Int(-1), nil
		}
		return Int(start + int64(len([]rune(string(r[start:])[:rel])))), nil
	})

	// strtrim(s [, chars] [, mode]): strip characters from both ends (mode
	// 0), left only (1), or right only (2). Default chars: whitespace.
	register("strtrim", 1, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("strtrim", args, 0, at)
		if err != nil {
			return Null(), err
		}
		cutset := ""
		whitespace := true
		mode := int64(0)
		if len(args) >= 2 {
			cutset, err = needString("strtrim", args, 1, at)
			if err != nil {
				return Null(), err
			}
			whitespace = false
		}
		if len(args) == 3 {
			mode, err = needInt("strtrim", args, 2, at)
			if err != nil {
				return Null(), err
			}
			if mode < 0 || mode > 2 {
				return Null(), argErr("strtrim", 2, at, "mode は 0（両端）、1（左）、2（右）のいずれかである必要があります。%d が指定されました", mode)
			}
		}
		trim := func(t string) string {
			switch mode {
			case 1:
				if whitespace {
					return strings.TrimLeft(t, " \t\r\n")
				}
				return strings.TrimLeft(t, cutset)
			case 2:
				if whitespace {
					return strings.TrimRight(t, " \t\r\n")
				}
				return strings.TrimRight(t, cutset)
			default:
				if whitespace {
					return strings.TrimSpace(t)
				}
				return strings.Trim(t, cutset)
			}
		}
		return Str(trim(s)), nil
	})

	// replace(s, old, new): replace all occurrences of old with new.
	register("replace", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("replace", args, 0, at)
		if err != nil {
			return Null(), err
		}
		old, err := needString("replace", args, 1, at)
		if err != nil {
			return Null(), err
		}
		nw, err := needString("replace", args, 2, at)
		if err != nil {
			return Null(), err
		}
		if old == "" {
			return Null(), argErr("replace", 1, at, "検索文字列を空にすることはできません")
		}
		return Str(strings.ReplaceAll(s, old, nw)), nil
	})

	// upper(s)/lower(s): ASCII case conversion (Japanese text unaffected).
	register("upper", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("upper", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Str(strings.ToUpper(s)), nil
	})
	register("lower", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("lower", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Str(strings.ToLower(s)), nil
	})

	// split(s, delim): split into an array of strings.
	register("split", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("split", args, 0, at)
		if err != nil {
			return Null(), err
		}
		delim, err := needString("split", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if delim == "" {
			return Null(), argErr("split", 1, at, "区切り文字を空にすることはできません")
		}
		parts := strings.Split(s, delim)
		elems := make([]Value, len(parts))
		for i, p := range parts {
			elems[i] = Str(p)
		}
		return ArrayOf(elems), nil
	})

	// strf(format, args...): formatted string (C/Go-style %d %f %s %x %c %%).
	register("strf", 1, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		format, err := needString("strf", args, 0, at)
		if err != nil {
			return Null(), err
		}
		fargs := make([]any, len(args)-1)
		for i, a := range args[1:] {
			switch a.K {
			case KInt:
				fargs[i] = a.I
			case KFloat:
				fargs[i] = a.F
			case KString:
				fargs[i] = a.S
			case KBool:
				fargs[i] = a.B
			default:
				fargs[i] = Stringify(a)
			}
		}
		return Str(fmt.Sprintf(format, fargs...)), nil
	})

	// getpath(path, mode): path decomposition. Modes follow HSP and combine
	// with addition: 0 copy, 1 strip extension, 2 extension only (with dot),
	// 8 strip directory, 16 lowercase, 32 directory only.
	register("getpath", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		p, err := needString("getpath", args, 0, at)
		if err != nil {
			return Null(), err
		}
		mode, err := needInt("getpath", args, 1, at)
		if err != nil {
			return Null(), err
		}
		// Normalize separators for decomposition, then restore style.
		win := strings.Contains(p, "\\")
		slash := strings.ReplaceAll(p, "\\", "/")
		dir, file := filepath.Split(slash)
		ext := filepath.Ext(file)
		base := strings.TrimSuffix(file, ext)
		out := p
		if mode&1 != 0 {
			out = joinDir(dir, base)
		}
		if mode&2 != 0 {
			// Extension only. When combined with 8/32 the HSP result is
			// still just the extension; keep it simple and HSP-like.
			out = ext
		}
		if mode&8 != 0 && mode&2 == 0 {
			if mode&1 != 0 {
				out = base
			} else {
				out = file
			}
		}
		if mode&32 != 0 {
			out = dir
		}
		if mode&16 != 0 {
			out = strings.ToLower(out)
		}
		if win {
			out = strings.ReplaceAll(out, "/", "\\")
		}
		return Str(out), nil
	})

	// asc(s): code point of the first character (GUI input()
	// control characters included: asc(input()) is 8 for backspace).
	// Empty strings are an error.
	register("asc", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("asc", args, 0, at)
		if err != nil {
			return Null(), err
		}
		r := []rune(s)
		if len(r) == 0 {
			return Null(), rtErrf(at, "asc：空文字列のコードは取得できません")
		}
		return Int(int64(r[0])), nil
	})

	// chr(code): single-character string for a Unicode code point.
	// Surrogates and out-of-range values are an error.
	register("chr", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		c, err := needInt("chr", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if c < 0 || c > 0x10FFFF || (c >= 0xD800 && c <= 0xDFFF) {
			return Null(), argErr("chr", 0, at, "無効なコードポイント %d です", c)
		}
		return Str(string(rune(c))), nil
	})
}

// joinDir rejoins a slash-style dir with a file part, keeping the caller's
// separator flavor (backslash when the original had any).
func joinDir(dir, file string) string {
	if dir == "" {
		return file
	}
	return dir + file
}
