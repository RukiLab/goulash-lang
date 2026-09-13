// Goulash v0.2 バイトコード逆アセンブラ。
//
// `gsh disasm <file.gsh>` が定数プール・レジスタ数・ソース位置付きで出力する。
// GOULASH_TRACE=1 の実行時トレースも同一フォーマッタを使う。
package main

import (
	"fmt"
	"strings"
)

// fmtInstr は1命令を人間可読形式で描画する（トレース用・位置なし）。
// 実行時トレースでは G系の D が addProg により gidx へ書換え済みのため、
// グローバル名で解決し直す。
func (m *vmachine) fmtInstr(proto *VMProto, pc int) string {
	ins := proto.Code[pc]
	op, a, b, c, d := ins.Decode()
	r := func(x uint8) string { return fmt.Sprintf("R%d", x) }
	gname := func(gidx uint32) string {
		if int(gidx) < len(m.globals.names) {
			return m.globals.names[gidx]
		}
		return fmt.Sprintf("G#%d", gidx)
	}
	switch op {
	case OpLoadG:
		return fmt.Sprintf("LOADG %s, G[%s]", r(a), gname(d))
	case OpStoreG:
		return fmt.Sprintf("STOREG %s, G[%s]", r(a), gname(d))
	case OpPutG:
		return fmt.Sprintf("PUTG %s, G[%s]", r(a), gname(d))
	case OpSaveVar:
		if c&SaveIsGlobal != 0 {
			return fmt.Sprintf("SAVEVAR S%d, flags=%d, G[%s]", b, c, gname(d))
		}
	}
	return formatInstr(proto, pc)
}

func formatInstr(proto *VMProto, pc int) string {
	return formatInstrProg(nil, proto, pc)
}

func formatInstrProg(prog *VMProgram, proto *VMProto, pc int) string {
	ins := proto.Code[pc]
	op, a, b, c, d := ins.Decode()
	r := func(x uint8) string { return fmt.Sprintf("R%d", x) }
	switch op {
	case OpMove:
		return fmt.Sprintf("MOVE %s, %s", r(a), r(b))
	case OpLoadK:
		return fmt.Sprintf("LOADK %s, K%d (%s)", r(a), d, valueRepr(proto.Consts[d]))
	case OpLoadG:
		return fmt.Sprintf("LOADG %s, G[%s]", r(a), proto.Names[d])
	case OpStoreG:
		return fmt.Sprintf("STOREG %s, G[%s]", r(a), proto.Names[d])
	case OpLoadN:
		return fmt.Sprintf("LOADN %s, N[%s], S%d", r(a), proto.Names[d], c)
	case OpStoreN:
		return fmt.Sprintf("STOREN %s, N[%s], S%d", r(a), proto.Names[d], c)
	case OpCkVal:
		return fmt.Sprintf("CKVAL %s", r(a))
	case OpTest:
		return fmt.Sprintf("TEST %s", r(a))
	case OpJmp:
		return fmt.Sprintf("JMP %d", pc+1+int(ins.SBx()))
	case OpJmpT:
		return fmt.Sprintf("JMPT %s, %d", r(a), pc+1+int(ins.SBx()))
	case OpJmpF:
		return fmt.Sprintf("JMPF %s, %d", r(a), pc+1+int(ins.SBx()))
	case OpCkInt:
		return fmt.Sprintf("CKINT %s", r(a))
	case OpCkNeg:
		return fmt.Sprintf("CKNEG %s", r(a))
	case OpCkArr:
		return fmt.Sprintf("CKARR %s", r(a))
	case OpCkBnd:
		return fmt.Sprintf("CKBND %s, %s", r(a), r(b))
	case OpGetI:
		return fmt.Sprintf("GETI %s, %s, %s", r(a), r(b), r(c))
	case OpSetI:
		return fmt.Sprintf("SETI %s, %s, %s", r(a), r(b), r(c))
	case OpNewArr:
		return fmt.Sprintf("NEWARR %s, %s..%s", r(a), r(b), r(uint8(int(b)+int(d)-1)))
	case OpAppend:
		return fmt.Sprintf("APPEND %s, %s", r(a), r(b))
	case OpAdd, OpSub, OpMul, OpDiv, OpMod,
		OpBitAnd, OpBitOr, OpBitXor, OpShl, OpShr,
		OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
		return fmt.Sprintf("%s %s, %s, %s", op, r(a), r(b), r(c))
	case OpNot, OpNeg, OpBitNot:
		return fmt.Sprintf("%s %s, %s", op, r(a), r(b))
	case OpRepDisp:
		return fmt.Sprintf("REPDISP %s, %d", r(a), pc+1+int(ins.SBx()))
	case OpRepIntErr:
		return "REPITMERR"
	case OpForIPrep:
		return fmt.Sprintf("FORIPREP %s", r(a))
	case OpForAPrep:
		return fmt.Sprintf("FORAPREP %s", r(a))
	case OpSaveVar:
		return fmt.Sprintf("SAVEVAR S%d, flags=%d, %s", b, c, saveVarName(proto, b, c, d))
	case OpForILoop:
		return fmt.Sprintf("FORILOOP %s, %d", r(a), pc+1+int(ins.SBx()))
	case OpForALoop:
		return fmt.Sprintf("FORALOOP %s, %s, %d", regOrNone(a), regOrNone(b), pc+1+int(ins.SBx()))
	case OpPutN:
		return fmt.Sprintf("PUTN S%d, %s", a, r(b))
	case OpPutG:
		return fmt.Sprintf("PUTG %s, G[%s]", r(a), proto.Names[d])
	case OpPopLoop:
		return "POPLOOP"
	case OpFuncDef:
		return fmt.Sprintf("FUNCDEF P%d (%s)", d, mprogProtoName(prog, int(d)))
	case OpCkCall:
		return fmt.Sprintf("CKCALL argc=%d, %s", c, proto.Names[d])
	case OpCallF:
		return fmt.Sprintf("CALLF %s, args=%s+%d, %s", r(a), r(b), c, proto.Names[d])
	case OpCallB:
		return fmt.Sprintf("CALLB %s, args=%s+%d, %s", r(a), r(b), c, builtinNameByID(int(d)))
	case OpCallV:
		return fmt.Sprintf("CALLV %s, %q", r(a), proto.Strs[d])
	case OpRet:
		if c == 1 {
			return fmt.Sprintf("RET %s", r(a))
		}
		return "RET void"
	case OpBrkTop:
		return "BRKTOP"
	case OpContTop:
		return "CONTTOP"
	case OpRetTop:
		return "RETTOP"
	case OpHalt:
		return "HALT"
	}
	return fmt.Sprintf("OP(%d) %d %d %d %d", uint8(op), a, b, c, d)
}

