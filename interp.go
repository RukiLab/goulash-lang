// Tree-walk interpreter for HSP successor language v0.1.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
)

// Decisions (v0.1):
//   - `/` on two ints is truncating integer division (Go-like); any float
//     operand yields a float. `%` accepts ints only.
//   - Conditions are strictly bool; `if 1` is an error.
//   - `;` is rejected by the parser.
//   - Array writes are bounds-checked: allocate with dim(), a literal,
//     or push(); out-of-range and missing bases are errors, never
//     auto-created. Map writes create missing keys; map reads of
//     missing keys (both ["k"] and .k) are errors.
//   - `b = a` shares the array (reference semantics).
//   - `+` with either side a string concatenates via stringify.

// RuntimeError is an execution error with source position.
type RuntimeError struct {
	File   string
	Line   int
	Column int
	Msg    string
}

func (e *RuntimeError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Msg)
	}
	return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Msg)
}

func rtErrf(at Pos, format string, args ...any) *RuntimeError {
	return &RuntimeError{File: at.File, Line: at.Line, Column: at.Column, Msg: fmt.Sprintf(format, args...)}
}

// Control-flow signals (unwind via panic; converted at loop/call boundaries).
type breakSignal struct{}
type continueSignal struct{}
type returnSignal struct{ v Value }

// Env is a lexical scope.
type Env struct {
	vars     map[string]Value
	readonly map[string]bool
	parent   *Env
}

func NewEnv(parent *Env) *Env {
	return &Env{vars: map[string]Value{}, readonly: map[string]bool{}, parent: parent}
}

func (e *Env) find(name string) (*Env, bool) {
	for cur := e; cur != nil; cur = cur.parent {
		if _, ok := cur.vars[name]; ok {
			return cur, true
		}
	}
	return nil, false
}

// Lookup searches the scope chain.
func (e *Env) Lookup(name string) (Value, bool) {
	if scope, ok := e.find(name); ok {
		return scope.vars[name], true
	}
	return Null(), false
}

// Define binds a name in the current scope.
func (e *Env) Define(name string, v Value) {
	e.vars[name] = v
	delete(e.readonly, name)
}

// Assign updates the defining scope, or defines in the current scope when new.
func (e *Env) Assign(name string, v Value, at Pos) error {
	if scope, ok := e.find(name); ok {
		if scope.readonly[name] {
			return rtErrf(at, "%q に代入できません：repeat カウンタは読み取り専用です", name)
		}
		scope.vars[name] = v
		return nil
	}
	e.vars[name] = v
	return nil
}

// Interp holds global state shared across statements (and REPL inputs).
type Interp struct {
	globals *Env
	be      Backend
	in      io.Reader
	errOut  io.Writer
	// Written by end() on the script goroutine, read by the frontend
	// after the run; atomic so a GUI window closed mid-script cannot race.
	exitCode atomic.Pointer[int]
	cliArgs  []string
}

// NewInterp creates an interpreter writing output to out and reading input
// from os.Stdin.
func NewInterp(out io.Writer) *Interp {
	return NewInterpWithIO(out, os.Stdin)
}

// NewInterpWithIO creates an interpreter with explicit output/input streams
// (used by tests).
func NewInterpWithIO(out io.Writer, in io.Reader) *Interp {
	return &Interp{globals: NewEnv(nil), be: NewConsoleBackend(out, in), in: in, errOut: os.Stderr}
}

// NewInterpWithBackend creates an interpreter over an arbitrary Backend
// (used by the GUI frontend).
func NewInterpWithBackend(be Backend, in io.Reader) *Interp {
	return &Interp{globals: NewEnv(nil), be: be, in: in, errOut: os.Stderr}
}

// SetArgs stores command-line arguments for args() (used by run).
func (in *Interp) SetArgs(args []string) {
	in.cliArgs = args
}

// ExitCode reports the code requested by end(), if any.
func (in *Interp) ExitCode() (int, bool) {
	if p := in.exitCode.Load(); p != nil {
		return *p, true
	}
	return 0, false
}

// Backend exposes the IO backend (used by gui-tag builtins).
func (in *Interp) Backend() Backend {
	return in.be
}

// EvalGlobal evaluates an expression at global scope (used by the REPL to
// echo bare expression values).
func (in *Interp) EvalGlobal(x Expr) (Value, error) {
	return in.evalExpr(x, in.globals)
}

