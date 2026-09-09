// P1 builtins: conversions, math, random.
package main

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

func init() {
	// int(v): float truncates toward zero; numeric strings parse;
	// bool maps to 1/0. Anything else is an error.
	register("int", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		switch args[0].K {
		case KInt:
			return args[0], nil
		case KFloat:
			return Int(int64(args[0].F)), nil
		case KBool:
			if args[0].B {
				return Int(1), nil
			}
			return Int(0), nil
		case KString:
			s := strings.TrimSpace(args[0].S)
			if v, err := strconv.ParseInt(s, 10, 64); err == nil {
				return Int(v), nil
			}
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return Int(int64(f)), nil
			}
			return Null(), argErr("int", 0, at, "%q を整数に変換できません", args[0].S)
		}
		return Null(), argErr("int", 0, at, "%s を整数に変換できません", typeNameOf(args[0]))
	})

	// float(v): int widens; numeric strings parse; bool maps to 1.0/0.0.
	register("float", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		switch args[0].K {
		case KFloat:
			return args[0], nil
		case KInt:
			return Float(float64(args[0].I)), nil
		case KBool:
			if args[0].B {
				return Float(1), nil
			}
			return Float(0), nil
		case KString:
			s := strings.TrimSpace(args[0].S)
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return Float(f), nil
			}
			return Null(), argErr("float", 0, at, "%q を浮動小数に変換できません", args[0].S)
		}
		return Null(), argErr("float", 0, at, "%s を浮動小数に変換できません", typeNameOf(args[0]))
	})

	// str(v): renders any value the way mes() prints it.
	register("str", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Str(Stringify(args[0])), nil
	})

	// vartype(v): type name ("int", "float", "string", "bool", "array",
	// "map", "function", "null").
	register("vartype", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		return Str(typeNameOf(args[0])), nil
	})

	// abs(v): absolute value, preserving int/float.
	register("abs", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		switch args[0].K {
		case KInt:
			if args[0].I < 0 {
				return Int(-args[0].I), nil
			}
			return args[0], nil
		case KFloat:
			return Float(math.Abs(args[0].F)), nil
		}
		return Null(), argErr("abs", 0, at, "数値である必要があります。%s が指定されました", typeNameOf(args[0]))
	})

	// sqrt(v): square root as float; negatives are an error.
	register("sqrt", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		f, err := needFloat("sqrt", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if f < 0 {
			return Null(), argErr("sqrt", 0, at, "%g の平方根を取得できません", f)
		}
		return Float(math.Sqrt(f)), nil
	})

	// Trig (radians, as in HSP).
	for name, fn := range map[string]func(float64) float64{
		"sin": math.Sin, "cos": math.Cos, "tan": math.Tan,
	} {
		register(name, 1, 1, trigFn(name, fn))
	}

	// atan(y) or atan(y, x): arctangent, or atan2 with two arguments.
	register("atan", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		y, err := needFloat("atan", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if len(args) == 2 {
			x, err := needFloat("atan", args, 1, at)
			if err != nil {
				return Null(), err
			}
			return Float(math.Atan2(y, x)), nil
		}
		return Float(math.Atan(y)), nil
	})

	// exp(v): e^v. log(v): natural logarithm (v must be positive).
	register("exp", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		f, err := needFloat("exp", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Float(math.Exp(f)), nil
	})
	register("log", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		f, err := needFloat("log", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if f <= 0 {
			return Null(), argErr("log", 0, at, "対数は正の数が必要です。%g が指定されました", f)
		}
		return Float(math.Log(f)), nil
	})

	// pow(base, exp): power as float.
	register("pow", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		b, err := needFloat("pow", args, 0, at)
		if err != nil {
			return Null(), err
		}
		e, err := needFloat("pow", args, 1, at)
		if err != nil {
			return Null(), err
		}
		return Float(math.Pow(b, e)), nil
	})

	// limit(v, lo, hi): clamp into range. All-int inputs yield int.
	register("limit", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		v, err := needFloat("limit", args, 0, at)
		if err != nil {
			return Null(), err
		}
		lo, err := needFloat("limit", args, 1, at)
		if err != nil {
			return Null(), err
		}
		hi, err := needFloat("limit", args, 2, at)
		if err != nil {
			return Null(), err
		}
		if lo > hi {
			return Null(), rtErrf(at, "limit：下限 %g が上限 %g を超えています", lo, hi)
		}
		c := v
		if c < lo {
			c = lo
		}
		if c > hi {
			c = hi
		}
		if args[0].K == KInt && args[1].K == KInt && args[2].K == KInt {
			return Int(int64(c)), nil
		}
		return Float(c), nil
	})

	// length(a): number of top-level elements. Nested levels via length(a[i]).
	register("length", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, err := needArray("length", args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Int(int64(len(arr.Elems))), nil
	})

	// rnd(n): integer in 0..n-1.
	register("rnd", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		n, err := needInt("rnd", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if n <= 0 {
			return Null(), argErr("rnd", 0, at, "正の上限が必要です。%d が指定されました", n)
		}
		return Int(int64(scriptRand.IntN(int(n)))), nil
	})

	// randomize([seed]): without args seeds from the clock; with an
	// integer seed the sequence becomes reproducible.
	register("randomize", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		if len(args) == 0 {
			scriptRand = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
			return Null(), nil
		}
		seed, err := needInt("randomize", args, 0, at)
		if err != nil {
			return Null(), err
		}
		scriptRand = rand.New(rand.NewPCG(uint64(seed), 0))
		return Null(), nil
	})
}

func trigFn(name string, fn func(float64) float64) builtinFn {
	return func(in *Interp, args []Value, at Pos) (Value, error) {
		f, err := needFloat(name, args, 0, at)
		if err != nil {
			return Null(), err
		}
		return Float(fn(f)), nil
	}
}

// scriptRand is the script-visible random source (reseeded by randomize).
var scriptRand = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
