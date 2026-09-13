// Goulash v0.2 AST → レジスタVMバイトコード・コンパイラ。
//
// 意味論の対応付け（詳細は docs/DESIGN_VM.md）：
//   - 評価順序は interp.go と同一（RHS先行・L→R・添字系の内外順序まで再現）。
//   - void/型/境界の検査位置は、検査命令（CKVAL/CKINT/CKNEG/CKARR）を
//     正しい Pos 付きで先行発行することで再現する。
//   - def は実行時登録（FUNCDEF）。前方参照は未定義エラーになる。
//   - repeat の束縛退避・復元は SAVEVAR/POPLOOP＋VM側ループスタックで行い、
//     break/return/エラー時の復元順序も interp.go の defer と一致させる。
package main

import "fmt"

// Compile は構文木を VM プログラムに変換する。parser が通した正規の
// プログラムに対して失敗するのは、レジスタ数（255）超過などの内部限界のみ。
func Compile(prog *Program) (*VMProgram, error) {
	g := &vmcGlobal{
		constMap: map[string]int{},
		nameMap:  map[string]int{},
		strMap:   map[string]int{},
	}
	m := &fcomp{g: g, isMain: true}
	if err := m.block(prog.Stmts); err != nil {
		return nil, err
	}
	m.emit(OpHalt, 0, 0, 0, 0, prog.At)
	mainProto, err := m.finish("main", nil)
	if err != nil {
		return nil, err
	}
	return &VMProgram{Main: mainProto, Protos: g.protos}, nil
}

// vmcGlobal はプログラム全体で共有するプール。
type vmcGlobal struct {
	consts   []Value
	constMap map[string]int
	names    []string
	nameMap  map[string]int
	strs     []string
	strMap   map[string]int
	protos   []*VMProto
}

func constKey(v Value) string {
	switch v.K {
	case KNull:
		return "null"
	case KInt:
		return fmt.Sprintf("i%d", v.I)
	case KFloat:
		return fmt.Sprintf("f%x", v.F)
	case KString:
		return "s" + v.S
	case KBool:
		return fmt.Sprintf("b%v", v.B)
	}
	return "?"
}

// fcomp は1プロトタイプ分のコンパイル状態。
type fcomp struct {
	g      *vmcGlobal
	code   []Instr
	pos    []Pos
	isMain bool
	// 関数内スロット（main では未使用）。
	slots    map[string]int
	numSlots int
	// 一時レジスタのバンプ割付。
	tempTop int
	maxRegs int
	maxArgs int
	// break/continue の解決スタック。
	ctxt []blockCtx
	// ジャンプパッチ。
	patches  []jumpPatch
	labels   map[int]int
	labelSeq int
}

type blockCtx struct {
	isLoop   bool
	isRepeat bool // repeat のみループスタックを積む（break 時に pop 要）
	end      int  // 終端ラベル
	cont     int  // 継続ラベル（ループのみ有効）
}

type jumpPatch struct {
	at    int
	label int
}

func (f *fcomp) newLabel() int {
	if f.labels == nil {
		f.labels = map[int]int{}
	}
	// ラベル ID の衝突を避けるため単調カウンタで発行する。
	f.labelSeq++
	return f.labelSeq
}

func (f *fcomp) bindLabel(id int) {
	f.labels[id] = len(f.code)
}

func (f *fcomp) emit(op Op, a, b, c uint8, d uint32, at Pos) {
	f.code = append(f.code, EncodeInstr(op, a, b, c, d))
	f.pos = append(f.pos, at)
}

func (f *fcomp) emitJump(op Op, a uint8, label int, at Pos) {
	// D は後でパッチする（暫定 0）。
	f.patches = append(f.patches, jumpPatch{at: len(f.code), label: label})
	f.emit(op, a, 0, 0, 0, at)
}

func (f *fcomp) patchAll() error {
	for _, p := range f.patches {
		target, ok := f.labels[p.label]
		if !ok {
			return fmt.Errorf("内部エラー：未解決ラベル %d", p.label)
		}
		off := target - p.at - 1
		op, a, b, c, _ := f.code[p.at].Decode()
		f.code[p.at] = EncodeInstr(op, a, b, c, EncodeSBx(off))
	}
	return nil
}

// --- 定数・名前プール ---

func (f *fcomp) constIdx(v Value) (uint32, error) {
	key := constKey(v)
	if i, ok := f.g.constMap[key]; ok {
		return uint32(i), nil
	}
	if len(f.g.consts) >= 1<<32-1 {
		return 0, fmt.Errorf("内部エラー：定数プールが大きすぎます")
	}
	i := len(f.g.consts)
	f.g.consts = append(f.g.consts, v)
	f.g.constMap[key] = i
	return uint32(i), nil
}

func (f *fcomp) nameIdx(name string) (uint32, error) {
	if i, ok := f.g.nameMap[name]; ok {
		return uint32(i), nil
	}
	if len(f.g.names) >= 1<<32-1 {
		return 0, fmt.Errorf("内部エラー：名前表が大きすぎます")
	}
	i := len(f.g.names)
	f.g.names = append(f.g.names, name)
	f.g.nameMap[name] = i
	return uint32(i), nil
}