// Run executes a program at global scope.
func (in *Interp) Run(prog *Program) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case breakSignal:
				err = &RuntimeError{Msg: "ループ外の break です"}
			case continueSignal:
				err = &RuntimeError{Msg: "ループ外の continue です"}
			case returnSignal:
				err = &RuntimeError{Msg: "関数外の return です"}
			case endSignal:
				err = nil // exitCode carries the result
			default:
				panic(r)
			}
		}
	}()
	return in.execBlock(prog.Stmts, in.globals)
}

func (in *Interp) execBlock(stmts []Stmt, env *Env) error {
	for _, s := range stmts {
		if err := in.execStmt(s, env); err != nil {
			return err
		}
	}
	return nil
}

func (in *Interp) execStmt(s Stmt, env *Env) error {
	switch n := s.(type) {
	case *ExprStmt:
		_, err := in.evalExpr(n.X, env)
		return err
	case *AssignStmt:
		v, err := in.evalExpr(n.Value, env)
		if err != nil {
			return err
		}
		return in.assign(n.Target, v, env)
	case *DefStmt:
		seen := map[string]bool{}
		for _, p := range n.Params {
			if seen[p.Name] {
				return rtErrf(p.At, "関数 %q にパラメータ %q が重複しています", n.Name, p.Name)
			}
			seen[p.Name] = true
		}
		if scope, ok := env.find(n.Name); ok && scope == env && scope.readonly[n.Name] {
			return rtErrf(n.At, "%q を再定義できません：repeat カウンタは読み取り専用です", n.Name)
		}
		if isBuiltin(n.Name) {
			return rtErrf(n.At, "%q を再定義できません：組み込み関数です", n.Name)
		}
		env.Define(n.Name, FuncOf(&FuncVal{Name: n.Name, Params: n.Params, Body: n.Body, Closure: env}))
		return nil
	case *IfStmt:
		c, err := in.evalExpr(n.Cond, env)
		if err != nil {
			return err
		}
		b, err := requireBool(c, n.Cond.Pos())
		if err != nil {
			return err
		}
		if b {
			return in.execBlock(n.Then.Stmts, env)
		}
		switch e := n.Else.(type) {
		case nil:
			return nil
		case *BlockStmt:
			return in.execBlock(e.Stmts, env)
		case *IfStmt:
			return in.execStmt(e, env)
		default:
			return rtErrf(n.At, "内部エラー：不正な else 分岐です")
		}
	case *RepeatStmt:
		cv, err := in.evalExpr(n.Count, env)
		if err != nil {
			return err
		}
		switch cv.K {
		case KArray:
			return in.execRepeatArray(n, cv.Arr, env)
		case KInt:
		default:
			return rtErrf(n.Count.Pos(), "repeat は整数または配列が必要です。%s が指定されました", typeNameOf(cv))
		}
		if n.HasItem {
			return rtErrf(n.At, "整数の repeat では変数は 1 つまでです（as i, item は配列専用です）")
		}
		if cv.I < 0 {
			return rtErrf(n.Count.Pos(), "repeat の回数は 0 以上である必要があります。%d が指定されました", cv.I)
		}
		// The counter lives in the current scope only for the loop duration:
		// save any previous binding and restore it afterwards (so shadowing
		// and nesting behave, and the counter is invisible after the loop).
		if n.HasVar {
			oldVal, hadVal := env.vars[n.Var]
			oldRO := env.readonly[n.Var]
			env.vars[n.Var] = Int(0)
			env.readonly[n.Var] = true
			defer func() {
				if hadVal {
					env.vars[n.Var] = oldVal
				} else {
					delete(env.vars, n.Var)
				}
				if oldRO {
					env.readonly[n.Var] = true
				} else {
					delete(env.readonly, n.Var)
				}
			}()
		}
		for k := int64(0); k < cv.I; k++ {
			if n.HasVar {
				env.vars[n.Var] = Int(k)
			}
			brk, _, err := in.execLoopBody(n.Body.Stmts, env)
			if err != nil {
				return err
			}
			if brk {
				break
			}
		}
		return nil
	case *BreakStmt:
		panic(breakSignal{})
	case *ContinueStmt:
		panic(continueSignal{})
	case *SwitchStmt:
		return in.execSwitch(n, env)
	case *WhileStmt:
		for {
			c, err := in.evalExpr(n.Cond, env)
			if err != nil {
				return err
			}
			b, err := requireBool(c, n.Cond.Pos())
			if err != nil {
				return err
			}
			if !b {
				return nil
			}
			brk, _, err := in.execLoopBody(n.Body.Stmts, env)
			if err != nil {
				return err
			}
			if brk {
				return nil
			}
		}
	case *ReturnStmt:
		if n.Value == nil {
			panic(returnSignal{v: Null()})
		}
		v, err := in.evalExpr(n.Value, env)
		if err != nil {
			return err
		}
		panic(returnSignal{v: v})
	case *BlockStmt:
		return in.execBlock(n.Stmts, env)
	case *TryStmt:
		return in.execTry(n, env)
	}
	return rtErrf(s.Pos(), "内部エラー：不明な文です")
}

