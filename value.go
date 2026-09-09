// Runtime value model for HSP successor language v0.1.
package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Kind classifies a Value.
type Kind int

const (
	KNull Kind = iota
	KInt
	KFloat
	KString
	KBool
	KArray
	KMap
	KFunc
)

func (k Kind) String() string {
	switch k {
	case KNull:
		return "null"
	case KInt:
		return "int"
	case KFloat:
		return "float"
	case KString:
		return "string"
	case KBool:
		return "bool"
	case KArray:
		return "array"
	case KMap:
		return "map"
	case KFunc:
		return "function"
	}
	return "unknown"
}

// Value is a dynamically typed v0.1 value. Arrays and maps hold shared
// pointers so `b = a` shares elements (reference semantics).
type Value struct {
	K   Kind
	I   int64
	F   float64
	S   string
	B   bool
	Arr *Array
	Mp  *MapVal
	Fn  *FuncVal
}

// Array is a heterogeneous resizable sequence.
type Array struct {
	Elems []Value
}

// MapVal is a string-keyed map with insertion order (for stable printing).
type MapVal struct {
	Fields map[string]Value
	Order  []string
}

// FuncVal is a user function with its defining environment (closure).
type FuncVal struct {
	Name    string
	Params  []Param
	Body    *BlockStmt
	Closure *Env
}

func Null() Value             { return Value{K: KNull} }
func Int(v int64) Value       { return Value{K: KInt, I: v} }
func Float(v float64) Value   { return Value{K: KFloat, F: v} }
func Str(v string) Value      { return Value{K: KString, S: v} }
func Bool(v bool) Value       { return Value{K: KBool, B: v} }
func ArrayOf(e []Value) Value { return Value{K: KArray, Arr: &Array{Elems: e}} }
func MapOf() Value {
	return Value{K: KMap, Mp: &MapVal{Fields: map[string]Value{}}}
}
func FuncOf(f *FuncVal) Value { return Value{K: KFunc, Fn: f} }

// SetMap stores a key, recording insertion order for newcomers.
func SetMap(m *MapVal, key string, v Value) {
	if _, ok := m.Fields[key]; !ok {
		m.Order = append(m.Order, key)
	}
	m.Fields[key] = v
}

// Stringify renders a value for mes() output and error messages.
// Strings print raw; floats use the shortest round-trip form.
func Stringify(v Value) string {
	switch v.K {
	case KNull:
		return "null"
	case KInt:
		return strconv.FormatInt(v.I, 10)
	case KFloat:
		return strconv.FormatFloat(v.F, 'g', -1, 64)
	case KString:
		return v.S
	case KBool:
		if v.B {
			return "true"
		}
		return "false"
	case KArray:
		parts := make([]string, len(v.Arr.Elems))
		for i, e := range v.Arr.Elems {
			parts[i] = Stringify(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case KMap:
		parts := make([]string, len(v.Mp.Order))
		for i, name := range v.Mp.Order {
			parts[i] = fmt.Sprintf("%q: %s", name, Stringify(v.Mp.Fields[name]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case KFunc:
		return fmt.Sprintf("<function %s>", v.Fn.Name)
	}
	return "unknown"
}

// typeNameOf reports the v0.1 type name of a value for diagnostics.
func typeNameOf(v Value) string {
	return v.K.String()
}

// valuesEqual implements ==/!= (deep for arrays and maps).
func valuesEqual(a, b Value) bool {
	if a.K == KInt && b.K == KFloat {
		return float64(a.I) == b.F
	}
	if a.K == KFloat && b.K == KInt {
		return a.F == float64(b.I)
	}
	if a.K != b.K {
		return false
	}
	switch a.K {
	case KNull:
		return true
	case KInt:
		return a.I == b.I
	case KFloat:
		return a.F == b.F
	case KString:
		return a.S == b.S
	case KBool:
		return a.B == b.B
	case KArray:
		if len(a.Arr.Elems) != len(b.Arr.Elems) {
			return false
		}
		for i := range a.Arr.Elems {
			if !valuesEqual(a.Arr.Elems[i], b.Arr.Elems[i]) {
				return false
			}
		}
		return true
	case KMap:
		if len(a.Mp.Fields) != len(b.Mp.Fields) {
			return false
		}
		for _, name := range a.Mp.Order {
			bv, ok := b.Mp.Fields[name]
			if !ok {
				return false
			}
			if !valuesEqual(a.Mp.Fields[name], bv) {
				return false
			}
		}
		return true
	case KFunc:
		return a.Fn == b.Fn
	}
	return false
}