func (f *fcomp) strIdx(s string) (uint32, error) {
	if i, ok := f.g.strMap[s]; ok {
		return uint32(i), nil
	}
	i := len(f.g.strs)
	f.g.strs = append(f.g.strs, s)
	f.g.strMap[s] = i
	return uint32(i), nil
}

// --- レジスタ割付 ---

func (f *fcomp) base() int {
	if f.isMain {
		return 0
	}
	return f.numSlots
}

func (f *fcomp) alloc() (uint8, error) {
	r := f.tempTop
	f.tempTop++
	if f.tempTop > f.maxRegs {
		f.maxRegs = f.tempTop
	}
	if r > 0xFE {
		return 0, fmt.Errorf("内部エラー：レジスタが足りません")
	}
	return uint8(r), nil
}

func (f *fcomp) mark() int { return f.tempTop }

func (f *fcomp) reset(m int) { f.tempTop = m }

// --- 仕上げ ---

func (f *fcomp) finish(name string, params []string) (*VMProto, error) {
	if err := f.patchAll(); err != nil {
		return nil, err
	}
	numRegs := f.maxRegs
	if f.isMain && numRegs == 0 {
		numRegs = 1
	}
	if numRegs > 0xFF {
		return nil, fmt.Errorf("内部エラー：レジスタが足りません")
	}
	return &VMProto{
		Name:      name,
		Params:    append([]string(nil), params...),
		NumParams: len(params),
		NumSlots:  f.numSlots,
		NumRegs:   numRegs,
		MaxArgs:   f.maxArgs,
		Code:      append([]Instr(nil), f.code...),
		Consts:    append([]Value(nil), f.g.consts...),
		Names:     append([]string(nil), f.g.names...),
		Strs:      append([]string(nil), f.g.strs...),
		Positions: append([]Pos(nil), f.pos...),
	}, nil
}

// labelSeq はラベル発行カウンタ。
func (f *fcomp) nextLabel() int { return f.newLabel() }

// --- 文 ---

func (f *fcomp) block(stmts []Stmt) error {
	for _, s := range stmts {
		if err := f.stmt(s); err != nil {
			return err
		}
	}
	return nil
}

func (f *fcomp) stmt(s Stmt) error {
	m := f.mark()
	defer f.reset(m)
	switch n := s.(type) {
	case *ExprStmt:
		_, err := f.expr(n.X)
		return err // void も破棄（検査なし）
	case *AssignStmt:
		rv, err := f.expr(n.Value)
		if err != nil {
			return err
		}
		// requireValue(v, target.Pos())：RHS の後・ターゲット解決の前。
		f.emit(OpCkVal, rv, 0, 0, 0, n.Target.Pos())
		return f.assign(n.Target, rv)
	case *CompoundAssignStmt:
		rv, err := f.expr(n.Value)
		if err != nil {
			return err
		}
		// requireValue(v, n.Value.Pos())。
		f.emit(OpCkVal, rv, 0, 0, 0, n.Value.Pos())
		return f.assignCompound(n.Target, n.Op, rv, n.At)
	case *DefStmt:
		return f.def(n)
	case *IfStmt:
		return f.ifStmt(n)
	case *RepeatStmt:
		return f.repeat(n)
	case *BreakStmt:
		return f.brk(n.At)
	case *ContinueStmt:
		return f.cont(n.At)
	case *SwitchStmt:
		return f.switchStmt(n)
	case *WhileStmt:
		return f.whileStmt(n)
	case *ReturnStmt:
		return f.returnStmt(n)
	case *BlockStmt:
		return f.block(n.Stmts)
	}
	return fmt.Errorf("内部エラー：不明な文です")
}

func (f *fcomp) def(n *DefStmt) error {
	// 本体は今コンパイルするが、登録（重複・組込検査つき）は実行時。
	sub := &fcomp{g: f.g, isMain: false, slots: map[string]int{}}
	for i, p := range n.Params {
		sub.slots[p.Name] = i
	}
	sub.numSlots = len(n.Params)
	collectSlots(n.Body, sub.slots)
	sub.numSlots = len(sub.slots)
	sub.tempTop = sub.numSlots
	sub.maxRegs = sub.numSlots
	if err := sub.block(n.Body.Stmts); err != nil {
		return err
	}
	// 本体 fall-through は void 返却。
	sub.emit(OpRet, 0, 0, 0, 0, n.At)
	names := make([]string, len(n.Params))
	ppos := make([]Pos, len(n.Params))
	for i, p := range n.Params {
		names[i] = p.Name
		ppos[i] = p.At
	}
	proto, err := sub.finish(n.Name, names)
	if err != nil {
		return err
	}
	proto.ParamPos = ppos
	proto.Slots = sub.slots
	idx := len(f.g.protos)
	f.g.protos = append(f.g.protos, proto)
	f.emit(OpFuncDef, 0, 0, 0, uint32(idx), n.At)
	return nil
}