// execTry runs the body; on error it binds the message to Var and runs
// the catch body. Control-flow panics (break/continue/return/end)
// propagate untouched since they are not error returns.
func (in *Interp) execTry(n *TryStmt, env *Env) error {
	if err := in.execBlock(n.Body.Stmts, env); err == nil {
		return nil
	} else {
		oldVal, hadVal := env.vars[n.Var]
		oldRO := env.readonly[n.Var]
		env.vars[n.Var] = Str(err.Error())
		env.readonly[n.Var] = true
		defer func() {
			if hadVal {
				env.vars[n.Var] = oldVal
			} else {
				delete(env.vars, n.Var)
			}
			if oldRO {
				env.readonly[n.Var] = true
			} else {
				delete(env.readonly, n.Var)
			}
		}()
		return in.execBlock(n.Catch.Stmts, env)
	}
}

// execRepeatArray iterates array elements: `repeat arr as x` binds each
// element to x (writable copy), `repeat arr as i, x` also binds the
// index to i (read-only). Iteration count is fixed at loop start;
// assigning x never writes back (use a[i] = ... for that).
func (in *Interp) execRepeatArray(n *RepeatStmt, arr *Array, env *Env) error {
	elems := append([]Value(nil), arr.Elems...)
	// Roles: `as x` binds the element (writable); `as i, x` binds the
	// index to i (read-only) and the element to x (writable).
	indexVar, elemVar := "", ""
	if n.HasItem {
		indexVar, elemVar = n.Var, n.Item
	} else if n.HasVar {
		elemVar = n.Var
	}
	// Loop variables shadow outer bindings for the loop duration only
	// (same save/restore discipline as the int counter).
	if indexVar != "" {
		oldVal, hadVal := env.vars[indexVar]
		oldRO := env.readonly[indexVar]
		env.vars[indexVar] = Int(0)
		env.readonly[indexVar] = true
		defer func() {
			if hadVal {
				env.vars[indexVar] = oldVal
			} else {
				delete(env.vars, indexVar)
			}
			if oldRO {
				env.readonly[indexVar] = true
			} else {
				delete(env.readonly, indexVar)
			}
		}()
	}
	if elemVar != "" {
		oldVal, hadVal := env.vars[elemVar]
		oldRO := env.readonly[elemVar]
		defer func() {
			if hadVal {
				env.vars[elemVar] = oldVal
			} else {
				delete(env.vars, elemVar)
			}
			if oldRO {
				env.readonly[elemVar] = true
			} else {
				delete(env.readonly, elemVar)
			}
		}()
	}
	for idx, elem := range elems {
		if indexVar != "" {
			env.vars[indexVar] = Int(int64(idx))
		}
		if elemVar != "" {
			env.vars[elemVar] = elem
		}
		brk, _, err := in.execLoopBody(n.Body.Stmts, env)
		if err != nil {
			return err
		}
		if brk {
			break
		}
	}
	return nil
}

// execBlockWithSignals runs stmts, translating break/continue panics for loops.
func (in *Interp) execLoopBody(stmts []Stmt, env *Env) (brk, cont bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case breakSignal:
				brk, err = true, nil
			case continueSignal:
				cont, err = true, nil
			default:
				panic(r)
			}
		}
	}()
	err = in.execBlock(stmts, env)
	return false, false, err
}

// execSwitch evaluates the scrutinee once and runs the first matching
// case (no fallthrough). A `break` inside a case ends the switch;
// `continue`, `return` and errors propagate to the enclosing context.
func (in *Interp) execSwitch(n *SwitchStmt, env *Env) (err error) {
	v, err := in.evalExpr(n.Value, env)
	if err != nil {
		return err
	}
	var body *BlockStmt
outer:
	for _, c := range n.Cases {
		if c.Default {
			continue
		}
		for _, vx := range c.Values {
			cv, err := in.evalExpr(vx, env)
			if err != nil {
				return err
			}
			if valuesEqual(v, cv) {
				body = c.Body
				break outer
			}
		}
	}
	if body == nil {
		for _, c := range n.Cases {
			if c.Default {
				body = c.Body
				break
			}
		}
	}
	if body == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(breakSignal); ok {
				err = nil // break ends the switch
			} else {
				panic(r)
			}
		}
	}()
	return in.execBlock(body.Stmts, env)
}

