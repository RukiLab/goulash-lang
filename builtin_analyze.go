// Code-analysis builtins for editor tooling (CUI + GUI).
//
// lextokens() exposes the lexer for syntax highlighting and static
// analysis; strwidth() measures display width for caret placement in
// hand-made editors; parsetree() exposes the parser for outline,
// navigation, and hover.
package main

import "strings"

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

	// parsetree(src): parse source into a tree for editors (outline,
	// navigation, hover): {type:"program", line, col, text, stmts,
	// enums}. Every node carries type/line/col/text plus kind fields
	// (name/params/cond/body/...); bodies are statement arrays;
	// expression statements unwrap to their expression. Bare-enum
	// declarations (consumed by the pre-pass) are listed under enums
	// with member positions. Raw directives (#...) are lex errors,
	// like lextokens. Parse errors are runtime errors.
	register("parsetree", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		src, err := needString("parsetree", args, 0, at)
		if err != nil {
			return Null(), err
		}
		toks, err := Lex(src)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		prog, err := ParseTokens(toks)
		if err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		m := nodeBase("program", Pos{Line: 1, Column: 1}, prog.String())
		stmts := make([]Value, 0, len(prog.Stmts))
		for _, s := range prog.Stmts {
			stmts = append(stmts, stmtValue(s))
		}
		SetMap(m.Mp, "stmts", ArrayOf(stmts))
		enums := make([]Value, 0)
		for _, d := range scanEnumDecls(toks) {
			em := nodeBase("enum", Pos{Line: d.line, Column: d.col}, enumDeclText(d))
			SetMap(em.Mp, "name", Str(d.name))
			mems := make([]Value, 0, len(d.members))
			for _, e := range d.members {
				mm := nodeBase("member", Pos{Line: e.line, Column: e.col}, e.name+" = "+e.lit)
				SetMap(mm.Mp, "name", Str(e.name))
				SetMap(mm.Mp, "value", Int(e.value))
				mems = append(mems, mm)
			}
			SetMap(em.Mp, "members", ArrayOf(mems))
			enums = append(enums, em)
		}
		SetMap(m.Mp, "enums", ArrayOf(enums))
		return m, nil
	})
}

// nodeBase builds the common node map: kind, position, canonical text.
func nodeBase(typ string, at Pos, text string) Value {
	m := MapOf()
	SetMap(m.Mp, "type", Str(typ))
	SetMap(m.Mp, "line", Int(int64(at.Line)))
	SetMap(m.Mp, "col", Int(int64(at.Column)))
	SetMap(m.Mp, "text", Str(text))
	return m
}

// enumDeclText rebuilds a canonical enum declaration for hover text.
func enumDeclText(d enumDeclInfo) string {
	parts := make([]string, len(d.members))
	for i, e := range d.members {
		parts[i] = e.name + " = " + e.lit
	}
	if d.name == "" {
		return "enum {" + strings.Join(parts, ", ") + "}"
	}
	return "enum " + d.name + " {" + strings.Join(parts, ", ") + "}"
}

// blockValues converts a statement block (nil-safe).
func blockValues(b *BlockStmt) []Value {
	out := make([]Value, 0)
	if b == nil {
		return out
	}
	for _, s := range b.Stmts {
		out = append(out, stmtValue(s))
	}
	return out
}

// exprOrNull converts an optional expression (nil = absent).
func exprOrNull(x Expr) Value {
	if x == nil {
		return Null()
	}
	return exprValue(x)
}

