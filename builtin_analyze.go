// Code-analysis builtins for editor tooling (CUI + GUI).
//
// lextokens() exposes the lexer for syntax highlighting and static
// analysis; strwidth() measures display width for caret placement in
// hand-made editors.
package main

func init() {
	// lextokens(src): tokenize source, returning an array of
	// {type, text, line, col} maps (EOF omitted). Lex errors are
	// runtime errors carrying the lexer position.
	register("lextokens", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		src, err := needString("lextokens", args, 0, at)
		if err != nil {
			return Null(), err
		}
		toks, err := Lex(src)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		out := make([]Value, 0, len(toks))
		for _, t := range toks {
			if t.Type == TokEOF {
				continue
			}
			m := MapOf()
			SetMap(m.Mp, "type", Str(string(t.Type)))
			SetMap(m.Mp, "text", Str(t.Lit))
			SetMap(m.Mp, "line", Int(int64(t.Line)))
			SetMap(m.Mp, "col", Int(int64(t.Column)))
			out = append(out, m)
		}
		return ArrayOf(out), nil
	})

	// strwidth(s): display width of s — pixels at the current GUI
	// face, rune count on CUI. For caret placement in editors.
	register("strwidth", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("strwidth", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Int(int64(in.be.TextWidth(s))), nil
	})
}