// collectSlots は関数内で束縛されうる名前をすべて集める。
// （読出・代入・callee 名・repeat 変数。漏れは LOADN の未定義誤爆になるため
// 過剰に集める。余分なスロットは無害。）
func collectSlots(b *BlockStmt, slots map[string]int) {
	add := func(name string) {
		if _, ok := slots[name]; !ok {
			slots[name] = len(slots)
		}
	}
	var walkE func(x Expr)
	var walkS func(s Stmt)
	walkE = func(x Expr) {
		switch n := x.(type) {
		case *VarExpr:
			add(n.Name)
		case *ArrayLit:
			for _, e := range n.Elems {
				walkE(e)
			}
		case *IndexExpr:
			walkE(n.Base)
			walkE(n.Index)
		case *CallExpr:
			walkE(n.Callee)
			for _, a := range n.Args {
				walkE(a)
			}
		case *UnaryExpr:
			walkE(n.X)
		case *BinaryExpr:
			walkE(n.L)
			walkE(n.R)
		}
	}
	walkS = func(s Stmt) {
		switch n := s.(type) {
		case *ExprStmt:
			walkE(n.X)
		case *AssignStmt:
			walkE(n.Target)
			walkE(n.Value)
		case *CompoundAssignStmt:
			walkE(n.Target)
			walkE(n.Value)
		case *IfStmt:
			walkE(n.Cond)
			walkS(n.Then)
			if n.Else != nil {
				walkS(n.Else)
			}
		case *RepeatStmt:
			walkE(n.Count)
			if n.HasVar {
				add(n.Var)
			}
			if n.HasItem {
				add(n.Item)
			}
			walkS(n.Body)
		case *WhileStmt:
			walkE(n.Cond)
			walkS(n.Body)
		case *SwitchStmt:
			walkE(n.Value)
			for _, c := range n.Cases {
				for _, v := range c.Values {
					walkE(v)
				}
				walkS(c.Body)
			}
		case *ReturnStmt:
			if n.Value != nil {
				walkE(n.Value)
			}
		case *BlockStmt:
			for _, q := range n.Stmts {
				walkS(q)
			}
		case *BreakStmt, *ContinueStmt, *DefStmt:
		}
	}
	walkS(b)
}

func (f *fcomp) ifStmt(n *IfStmt) error {
	cv, err := f.expr(n.Cond)
	if err != nil {
		return err
	}
	f.emit(OpTest, cv, 0, 0, 0, n.Cond.Pos())
	elseLabel := f.nextLabel()
	endLabel := f.nextLabel()
	f.emitJump(OpJmpF, cv, elseLabel, n.Cond.Pos())
	if err := f.block(n.Then.Stmts); err != nil {
		return err
	}
	f.emitJump(OpJmp, 0, endLabel, n.At)
	f.bindLabel(elseLabel)
	switch e := n.Else.(type) {
	case nil:
	case *BlockStmt:
		if err := f.block(e.Stmts); err != nil {
			return err
		}
	case *IfStmt:
		if err := f.ifStmt(e); err != nil {
			return err
		}
	default:
		return fmt.Errorf("内部エラー：不正な else 分岐です")
	}
	f.bindLabel(endLabel)
	return nil
}

func (f *fcomp) whileStmt(n *WhileStmt) error {
	topLabel := f.nextLabel()
	endLabel := f.nextLabel()
	f.ctxt = append(f.ctxt, blockCtx{isLoop: true, end: endLabel, cont: topLabel})
	defer func() { f.ctxt = f.ctxt[:len(f.ctxt)-1] }()
	f.bindLabel(topLabel)
	cv, err := f.expr(n.Cond)
	if err != nil {
		return err
	}
	f.emit(OpTest, cv, 0, 0, 0, n.Cond.Pos())
	f.emitJump(OpJmpF, cv, endLabel, n.Cond.Pos())
	if err := f.block(n.Body.Stmts); err != nil {
		return err
	}
	f.emitJump(OpJmp, 0, topLabel, n.At)
	f.bindLabel(endLabel)
	return nil
}

func (f *fcomp) brk(at Pos) error {
	for i := len(f.ctxt) - 1; i >= 0; i-- {
		c := f.ctxt[i]
		if c.isLoop {
			if c.isRepeat {
				// repeat 脱出：制御 pop（束縛復元）＋終端へ。
				f.emit(OpPopLoop, 0, 0, 0, 0, at)
			}
			// while 脱出：復元なしで終端へ。
			f.emitJump(OpJmp, 0, c.end, at)
			return nil
		}
		// switch 脱出：復元なしで終端へ。
		f.emitJump(OpJmp, 0, c.end, at)
		return nil
	}
	f.emit(OpBrkTop, 0, 0, 0, 0, at)
	return nil
}

func (f *fcomp) cont(at Pos) error {
	for i := len(f.ctxt) - 1; i >= 0; i-- {
		if f.ctxt[i].isLoop {
			// 継続：復元なしで継続点へ（repeat の制御は残す）。
			f.emitJump(OpJmp, 0, f.ctxt[i].cont, at)
			return nil
		}
	}
	f.emit(OpContTop, 0, 0, 0, 0, at)
	return nil
}