func regOrNone(x uint8) string {
	if x == NoReg {
		return "-"
	}
	return fmt.Sprintf("R%d", x)
}

func saveVarName(proto *VMProto, b, c uint8, d uint32) string {
	if c&SaveIsGlobal != 0 {
		return "G[" + proto.Names[d] + "]"
	}
	return fmt.Sprintf("S%d", b)
}

func valueRepr(v Value) string {
	switch v.K {
	case KNull:
		return "void"
	case KString:
		return fmt.Sprintf("%q", v.S)
	default:
		return Stringify(v)
	}
}

func builtinNameByID(id int) string {
	ensureBuiltinTable()
	if id >= 0 && id < len(builtinTable) {
		return builtinTable[id].name
	}
	return fmt.Sprintf("builtin#%d", id)
}

func mprogProtoName(prog *VMProgram, idx int) string {
	if prog != nil && idx >= 0 && idx < len(prog.Protos) {
		return prog.Protos[idx].Name
	}
	return fmt.Sprintf("P%d", idx)
}

// Disassemble はプログラム全体をテキスト化する。
func Disassemble(prog *VMProgram) string {
	var sb strings.Builder
	dump := func(p *VMProto) {
		fmt.Fprintf(&sb, "== %s (params=%d slots=%d regs=%d maxargs=%d) ==\n",
			p.Name, p.NumParams, p.NumSlots, p.NumRegs, p.MaxArgs)
		if len(p.Consts) > 0 {
			sb.WriteString("-- consts --\n")
			for i, kv := range p.Consts {
				fmt.Fprintf(&sb, "  K%d = %s\n", i, valueRepr(kv))
			}
		}
		if len(p.Names) > 0 {
			sb.WriteString("-- names --\n")
			for i, n := range p.Names {
				fmt.Fprintf(&sb, "  N%d = %s\n", i, n)
			}
		}
		for pc := range p.Code {
			fmt.Fprintf(&sb, "  %4d  %-28s ; %s\n", pc, formatInstrProg(prog, p, pc), p.Positions[pc])
		}
	}
	dump(prog.Main)
	for _, p := range prog.Protos {
		dump(p)
	}
	return sb.String()
}