// assign implements `target = value`.
func (in *Interp) assign(target Expr, v Value, env *Env) error {
	switch t := target.(type) {
	case *VarExpr:
		return env.Assign(t.Name, v, t.At)
	case *IndexExpr:
		return in.assignIndex(t, v, env)
	case *FieldExpr:
		base, err := in.evalExpr(t.Base, env)
		if err != nil {
			return err
		}
		if base.K != KMap {
			return rtErrf(t.At, "フィールド %q に代入できません：値は %s であり、map ではありません", t.Field, typeNameOf(base))
		}
		SetMap(base.Mp, t.Field, v)
		return nil
	}
	return rtErrf(target.Pos(), "'=' の左辺に代入できません")
}

func (in *Interp) assignIndex(t *IndexExpr, v Value, env *Env) error {
	idxv, err := in.evalExpr(t.Index, env)
	if err != nil {
		return err
	}
	// Map path (string keys), including nested m["a"]["b"].
	if idxv.K == KString {
		return in.assignMapIndex(t, idxv.S, v, env)
	}
	if idxv.K != KInt {
		return rtErrf(t.Index.Pos(), "配列のインデックスは整数である必要があります。%s が指定されました", typeNameOf(idxv))
	}
	if idxv.I < 0 {
		return rtErrf(t.Index.Pos(), "配列のインデックスは 0 以上である必要があります。%d が指定されました", idxv.I)
	}
	idx := int(idxv.I)
	// Nested target like a[0][1]: resolve the inner base to an array value.
	// A map under an int index (m["a"][0]) resolves symmetrically.
	if inner, ok := t.Base.(*IndexExpr); ok {
		base, err := in.evalExpr(inner.Base, env)
		if err != nil {
			return err
		}
		midv, err := in.evalExpr(inner.Index, env)
		if err != nil {
			return err
		}
		if base.K == KMap {
			if midv.K != KString {
				return rtErrf(inner.Index.Pos(), "map のキーは文字列である必要があります。%s が指定されました", typeNameOf(midv))
			}
			elem, ok := base.Mp.Fields[midv.S]
			if !ok || elem.K != KArray {
				return rtErrf(inner.At, "%s にインデックスでアクセスできません（[...] は配列と map に対応しています）", typeNameOf(elem))
			}
			if err := setIndex(elem.Arr, idx, v, t.At); err != nil {
				return err
			}
			return nil
		}
		arr, err := in.indexBaseForWrite(inner, base, env)
		if err != nil {
			return err
		}
		if midv.K != KInt {
			return rtErrf(inner.Index.Pos(), "配列のインデックスは整数である必要があります。%s が指定されました", typeNameOf(midv))
		}
		if midv.I < 0 || int(midv.I) >= len(arr.Elems) {
			return rtErrf(inner.At, "インデックス %d は範囲外です（長さ %d）", midv.I, len(arr.Elems))
		}
		elem := arr.Elems[int(midv.I)]
		if elem.K != KArray {
			return rtErrf(inner.At, "%s にインデックスでアクセスできません（[...] は配列と map に対応しています）", typeNameOf(elem))
		}
		if err := setIndex(elem.Arr, idx, v, t.At); err != nil {
			return err
		}
		return nil
	}
	base, err := in.evalExpr(t.Base, env)
	if err != nil {
		return err
	}
	arr, err := in.indexBaseForWrite(t, base, env)
	if err != nil {
		return err
	}
	if err := setIndex(arr, idx, v, t.At); err != nil {
		return err
	}
	return nil
}