func (f *fcomp) returnStmt(n *ReturnStmt) error {
	if f.isMain {
		// トップレベル return：値の副作用だけ実行してエラー。
		if n.Value != nil {
			if _, err := f.expr(n.Value); err != nil {
				return err
			}
		}
		f.emit(OpRetTop, 0, 0, 0, 0, n.At)
		return nil
	}
	if n.Value == nil {
		f.emit(OpRet, 0, 0, 0, 0, n.At)
		return nil
	}
	rv, err := f.expr(n.Value)
	if err != nil {
		return err
	}
	f.emit(OpRet, rv, 0, 1, 0, n.At)
	return nil
}

// --- repeat ---

func (f *fcomp) repeat(n *RepeatStmt) error {
	cv, err := f.expr(n.Count)
	if err != nil {
		return err
	}
	arrayPath := f.nextLabel()
	afterAll := f.nextLabel()
	// REPDISP：配列なら arrayPath、整数なら継続、それ以外はエラー（Count 位置）。
	f.emitJump(OpRepDisp, cv, arrayPath, n.Count.Pos())
	if n.HasItem {
		// 整数形の as i,x は無条件エラー（n.At 位置）。
		f.emit(OpRepIntErr, 0, 0, 0, 0, n.At)
	}
	f.emit(OpForIPrep, cv, 0, 0, 0, n.Count.Pos())
	if n.HasVar {
		if err := f.saveVar(n.Var, SaveReadonly|SaveInitZero, n.At); err != nil {
			return err
		}
	}
	endLabel := f.nextLabel()
	topLabel := f.nextLabel()
	f.ctxt = append(f.ctxt, blockCtx{isLoop: true, isRepeat: true, end: afterAll, cont: topLabel})
	f.bindLabel(topLabel)
	kv, err := f.alloc()
	if err != nil {
		return err
	}
	f.emitJump(OpForILoop, kv, endLabel, n.At)
	if n.HasVar {
		if err := f.putVar(n.Var, kv, n.At); err != nil {
			return err
		}
	}
	if err := f.block(n.Body.Stmts); err != nil {
		f.ctxt = f.ctxt[:len(f.ctxt)-1]
		return err
	}
	f.ctxt = f.ctxt[:len(f.ctxt)-1]
	f.emitJump(OpJmp, 0, topLabel, n.At)
	f.bindLabel(endLabel)
	f.emit(OpPopLoop, 0, 0, 0, 0, n.At)
	f.emitJump(OpJmp, 0, afterAll, n.At)

	// 配列形。
	f.bindLabel(arrayPath)
	f.emit(OpForAPrep, cv, 0, 0, 0, n.Count.Pos())
	indexVar, elemVar := "", ""
	if n.HasItem {
		indexVar, elemVar = n.Var, n.Item
	} else if n.HasVar {
		elemVar = n.Var
	}
	if indexVar != "" {
		if err := f.saveVar(indexVar, SaveReadonly|SaveInitZero, n.At); err != nil {
			return err
		}
	}
	if elemVar != "" {
		if err := f.saveVar(elemVar, 0, n.At); err != nil {
			return err
		}
	}
	endLabel2 := f.nextLabel()
	topLabel2 := f.nextLabel()
	f.ctxt = append(f.ctxt, blockCtx{isLoop: true, isRepeat: true, end: afterAll, cont: topLabel2})
	f.bindLabel(topLabel2)
	iv, err := f.alloc()
	if err != nil {
		return err
	}
	ev, err := f.alloc()
	if err != nil {
		return err
	}
	f.emitForALoop(iv, ev, endLabel2, n.At)
	if indexVar != "" {
		if err := f.putVar(indexVar, iv, n.At); err != nil {
			f.ctxt = f.ctxt[:len(f.ctxt)-1]
			return err
		}
	}
	if elemVar != "" {
		if err := f.putVar(elemVar, ev, n.At); err != nil {
			f.ctxt = f.ctxt[:len(f.ctxt)-1]
			return err
		}
	}
	if err := f.block(n.Body.Stmts); err != nil {
		f.ctxt = f.ctxt[:len(f.ctxt)-1]
		return err
	}
	f.ctxt = f.ctxt[:len(f.ctxt)-1]
	f.emitJump(OpJmp, 0, topLabel2, n.At)
	f.bindLabel(endLabel2)
	f.emit(OpPopLoop, 0, 0, 0, 0, n.At)
	f.bindLabel(afterAll)
	return nil
}

func (f *fcomp) emitForALoop(iv, ev uint8, label int, at Pos) {
	f.patches = append(f.patches, jumpPatch{at: len(f.code), label: label})
	f.emit(OpForALoop, iv, ev, 0, 0, at)
}

// saveVar はループ変数の退避を発行する（SAVEVAR）。
func (f *fcomp) saveVar(name string, flags uint8, at Pos) error {
	if f.isMain {
		ni, err := f.nameIdx(name)
		if err != nil {
			return err
		}
		f.emit(OpSaveVar, NoReg, SaveIsGlobal|flags, 0, ni, at)
		return nil
	}
	slot, ok := f.slots[name]
	if !ok {
		return fmt.Errorf("内部エラー：スロット未割当 %q", name)
	}
	f.emit(OpSaveVar, uint8(slot), flags, 0, 0, at)
	return nil
}

