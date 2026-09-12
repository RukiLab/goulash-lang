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
	// abs(MinInt64) is an error: the negation wraps and has no
	// representable absolute value.
	register("abs", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		switch args[0].K {
		case KInt:
			if args[0].I < 0 {
				if args[0].I == math.MinInt64 {
					return Null(), argErr("abs", 0, at, "%d の絶対値を取得できません", args[0].I)
				}
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

	// limit(v, lo, hi): clamp into range. All-int inputs clamp in
	// int64 and yield int; otherwise float64.
	register("limit", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		intPath := true
		for _, a := range args {
			if a.K != KInt {
				intPath = false
				break
			}
		}
		if intPath {
			lo, hi, c := args[1].I, args[2].I, args[0].I
			if lo > hi {
				return Null(), rtErrf(at, "limit：下限 %d が上限 %d を超えています", lo, hi)
			}
			if c < lo {
				c = lo
			}
			if c > hi {
				c = hi
			}
			return Int(c), nil
		}
		nums := make([]float64, 3)
		for i := range args {
			f, err := needFloat("limit", args, i, at)
			if err != nil {
				return Null(), err
			}
			nums[i] = f
		}
		lo, hi, c := nums[1], nums[2], nums[0]
		if lo > hi {
			return Null(), rtErrf(at, "limit：下限 %g が上限 %g を超えています", lo, hi)
		}
		if c < lo {
			c = lo
		}
		if c > hi {
			c = hi
		}
		return Float(c), nil
	})

	// min(a, b, ...) / max(a, b, ...): least/greatest argument.
	// All-int inputs yield int; otherwise float64.
	for name, wantMin := range map[string]bool{"min": true, "max": false} {
		register(name, 2, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
			intPath := true
			for _, a := range args {
				if a.K != KInt {
					intPath = false
					break
				}
			}
			if intPath {
				best := args[0].I
				for _, a := range args[1:] {
					if wantMin {
						if a.I < best {
							best = a.I
						}
					} else if a.I > best {
						best = a.I
					}
				}
				return Int(best), nil
			}
			nums := make([]float64, len(args))
			for i := range args {
				f, err := needFloat(name, args, i, at)
				if err != nil {
					return Null(), err
				}
				nums[i] = f
			}
			best := nums[0]
			for _, f := range nums[1:] {
				if wantMin {
					if f < best {
						best = f
					}
				} else if f > best {
					best = f
				}
			}
			return Float(best), nil
		})
	}

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