// assignMapIndex implements `m["k"] = v` (string keys). Missing keys are
// created; nested targets like m["a"]["b"] or a[0]["b"] resolve one level
// (a missing middle is an error, never auto-created).
func (in *Interp) assignMapIndex(t *IndexExpr, key string, v Value, env *Env) error {
	if inner, ok := t.Base.(*IndexExpr); ok {
		base, err := in.evalExpr(inner.Base, env)
		if err != nil {
			return err
		}
		midv, err := in.evalExpr(inner.Index, env)
		if err != nil {
			return err
		}
		if base.K == KArray {
			if midv.K != KInt {
				return rtErrf(inner.Index.Pos(), "配列のインデックスは整数である必要があります。%s が指定されました", typeNameOf(midv))
			}
			if midv.I < 0 || int(midv.I) >= len(base.Arr.Elems) {
				return rtErrf(inner.At, "インデックス %d は範囲外です（長さ %d）", midv.I, len(base.Arr.Elems))
			}
			elem := base.Arr.Elems[int(midv.I)]
			if elem.K != KMap {
				return rtErrf(inner.At, "%s にインデックスでアクセスできません（[...] は配列と map に対応しています）", typeNameOf(elem))
			}
			SetMap(elem.Mp, key, v)
			return nil
		}
		if base.K != KMap {
			return rtErrf(inner.At, "%s にインデックスでアクセスできません（[...] は配列と map に対応しています）", typeNameOf(base))
		}
		if midv.K != KString {
			return rtErrf(inner.Index.Pos(), "map のキーは文字列である必要があります。%s が指定されました", typeNameOf(midv))
		}
		elem, ok := base.Mp.Fields[midv.S]
		if !ok || elem.K != KMap {
			return rtErrf(inner.At, "%s にインデックスでアクセスできません（[...] は配列と map に対応しています）", typeNameOf(elem))
		}
		SetMap(elem.Mp, key, v)
		return nil
	}
	base, err := in.evalExpr(t.Base, env)
	if err != nil {
		// Auto-create: `m["k"] = v` with undefined `m` makes a map.
		if _, ok := err.(*RuntimeError); ok {
			if bv, isVar := t.Base.(*VarExpr); isVar && isUndefinedVar(err, bv.Name) {
				mv := MapOf()
				SetMap(mv.Mp, key, v)
				env.Define(bv.Name, mv)
				return nil
			}
		}
		return err
	}
	if base.K == KNull {
		if bv, ok := t.Base.(*VarExpr); ok {
			mv := MapOf()
			SetMap(mv.Mp, key, v)
			env.Define(bv.Name, mv)
			return nil
		}
	}
	if base.K != KMap {
		return rtErrf(t.At, "%s にインデックスでアクセスできません（[...] は配列と map に対応しています）", typeNameOf(base))
	}
	SetMap(base.Mp, key, v)
	return nil
}

// indexBaseForWrite resolves the array being written through [...] (single level).
// The array must already exist (dim, literal, or push); out-of-range and
// missing bases are errors, never auto-created.
func (in *Interp) indexBaseForWrite(t *IndexExpr, base Value, env *Env) (*Array, error) {
	if base.K == KArray {
		return base.Arr, nil
	}
	return nil, rtErrf(t.At, "%s にインデックスでアクセスできません（[...] は配列と map に対応しています）", typeNameOf(base))
}

// setIndex stores within bounds; out-of-range is an error.
// Grow arrays with dim() (up front) or push() (at the tail).
func setIndex(arr *Array, idx int, v Value, at Pos) error {
	if idx < 0 || idx >= len(arr.Elems) {
		return rtErrf(at, "インデックス %d は範囲外です（長さ %d）", idx, len(arr.Elems))
	}
	arr.Elems[idx] = v
	return nil
}

func isUndefinedVar(err error, name string) bool {
	re, ok := err.(*RuntimeError)
	if !ok {
		return false
	}
	return strings.Contains(re.Msg, fmt.Sprintf("未定義の変数 %q です", name)) || strings.Contains(re.Msg, fmt.Sprintf("undefined variable %q", name))
}

// ---------- expressions ----------