// putVar はループ変数への無検査書込を発行する（PUTN/PUTG）。
func (f *fcomp) putVar(name string, src uint8, at Pos) error {
	if f.isMain {
		ni, err := f.nameIdx(name)
		if err != nil {
			return err
		}
		f.emit(OpPutG, src, 0, 0, ni, at)
		return nil
	}
	slot, ok := f.slots[name]
	if !ok {
		return fmt.Errorf("内部エラー：スロット未割当 %q", name)
	}
	f.emit(OpPutN, uint8(slot), src, 0, 0, at)
	return nil
}

// --- switch ---

func (f *fcomp) switchStmt(n *SwitchStmt) error {
	sv, err := f.expr(n.Value)
	if err != nil {
		return err
	}
	// requireValue(v, n.Value.Pos())。
	f.emit(OpCkVal, sv, 0, 0, 0, n.Value.Pos())
	endLabel := f.nextLabel()
	defLabel := -1
	hasDefault := false
	for _, c := range n.Cases {
		if c.Default {
			hasDefault = true
		}
	}
	type caseArm struct {
		label int
	}
	var arms []caseArm
	if hasDefault {
		defLabel = f.nextLabel()
	}
	// 第一パス：一致探索（case 値を逐次評価・短絡）。
	for _, c := range n.Cases {
		if c.Default {
			continue
		}
		armLabel := f.nextLabel()
		arms = append(arms, caseArm{label: armLabel})
		for _, vx := range c.Values {
			vv, err := f.expr(vx)
			if err != nil {
				return err
			}
			// requireValue(cv, vx.Pos())。
			f.emit(OpCkVal, vv, 0, 0, 0, vx.Pos())
			cmp, err := f.alloc()
			if err != nil {
				return err
			}
			f.emit(OpEq, cmp, sv, vv, 0, n.At)
			f.emitJump(OpJmpT, cmp, armLabel, n.At)
		}
	}
	if hasDefault {
		f.emitJump(OpJmp, 0, defLabel, n.At)
	} else {
		f.emitJump(OpJmp, 0, endLabel, n.At)
	}
	// 第二パス：本体（break は switch 終端へ、continue は外側ループへ）。
	f.ctxt = append(f.ctxt, blockCtx{isLoop: false, end: endLabel})
	defer func() { f.ctxt = f.ctxt[:len(f.ctxt)-1] }()
	ai := 0
	for _, c := range n.Cases {
		if c.Default {
			continue
		}
		f.bindLabel(arms[ai].label)
		ai++
		if err := f.block(c.Body.Stmts); err != nil {
			return err
		}
		f.emitJump(OpJmp, 0, endLabel, c.At)
	}
	if hasDefault {
		f.bindLabel(defLabel)
		for _, c := range n.Cases {
			if !c.Default {
				continue
			}
			if err := f.block(c.Body.Stmts); err != nil {
				return err
			}
			break
		}
	}
	f.bindLabel(endLabel)
	return nil
}

// --- 代入 ---

func (f *fcomp) storeVar(name string, src uint8, at Pos) error {
	ni, err := f.nameIdx(name)
	if err != nil {
		return err
	}
	if f.isMain {
		f.emit(OpStoreG, src, 0, 0, ni, at)
		return nil
	}
	slot, ok := f.slots[name]
	if !ok {
		return fmt.Errorf("内部エラー：スロット未割当 %q", name)
	}
	f.emit(OpStoreN, src, 0, uint8(slot), ni, at)
	return nil
}

// assign は `target = RHS済` を発行する（RHS の CKVAL は呼出側で発行済み）。
func (f *fcomp) assign(target Expr, rv uint8) error {
	switch t := target.(type) {
	case *VarExpr:
		return f.storeVar(t.Name, rv, t.At)
	case *IndexExpr:
		return f.assignIndex(t, rv)
	}
	return fmt.Errorf("内部エラー：不正な代入先です")
}

// assignIndex は interp.go の assignIndex と同一の評価順序・検査順序で発行する。
// 単層：idx → CKINT/CKNEG → base → SETI。
// 複層：外側 idx → CKINT/CKNEG → 内側 base（式として）→ 内側 idx →
// CKARR(base) → CKINT → GETI（範囲）→ CKARR（要素）→ SETI。
func (f *fcomp) assignIndex(t *IndexExpr, rv uint8) error {
	oi, err := f.expr(t.Index)
	if err != nil {
		return err
	}
	f.emit(OpCkInt, oi, 0, 0, 0, t.Index.Pos())
	f.emit(OpCkNeg, oi, 0, 0, 0, t.Index.Pos())
	if inner, ok := t.Base.(*IndexExpr); ok {
		bv, err := f.expr(inner.Base)
		if err != nil {
			return err
		}
		ii, err := f.expr(inner.Index)
		if err != nil {
			return err
		}
		f.emit(OpCkArr, bv, 0, 0, 0, inner.At)
		f.emit(OpCkInt, ii, 0, 0, 0, inner.Index.Pos())
		ev, err := f.alloc()
		if err != nil {
			return err
		}
		f.emit(OpGetI, ev, bv, ii, 0, inner.At)
		f.emit(OpCkArr, ev, 0, 0, 0, inner.At)
		f.emit(OpSetI, ev, oi, rv, 0, t.At)
		return nil
	}
	bv, err := f.expr(t.Base)
	if err != nil {
		return err
	}
	f.emit(OpSetI, bv, oi, rv, 0, t.At)
	return nil
}

