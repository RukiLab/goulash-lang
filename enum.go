// Bare-enum expansion (parser pre-pass) for Goulash v0.1.
//
// `enum [Name] { A [, B [= n]] ... }` as a statement defines integer
// constants with #define-like semantics: textual substitution in line
// order, visible from the declaration to the end of the file. Named
// enums prefix members (Color_Red); anonymous enums use bare names.
// `#enum` (preprocessor) remains accepted and expands the same way.
//
// `enum` stays an ordinary identifier everywhere else: `enum = 5`
// assigns a variable, and member names after `.` are never rewritten.
package main

import (
	"fmt"
	"strconv"
)

// enumMember is one enum entry with its resolved integer literal.
type enumEntry struct {
	name string // fully prefixed
	lit  string // decimal literal
}

// expandEnums consumes bare-enum declarations and rewrites later uses
// into INT tokens. It runs inside ParseTokens, so every entry point
// (run, REPL, tests) shares the behavior.
func expandEnums(toks []Token) ([]Token, error) {
	consts := map[string]string{}
	var out []Token
	stmtStart := true
	i := 0
	for i < len(toks) {
		t := toks[i]
		if t.Type == TokEOF {
			out = append(out, t)
			i++
			continue
		}
		if stmtStart && t.Type == TokIdent && t.Lit == "enum" {
			decl, next, err := matchEnumDecl(toks, i)
			if err != nil {
				return nil, err
			}
			if decl != nil {
				for _, m := range decl {
					if _, dup := consts[m.name]; dup {
						return nil, &ParseError{File: t.File, Line: t.Line, Column: t.Column, Msg: fmt.Sprintf("enum %q は既に定義されています", m.name)}
					}
					consts[m.name] = m.lit
				}
				i = next
				stmtStart = true
				continue
			}
		}
		if t.Type == TokIdent && !prevIsDot(out) {
			if lit, ok := consts[t.Lit]; ok {
				t.Type = TokInt
				t.Lit = lit
			}
		}
		out = append(out, t)
		stmtStart = isEnumBoundary(t)
		i++
	}
	return out, nil
}

// matchEnumDecl parses a bare-enum declaration at toks[i] (IDENT
// "enum"). It returns nil when the tokens are not enum-shaped, leaving
// `enum` as an ordinary identifier (e.g. `enum = 5`). A statement-start
// `enum` followed by an identifier that is not brace-opened is always
// malformed, so it reports the enum syntax instead of a generic error.
func matchEnumDecl(toks []Token, i int) ([]enumEntry, int, error) {
	at := func(n int) Token {
		if i+n >= len(toks) {
			return Token{Type: TokEOF}
		}
		return toks[i+n]
	}
	bad := func() ([]enumEntry, int, error) {
		t := toks[i]
		return nil, 0, &ParseError{File: t.File, Line: t.Line, Column: t.Column, Msg: "不正な enum です（enum [Name] { A [, B [= n]] ... } が必要です）"}
	}
	j := i + 1
	prefix := ""
	switch {
	case at(1).Type == TokLBrace:
		j = i + 2
	case at(1).Type == TokIdent && at(2).Type == TokLBrace:
		prefix = at(1).Lit + "_"
		j = i + 3
	case at(1).Type == TokIdent:
		return bad()
	default:
		return nil, 0, nil
	}
	// Newlines may appear anywhere inside the braces (the opening
	// brace itself stays on the enum head line, Go-style).
	skipNL := func() {
		for at(j-i).Type == TokNewline {
			j++
		}
	}
	var members []enumEntry
	next := int64(0)
	skipNL()
	for {
		t := at(j - i)
		if t.Type == TokRBrace {
			j++
			break
		}
		if t.Type != TokIdent {
			return bad()
		}
		full := prefix + t.Lit
		j++
		v := next
		if at(j-i).Type == TokAssign {
			j++
			neg := false
			if at(j-i).Type == TokMinus {
				neg = true
				j++
			}
			nt := at(j - i)
			if nt.Type != TokInt {
				return bad()
			}
			n, err := strconv.ParseInt(nt.Lit, 10, 64)
			if err != nil {
				return bad()
			}
			if neg {
				n = -n
			}
			v = n
			j++
		}
		members = append(members, enumEntry{name: full, lit: strconv.FormatInt(v, 10)})
		next = v + 1
		skipNL()
		switch at(j - i).Type {
		case TokComma:
			j++
			skipNL()
		case TokRBrace:
			j++
			return members, j, nil
		default:
			return bad()
		}
	}
	if len(members) == 0 {
		return bad()
	}
	return members, j, nil
}

// prevIsDot reports whether the last emitted token is a field dot, in
// which case the identifier names a field and must not be rewritten.
func prevIsDot(out []Token) bool {
	return len(out) > 0 && out[len(out)-1].Type == TokDot
}

// isEnumBoundary reports tokens after which a new statement may start.
func isEnumBoundary(t Token) bool {
	switch t.Type {
	case TokNewline, TokLBrace, TokRBrace, TokSemicolon:
		return true
	}
	return false
}