func (in *Interp) evalExpr(x Expr, env *Env) (Value, error) {
	switch n := x.(type) {
	case *IntLit:
		return Int(n.Value), nil
	case *FloatLit:
		return Float(n.Value), nil
	case *StringLit:
		return Str(n.Value), nil
	case *BoolLit:
		return Bool(n.Value), nil
	case *NullLit:
		return Null(), nil
	case *VarExpr:
		v, ok := env.Lookup(n.Name)
		if !ok {
			return Null(), rtErrf(n.At, "未定義の変数 %q です", n.Name)
		}
		return v, nil
	case *ArrayLit:
		elems := make([]Value, len(n.Elems))
		for i, e := range n.Elems {
			v, err := in.evalExpr(e, env)
			if err != nil {
				return Null(), err
			}
			elems[i] = v
		}
		return ArrayOf(elems), nil
	case *MapLit:
		mv := MapOf()
		for _, f := range n.Fields {
			kv, err := in.evalExpr(f.Key, env)
			if err != nil {
				return Null(), err
			}
			if kv.K != KString {
				return Null(), rtErrf(f.Key.Pos(), "map のキーは文字列である必要があります。%s が指定されました", typeNameOf(kv))
			}
			v, err := in.evalExpr(f.Value, env)
			if err != nil {
				return Null(), err
			}
			SetMap(mv.Mp, kv.S, v) // later keys win
		}
		return mv, nil
	case *IndexExpr:
		base, err := in.evalExpr(n.Base, env)
		if err != nil {
			return Null(), err
		}
		idxv, err := in.evalExpr(n.Index, env)
		if err != nil {
			return Null(), err
		}
		if base.K == KMap {
			if idxv.K != KString {
				return Null(), rtErrf(n.Index.Pos(), "map のキーは文字列である必要があります。%s が指定されました", typeNameOf(idxv))
			}
			v, ok := base.Mp.Fields[idxv.S]
			if !ok {
				return Null(), rtErrf(n.At, "map にキー %q がありません", idxv.S)
			}
			return v, nil
		}
		if idxv.K != KInt {
			return Null(), rtErrf(n.Index.Pos(), "配列のインデックスは整数である必要があります。%s が指定されました", typeNameOf(idxv))
		}
		if base.K != KArray {
			return Null(), rtErrf(n.At, "%s にインデックスでアクセスできません（[...] は配列と map に対応しています）", typeNameOf(base))
		}
		if idxv.I < 0 {
			return Null(), rtErrf(n.Index.Pos(), "配列のインデックスは 0 以上である必要があります。%d が指定されました", idxv.I)
		}
		if int(idxv.I) >= len(base.Arr.Elems) {
			return Null(), rtErrf(n.At, "インデックス %d は範囲外です（長さ %d）", idxv.I, len(base.Arr.Elems))
		}
		return base.Arr.Elems[int(idxv.I)], nil
	case *FieldExpr:
		base, err := in.evalExpr(n.Base, env)
		if err != nil {
			return Null(), err
		}
		if base.K != KMap {
			return Null(), rtErrf(n.At, "%s のフィールド %q にアクセスできません（'.' は map のみ対応しています）", typeNameOf(base), n.Field)
		}
		v, ok := base.Mp.Fields[n.Field]
		if !ok {
			return Null(), rtErrf(n.At, "map にキー %q がありません", n.Field)
		}
		return v, nil
	case *CallExpr:
		return in.evalCall(n, env)
	case *UnaryExpr:
		v, err := in.evalExpr(n.X, env)
		if err != nil {
			return Null(), err
		}
		switch n.Op {
		case TokMinus:
			switch v.K {
			case KInt:
				return Int(-v.I), nil
			case KFloat:
				return Float(-v.F), nil
			}
			return Null(), rtErrf(n.At, "%s を負数化できません", typeNameOf(v))
		case TokBang:
			b, err := requireBool(v, n.X.Pos())
			if err != nil {
				return Null(), err
			}
			return Bool(!b), nil
		case TokBitNot:
			if v.K != KInt {
				return Null(), rtErrf(n.At, "%s に ~ を適用できません（整数のみ対応しています）", typeNameOf(v))
			}
			return Int(^v.I), nil
		}
		return Null(), rtErrf(n.At, "内部エラー：不正な単項演算子です")
	case *BinaryExpr:
		return in.evalBinary(n, env)
	}
	return Null(), rtErrf(x.Pos(), "内部エラー：不明な式です")
}

func (in *Interp) evalCall(n *CallExpr, env *Env) (Value, error) {
	// Named calls: user functions first, builtins second.
	if v, ok := n.Callee.(*VarExpr); ok {
		if fv, ok := env.Lookup(v.Name); ok {
			if fv.K == KArray {
				return Null(), rtErrf(n.At, "値は配列です。配列へのアクセスは [...] を使用してください（例：a[0]、a(0) ではありません）")
			}
			if fv.K != KFunc {
				return Null(), rtErrf(n.At, "%q は %s であり、関数ではありません", v.Name, typeNameOf(fv))
			}
			return in.callChecked(fv.Fn, n.Args, env, n.At)
		}
		if isBuiltin(v.Name) {
			args := make([]Value, len(n.Args))
			for i, a := range n.Args {
				ev, err := in.evalExpr(a, env)
				if err != nil {
					return Null(), err
				}
				args[i] = ev
			}
			return callBuiltin(v.Name, in, args, n.At)
		}
		return Null(), rtErrf(n.At, "未定義の関数 %q です", v.Name)
	}
	callee, err := in.evalExpr(n.Callee, env)
	if err != nil {
		return Null(), err
	}
	if callee.K == KArray {
		return Null(), rtErrf(n.At, "値は配列です。配列へのアクセスは [...] を使用してください（例：a[0]、a(0) ではありません）")
	}
	if callee.K != KFunc {
		name := n.Callee.String()
		if v, ok := n.Callee.(*VarExpr); ok {
			return Null(), rtErrf(n.At, "未定義の関数 %q です", v.Name)
		}
		return Null(), rtErrf(n.At, "%s を呼び出せません（%s は関数ではありません）", name, typeNameOf(callee))
	}
	fn := callee.Fn
	return in.callChecked(fn, n.Args, env, n.At)
}