func binOpFor(tok TokenType) (Op, bool) {
	switch tok {
	case TokPlus:
		return OpAdd, true
	case TokMinus:
		return OpSub, true
	case TokStar:
		return OpMul, true
	case TokSlash:
		return OpDiv, true
	case TokMod:
		return OpMod, true
	case TokBitAnd:
		return OpBitAnd, true
	case TokBitOr:
		return OpBitOr, true
	case TokBitXor:
		return OpBitXor, true
	case TokShl:
		return OpShl, true
	case TokShr:
		return OpShr, true
	}
	return 0, false
}

// assignCompound は `target op= RHS済` を発行する。ターゲットの番地評価は1回。
func (f *fcomp) assignCompound(target Expr, op TokenType, rv uint8, at Pos) error {
	bop, ok := binOpFor(op)
	if !ok {
		return fmt.Errorf("内部エラー：不正な複合代入です")
	}
	if t, ok := target.(*VarExpr); ok {
		lv, err := f.loadVar(t)
		if err != nil {
			return err
		}
		res, err := f.alloc()
		if err != nil {
			return err
		}
		f.emit(bop, res, lv, rv, 0, at)
		f.emit(OpCkVal, res, 0, 0, 0, at)
		return f.storeVar(t.Name, res, t.At)
	}
	t, ok := target.(*IndexExpr)
	if !ok {
		return fmt.Errorf("内部エラー：不正な複合代入です")
	}
	// 鎖を外→内に集める。
	var chain []*IndexExpr
	for node := t; ; {
		chain = append(chain, node)
		base, ok := node.Base.(*IndexExpr)
		if !ok {
			break
		}
		node = base
	}
	// 添字を外→内に評価（各 CKINT 付き）、根元 base を評価。
	idxs := make([]uint8, len(chain))
	for i, node := range chain {
		iv, err := f.expr(node.Index)
		if err != nil {
			return err
		}
		f.emit(OpCkInt, iv, 0, 0, 0, node.Index.Pos())
		idxs[i] = iv
	}
	cur, err := f.expr(chain[len(chain)-1].Base)
	if err != nil {
		return err
	}
	// 内→外へ辿る。
	for i := len(chain) - 1; i >= 0; i-- {
		node := chain[i]
		f.emit(OpCkArr, cur, 0, 0, 0, node.At)
		if i > 0 {
			f.emit(OpCkBnd, idxs[i], cur, 0, 0, node.At)
			nv, err := f.alloc()
			if err != nil {
				return err
			}
			f.emit(OpGetI, nv, cur, idxs[i], 0, node.At)
			cur = nv
			continue
		}
		f.emit(OpCkNeg, idxs[i], 0, 0, 0, node.Index.Pos())
		f.emit(OpCkBnd, idxs[i], cur, 0, 0, node.At)
		res, err := f.alloc()
		if err != nil {
			return err
		}
		// 現在値の読出（検査済みのため防御は発火しない）。
		f.emit(OpGetI, res, cur, idxs[i], 0, node.At)
		fin, err := f.alloc()
		if err != nil {
			return err
		}
		f.emit(bop, fin, res, rv, 0, at)
		f.emit(OpCkVal, fin, 0, 0, 0, at)
		f.emit(OpSetI, cur, idxs[i], fin, 0, node.At)
	}
	return nil
}

// loadVar は変数読出を発行する。
func (f *fcomp) loadVar(t *VarExpr) (uint8, error) {
	dst, err := f.alloc()
	if err != nil {
		return 0, err
	}
	ni, err := f.nameIdx(t.Name)
	if err != nil {
		return 0, err
	}
	if f.isMain {
		f.emit(OpLoadG, dst, 0, 0, ni, t.At)
		return dst, nil
	}
	slot, ok := f.slots[t.Name]
	if !ok {
		return 0, fmt.Errorf("内部エラー：スロット未割当 %q", t.Name)
	}
	f.emit(OpLoadN, dst, 0, uint8(slot), ni, t.At)
	return dst, nil
}

// --- 式 ---