// stmtValue converts one statement; expression statements unwrap to
// their expression so the tree has no "expr" wrapper nodes.
func stmtValue(s Stmt) Value {
	switch n := s.(type) {
	case *AssignStmt:
		m := nodeBase("assign", n.Pos(), n.String())
		SetMap(m.Mp, "target", exprValue(n.Target))
		SetMap(m.Mp, "value", exprValue(n.Value))
		return m
	case *ExprStmt:
		return exprValue(n.X)
	case *IfStmt:
		m := nodeBase("if", n.Pos(), n.String())
		SetMap(m.Mp, "cond", exprValue(n.Cond))
		SetMap(m.Mp, "then", ArrayOf(blockValues(n.Then)))
		var elseV Value = Null()
		if n.Else != nil {
			if b, ok := n.Else.(*BlockStmt); ok {
				elseV = ArrayOf(blockValues(b))
			} else {
				elseV = stmtValue(n.Else)
			}
		}
		SetMap(m.Mp, "else", elseV)
		return m
	case *RepeatStmt:
		m := nodeBase("repeat", n.Pos(), n.String())
		SetMap(m.Mp, "count", exprValue(n.Count))
		if n.HasVar {
			SetMap(m.Mp, "var", Str(n.Var))
		} else {
			SetMap(m.Mp, "var", Null())
		}
		if n.HasItem {
			SetMap(m.Mp, "item", Str(n.Item))
		} else {
			SetMap(m.Mp, "item", Null())
		}
		SetMap(m.Mp, "body", ArrayOf(blockValues(n.Body)))
		return m
	case *WhileStmt:
		m := nodeBase("while", n.Pos(), n.String())
		SetMap(m.Mp, "cond", exprValue(n.Cond))
		SetMap(m.Mp, "body", ArrayOf(blockValues(n.Body)))
		return m
	case *TryStmt:
		m := nodeBase("try", n.Pos(), n.String())
		SetMap(m.Mp, "body", ArrayOf(blockValues(n.Body)))
		SetMap(m.Mp, "var", Str(n.Var))
		SetMap(m.Mp, "catch", ArrayOf(blockValues(n.Catch)))
		return m
	case *SwitchStmt:
		m := nodeBase("switch", n.Pos(), n.String())
		SetMap(m.Mp, "value", exprValue(n.Value))
		cases := make([]Value, 0, len(n.Cases))
		for _, c := range n.Cases {
			cm := nodeBase("case", c.At, "")
			vals := make([]Value, 0, len(c.Values))
			for _, v := range c.Values {
				vals = append(vals, exprValue(v))
			}
			SetMap(cm.Mp, "values", ArrayOf(vals))
			SetMap(cm.Mp, "body", ArrayOf(blockValues(c.Body)))
			SetMap(cm.Mp, "default", Bool(c.Default))
			cases = append(cases, cm)
		}
		SetMap(m.Mp, "cases", ArrayOf(cases))
		return m
	case *BreakStmt:
		return nodeBase("break", n.Pos(), n.String())
	case *ContinueStmt:
		return nodeBase("continue", n.Pos(), n.String())
	case *ReturnStmt:
		m := nodeBase("return", n.Pos(), n.String())
		SetMap(m.Mp, "value", exprOrNull(n.Value))
		return m
	case *DefStmt:
		m := nodeBase("def", n.Pos(), n.String())
		SetMap(m.Mp, "name", Str(n.Name))
		params := make([]Value, 0, len(n.Params))
		for _, p := range n.Params {
			pm := nodeBase("param", p.At, p.Name)
			SetMap(pm.Mp, "name", Str(p.Name))
			SetMap(pm.Mp, "default", exprOrNull(p.Default))
			params = append(params, pm)
		}
		SetMap(m.Mp, "params", ArrayOf(params))
		SetMap(m.Mp, "body", ArrayOf(blockValues(n.Body)))
		return m
	default:
		return nodeBase("unknown", s.Pos(), s.String())
	}
}

// exprValue converts one expression node.
func exprValue(x Expr) Value {
	switch n := x.(type) {
	case *IntLit:
		m := nodeBase("int", n.Pos(), n.String())
		SetMap(m.Mp, "value", Int(n.Value))
		return m
	case *FloatLit:
		m := nodeBase("float", n.Pos(), n.String())
		SetMap(m.Mp, "value", Float(n.Value))
		return m
	case *StringLit:
		m := nodeBase("string", n.Pos(), n.String())
		SetMap(m.Mp, "value", Str(n.Value))
		return m
	case *BoolLit:
		m := nodeBase("bool", n.Pos(), n.String())
		SetMap(m.Mp, "value", Bool(n.Value))
		return m
	case *NullLit:
		return nodeBase("null", n.Pos(), n.String())
	case *VarExpr:
		m := nodeBase("var", n.Pos(), n.String())
		SetMap(m.Mp, "name", Str(n.Name))
		return m
	case *ArrayLit:
		m := nodeBase("array", n.Pos(), n.String())
		elems := make([]Value, 0, len(n.Elems))
		for _, e := range n.Elems {
			elems = append(elems, exprValue(e))
		}
		SetMap(m.Mp, "elems", ArrayOf(elems))
		return m
	case *MapLit:
		m := nodeBase("map", n.Pos(), n.String())
		fields := make([]Value, 0, len(n.Fields))
		for _, f := range n.Fields {
			fm := nodeBase("field", f.At, f.Key.String()+": "+f.Value.String())
			SetMap(fm.Mp, "key", exprValue(f.Key))
			SetMap(fm.Mp, "value", exprValue(f.Value))
			fields = append(fields, fm)
		}
		SetMap(m.Mp, "fields", ArrayOf(fields))
		return m
	case *IndexExpr:
		m := nodeBase("index", n.Pos(), n.String())
		SetMap(m.Mp, "base", exprValue(n.Base))
		SetMap(m.Mp, "index", exprValue(n.Index))
		return m
	case *FieldExpr:
		m := nodeBase("field", n.Pos(), n.String())
		SetMap(m.Mp, "base", exprValue(n.Base))
		SetMap(m.Mp, "field", Str(n.Field))
		return m
	case *CallExpr:
		m := nodeBase("call", n.Pos(), n.String())
		SetMap(m.Mp, "callee", exprValue(n.Callee))
		args := make([]Value, 0, len(n.Args))
		for _, a := range n.Args {
			args = append(args, exprValue(a))
		}
		SetMap(m.Mp, "args", ArrayOf(args))
		return m
	case *UnaryExpr:
		m := nodeBase("unary", n.Pos(), n.String())
		SetMap(m.Mp, "op", Str(opSymbol(n.Op)))
		SetMap(m.Mp, "x", exprValue(n.X))
		return m
	case *BinaryExpr:
		m := nodeBase("binary", n.Pos(), n.String())
		SetMap(m.Mp, "op", Str(opSymbol(n.Op)))
		SetMap(m.Mp, "left", exprValue(n.L))
		SetMap(m.Mp, "right", exprValue(n.R))
		return m
	default:
		return nodeBase("unknown", x.Pos(), x.String())
	}
}