// callChecked evaluates call arguments and invokes fn.
// Missing trailing arguments are filled from default expressions,
// evaluated in the caller's environment at call time.
func (in *Interp) callChecked(fn *FuncVal, argExprs []Expr, env *Env, at Pos) (Value, error) {
	if len(argExprs) > len(fn.Params) {
		return Null(), rtErrf(at, "関数 %s は引数を %d 個必要としますが、%d 個が渡されました", fn.Name, len(fn.Params), len(argExprs))
	}
	args := make([]Value, len(fn.Params))
	for i, a := range argExprs {
		ev, err := in.evalExpr(a, env)
		if err != nil {
			return Null(), err
		}
		args[i] = ev
	}
	for i := len(argExprs); i < len(fn.Params); i++ {
		d := fn.Params[i].Default
		if d == nil {
			return Null(), rtErrf(at, "関数 %s は引数を %d 個必要としますが、%d 個が渡されました", fn.Name, len(fn.Params), len(argExprs))
		}
		ev, err := in.evalExpr(d, env)
		if err != nil {
			return Null(), err
		}
		args[i] = ev
	}
	return in.callFunc(fn, args, at)
}

func (in *Interp) callFunc(fn *FuncVal, args []Value, at Pos) (v Value, err error) {
	if len(args) != len(fn.Params) {
		return Null(), rtErrf(at, "関数 %s は引数を %d 個必要としますが、%d 個が渡されました", fn.Name, len(fn.Params), len(args))
	}
	callEnv := NewEnv(fn.Closure)
	for i, p := range fn.Params {
		callEnv.Define(p.Name, args[i])
	}
	defer func() {
		if r := recover(); r != nil {
			switch sig := r.(type) {
			case returnSignal:
				v, err = sig.v, nil
			case breakSignal:
				v, err = Null(), rtErrf(at, "ループ外の break です（関数呼び出しをまたぐことはできません）")
			case continueSignal:
				v, err = Null(), rtErrf(at, "ループ外の continue です（関数呼び出しをまたぐことはできません）")
			default:
				panic(r)
			}
		}
	}()
	if err := in.execBlock(fn.Body.Stmts, callEnv); err != nil {
		return Null(), err
	}
	return Null(), nil
}

func (in *Interp) evalBinary(n *BinaryExpr, env *Env) (Value, error) {
	// Short-circuit logical operators (strict bool).
	if n.Op == TokAnd || n.Op == TokOr {
		l, err := in.evalExpr(n.L, env)
		if err != nil {
			return Null(), err
		}
		lb, err := requireBool(l, n.L.Pos())
		if err != nil {
			return Null(), err
		}
		if n.Op == TokAnd && !lb {
			return Bool(false), nil
		}
		if n.Op == TokOr && lb {
			return Bool(true), nil
		}
		r, err := in.evalExpr(n.R, env)
		if err != nil {
			return Null(), err
		}
		rb, err := requireBool(r, n.R.Pos())
		if err != nil {
			return Null(), err
		}
		return Bool(rb), nil
	}
	l, err := in.evalExpr(n.L, env)
	if err != nil {
		return Null(), err
	}
	r, err := in.evalExpr(n.R, env)
	if err != nil {
		return Null(), err
	}
	switch n.Op {
	case TokEq:
		return Bool(valuesEqual(l, r)), nil
	case TokNotEq:
		return Bool(!valuesEqual(l, r)), nil
	case TokLt, TokLtEq, TokGt, TokGtEq:
		return compare(n.Op, l, r, n.At)
	case TokPlus:
		return add(l, r, n.At)
	case TokMinus, TokStar, TokSlash, TokMod:
		return arith(n.Op, l, r, n.At)
	case TokBitAnd, TokBitOr, TokBitXor:
		return bitwise(n.Op, l, r, n.At)
	case TokShl, TokShr:
		return shift(n.Op, l, r, n.At)
	}
	return Null(), rtErrf(n.At, "内部エラー：不正な二項演算子です")
}