// expr は式を評価する命令列を発行し、結果レジスタを返す。
func (f *fcomp) expr(x Expr) (uint8, error) {
	switch n := x.(type) {
	case *IntLit:
		return f.loadConst(Int(n.Value), n.At)
	case *FloatLit:
		return f.loadConst(Float(n.Value), n.At)
	case *StringLit:
		return f.loadConst(Str(n.Value), n.At)
	case *BoolLit:
		return f.loadConst(Bool(n.Value), n.At)
	case *NullLit:
		return f.loadConst(Null(), n.At)
	case *VarExpr:
		return f.loadVar(n)
	case *ArrayLit:
		return f.arrayLit(n)
	case *IndexExpr:
		return f.indexRead(n)
	case *CallExpr:
		return f.call(n)
	case *UnaryExpr:
		return f.unary(n)
	case *BinaryExpr:
		return f.binary(n)
	}
	return 0, fmt.Errorf("内部エラー：不明な式です")
}

func (f *fcomp) loadConst(v Value, at Pos) (uint8, error) {
	dst, err := f.alloc()
	if err != nil {
		return 0, err
	}
	ci, err := f.constIdx(v)
	if err != nil {
		return 0, err
	}
	f.emit(OpLoadK, dst, 0, 0, ci, at)
	return dst, nil
}

// arrayLit は要素を順に評価（各 CKVAL）して束ねる。
// 巨大リテラルでもレジスタを食い潰さないよう、空配列＋APPEND の逐次構築に
// lowering する（評価順序・検査位置は不変、要素毎に一時を解放）。
func (f *fcomp) arrayLit(n *ArrayLit) (uint8, error) {
	dst, err := f.alloc()
	if err != nil {
		return 0, err
	}
	f.emit(OpNewArr, dst, 0, 0, 0, n.At)
	for _, e := range n.Elems {
		m := f.mark()
		ev, err := f.expr(e)
		if err != nil {
			return 0, err
		}
		f.emit(OpCkVal, ev, 0, 0, 0, e.Pos())
		f.emit(OpAppend, dst, ev, 0, 0, n.At)
		f.reset(m)
	}
	return dst, nil
}

// indexRead は base → idx の順で評価し、CKINT/CKNEG を Index 位置で先行する。
func (f *fcomp) indexRead(n *IndexExpr) (uint8, error) {
	bv, err := f.expr(n.Base)
	if err != nil {
		return 0, err
	}
	iv, err := f.expr(n.Index)
	if err != nil {
		return 0, err
	}
	f.emit(OpCkInt, iv, 0, 0, 0, n.Index.Pos())
	f.emit(OpCkNeg, iv, 0, 0, 0, n.Index.Pos())
	dst, err := f.alloc()
	if err != nil {
		return 0, err
	}
	f.emit(OpGetI, dst, bv, iv, 0, n.At)
	return dst, nil
}

func (f *fcomp) unary(n *UnaryExpr) (uint8, error) {
	xv, err := f.expr(n.X)
	if err != nil {
		return 0, err
	}
	dst, err := f.alloc()
	if err != nil {
		return 0, err
	}
	switch n.Op {
	case TokMinus:
		f.emit(OpNeg, dst, xv, 0, 0, n.At)
	case TokBang:
		// requireBool(v, n.X.Pos())：NOT 自身の位置を X 位置にする。
		f.emit(OpNot, dst, xv, 0, 0, n.X.Pos())
	case TokBitNot:
		f.emit(OpBitNot, dst, xv, 0, 0, n.At)
	default:
		return 0, fmt.Errorf("内部エラー：不正な単項演算子です")
	}
	return dst, nil
}

func (f *fcomp) binary(n *BinaryExpr) (uint8, error) {
	if n.Op == TokAnd || n.Op == TokOr {
		return f.logical(n)
	}
	lv, err := f.expr(n.L)
	if err != nil {
		return 0, err
	}
	rv, err := f.expr(n.R)
	if err != nil {
		return 0, err
	}
	dst, err := f.alloc()
	if err != nil {
		return 0, err
	}
	switch n.Op {
	case TokEq:
		f.emit(OpCkVal, lv, 0, 0, 0, n.L.Pos())
		f.emit(OpCkVal, rv, 0, 0, 0, n.R.Pos())
		f.emit(OpEq, dst, lv, rv, 0, n.At)
	case TokNotEq:
		f.emit(OpCkVal, lv, 0, 0, 0, n.L.Pos())
		f.emit(OpCkVal, rv, 0, 0, 0, n.R.Pos())
		f.emit(OpNe, dst, lv, rv, 0, n.At)
	case TokLt:
		f.emit(OpLt, dst, lv, rv, 0, n.At)
	case TokLtEq:
		f.emit(OpLe, dst, lv, rv, 0, n.At)
	case TokGt:
		f.emit(OpGt, dst, lv, rv, 0, n.At)
	case TokGtEq:
		f.emit(OpGe, dst, lv, rv, 0, n.At)
	case TokPlus:
		f.emit(OpAdd, dst, lv, rv, 0, n.At)
	case TokMinus:
		f.emit(OpSub, dst, lv, rv, 0, n.At)
	case TokStar:
		f.emit(OpMul, dst, lv, rv, 0, n.At)
	case TokSlash:
		f.emit(OpDiv, dst, lv, rv, 0, n.At)
	case TokMod:
		f.emit(OpMod, dst, lv, rv, 0, n.At)
	case TokBitAnd:
		f.emit(OpBitAnd, dst, lv, rv, 0, n.At)
	case TokBitOr:
		f.emit(OpBitOr, dst, lv, rv, 0, n.At)
	case TokBitXor:
		f.emit(OpBitXor, dst, lv, rv, 0, n.At)
	case TokShl:
		f.emit(OpShl, dst, lv, rv, 0, n.At)
	case TokShr:
		f.emit(OpShr, dst, lv, rv, 0, n.At)
	default:
		return 0, fmt.Errorf("内部エラー：不正な二項演算子です")
	}
	return dst, nil
}