func requireBool(v Value, at Pos) (bool, error) {
	if v.K != KBool {
		return false, rtErrf(at, "条件式は bool 型である必要があります。%s が指定されました", typeNameOf(v))
	}
	return v.B, nil
}

func isNum(v Value) bool { return v.K == KInt || v.K == KFloat }
func toFloat(v Value) float64 {
	if v.K == KFloat {
		return v.F
	}
	return float64(v.I)
}

func compare(op TokenType, l, r Value, at Pos) (Value, error) {
	var c int // -1, 0, +1
	switch {
	case isNum(l) && isNum(r):
		lf, rf := toFloat(l), toFloat(r)
		switch {
		case lf < rf:
			c = -1
		case lf > rf:
			c = 1
		}
	case l.K == KString && r.K == KString:
		c = strings.Compare(l.S, r.S)
	default:
		return Null(), rtErrf(at, "%s と %s を比較できません（< <= > >= は数値または文字列のみ対応しています）", typeNameOf(l), typeNameOf(r))
	}
	switch op {
	case TokLt:
		return Bool(c < 0), nil
	case TokLtEq:
		return Bool(c <= 0), nil
	case TokGt:
		return Bool(c > 0), nil
	case TokGtEq:
		return Bool(c >= 0), nil
	}
	return Null(), rtErrf(at, "内部エラー：不正な比較演算子です")
}

func add(l, r Value, at Pos) (Value, error) {
	if l.K == KString || r.K == KString {
		return Str(Stringify(l) + Stringify(r)), nil
	}
	if isNum(l) && isNum(r) {
		if l.K == KFloat || r.K == KFloat {
			return Float(toFloat(l) + toFloat(r)), nil
		}
		return Int(l.I + r.I), nil
	}
	return Null(), rtErrf(at, "%s と %s を加算できません", typeNameOf(l), typeNameOf(r))
}

func arith(op TokenType, l, r Value, at Pos) (Value, error) {
	if !isNum(l) || !isNum(r) {
		return Null(), rtErrf(at, "演算子 %s は数値が必要です。%s と %s が指定されました", string(op), typeNameOf(l), typeNameOf(r))
	}
	if op == TokMod {
		if l.K != KInt || r.K != KInt {
			return Null(), rtErrf(at, "演算子 %% は整数が必要です。%s と %s が指定されました", typeNameOf(l), typeNameOf(r))
		}
		if r.I == 0 {
			return Null(), rtErrf(at, "0 による剰余演算です")
		}
		return Int(l.I % r.I), nil
	}
	if l.K == KFloat || r.K == KFloat {
		lf, rf := toFloat(l), toFloat(r)
		switch op {
		case TokMinus:
			return Float(lf - rf), nil
		case TokStar:
			return Float(lf * rf), nil
		case TokSlash:
			if rf == 0 {
				return Null(), rtErrf(at, "0 による除算です")
			}
			return Float(lf / rf), nil
		}
	}
	switch op {
	case TokMinus:
		return Int(l.I - r.I), nil
	case TokStar:
		return Int(l.I * r.I), nil
	case TokSlash:
		if r.I == 0 {
			return Null(), rtErrf(at, "0 による除算です")
		}
		return Int(l.I / r.I), nil
	}
	return Null(), rtErrf(at, "内部エラー：不正な算術演算子です")
}

// bitwise implements & | ^ (integers only, like %).
func bitwise(op TokenType, l, r Value, at Pos) (Value, error) {
	if l.K != KInt || r.K != KInt {
		return Null(), rtErrf(at, "演算子 %s は整数が必要です。%s と %s が指定されました", opSymbol(op), typeNameOf(l), typeNameOf(r))
	}
	switch op {
	case TokBitAnd:
		return Int(l.I & r.I), nil
	case TokBitOr:
		return Int(l.I | r.I), nil
	case TokBitXor:
		return Int(l.I ^ r.I), nil
	}
	return Null(), rtErrf(at, "内部エラー：不正なビット演算子です")
}

// shift implements << >> (integers only; arithmetic right shift).
func shift(op TokenType, l, r Value, at Pos) (Value, error) {
	if l.K != KInt || r.K != KInt {
		return Null(), rtErrf(at, "演算子 %s は整数が必要です。%s と %s が指定されました", opSymbol(op), typeNameOf(l), typeNameOf(r))
	}
	if r.I < 0 || r.I > 63 {
		return Null(), rtErrf(at, "シフト数は 0 から 63 の範囲で指定してください。%d が指定されました", r.I)
	}
	if op == TokShl {
		return Int(l.I << uint(r.I)), nil
	}
	return Int(l.I >> uint(r.I)), nil
}