// logical は短絡評価（strict bool）を発行する。
func (f *fcomp) logical(n *BinaryExpr) (uint8, error) {
	dst, err := f.alloc()
	if err != nil {
		return 0, err
	}
	lv, err := f.expr(n.L)
	if err != nil {
		return 0, err
	}
	f.emit(OpTest, lv, 0, 0, 0, n.L.Pos())
	shortLabel := f.nextLabel()
	endLabel := f.nextLabel()
	fci, err := f.constIdx(Bool(n.Op == TokOr))
	if err != nil {
		return 0, err
	}
	if n.Op == TokAnd {
		f.emitJump(OpJmpF, lv, shortLabel, n.L.Pos())
	} else {
		f.emitJump(OpJmpT, lv, shortLabel, n.L.Pos())
	}
	rv, err := f.expr(n.R)
	if err != nil {
		return 0, err
	}
	f.emit(OpTest, rv, 0, 0, 0, n.R.Pos())
	f.emit(OpMove, dst, rv, 0, 0, n.At)
	f.emitJump(OpJmp, 0, endLabel, n.At)
	f.bindLabel(shortLabel)
	f.emit(OpLoadK, dst, 0, 0, fci, n.At)
	f.bindLabel(endLabel)
	return dst, nil
}

// call は呼出を発行する。引数は左→右に連続レジスタへ評価する。
// void 検査は mes/print/logmes 以外の組込・ユーザ関数で各引数位置に発行する。
func (f *fcomp) call(n *CallExpr) (uint8, error) {
	dst, err := f.alloc()
	if err != nil {
		return 0, err
	}
	if v, ok := n.Callee.(*VarExpr); ok {
		checkVoid := true
		if isBuiltin(v.Name) && (v.Name == "mes" || v.Name == "print" || v.Name == "logmes") {
			checkVoid = false
		}
		// CKCALL：解決順序（関数→変数→組込→未定義）と arity を
		// 引数評価より先に確定させる（interp.go の evalCall と同順）。
		ni, err := f.nameIdx(v.Name)
		if err != nil {
			return 0, err
		}
		if len(n.Args) > 0xFF {
			return 0, fmt.Errorf("内部エラー：引数が多すぎます")
		}
		f.emit(OpCkCall, 0, 0, uint8(len(n.Args)), ni, n.At)
		m := f.mark()
		argRegs := make([]uint8, len(n.Args))
		for i, a := range n.Args {
			av, err := f.expr(a)
			if err != nil {
				return 0, err
			}
			if checkVoid {
				f.emit(OpCkVal, av, 0, 0, 0, a.Pos())
			}
			argRegs[i] = av
		}
		_ = m
		base, err := f.packArgs(argRegs, n.At)
		if err != nil {
			return 0, err
		}
		if isBuiltin(v.Name) {
			id := lookupBuiltinID(v.Name)
			if id < 0 {
				return 0, fmt.Errorf("内部エラー：組込 %q の解決に失敗しました", v.Name)
			}
			if len(n.Args) > f.maxArgs {
				f.maxArgs = len(n.Args)
			}
			f.emit(OpCallB, dst, base, uint8(len(n.Args)), uint32(id), n.At)
			return dst, nil
		}
		f.emit(OpCallF, dst, base, uint8(len(n.Args)), ni, n.At)
		return dst, nil
	}
	cv, err := f.expr(n.Callee)
	if err != nil {
		return 0, err
	}
	si, err := f.strIdx(n.Callee.String())
	if err != nil {
		return 0, err
	}
	f.emit(OpCallV, cv, 0, 0, si, n.At)
	// 到達不能（CALLV は常にエラー）。結果レジスタは dst を返す。
	_ = dst
	return cv, nil
}

// packArgs は引数レジスタの連続性を保証し、基底を返す。
func (f *fcomp) packArgs(regs []uint8, at Pos) (uint8, error) {
	if len(regs) == 0 {
		return uint8(f.tempTop), nil
	}
	base := regs[0]
	ok := true
	for i, r := range regs {
		if r != base+uint8(i) {
			ok = false
			break
		}
	}
	if !ok {
		nb, err := f.alloc()
		if err != nil {
			return 0, err
		}
		for i := 1; i < len(regs); i++ {
			if _, err := f.alloc(); err != nil {
				return 0, err
			}
		}
		_ = nb
		base = nb
		for i, r := range regs {
			f.emit(OpMove, base+uint8(i), r, 0, 0, at)
		}
	}
	return base, nil
}
