// P6 builtins: peek/poke on int arrays as byte buffers (little-endian).
package main

func init() {
	// peek(arr, i): one byte (0-255).
	register("peek", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, err := peekArgs("peek", args, at, 1)
		if err != nil {
			return Null(), err
		}
		return Int(arr.Elems[i].I), nil
	})

	// wpeek(arr, i): two bytes, little-endian (0-65535).
	register("wpeek", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, err := peekArgs("wpeek", args, at, 2)
		if err != nil {
			return Null(), err
		}
		return Int(arr.Elems[i].I | arr.Elems[i+1].I<<8), nil
	})

	// lpeek(arr, i): four bytes, little-endian.
	register("lpeek", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, err := peekArgs("lpeek", args, at, 4)
		if err != nil {
			return Null(), err
		}
		v := arr.Elems[i].I | arr.Elems[i+1].I<<8 |
			arr.Elems[i+2].I<<16 | arr.Elems[i+3].I<<24
		return Int(v), nil
	})

	// poke(arr, i, v): write one byte (0-255).
	register("poke", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, v, err := pokeArgs("poke", args, at, 1)
		if err != nil {
			return Null(), err
		}
		arr.Elems[i] = Int(v & 0xFF)
		return Null(), nil
	})

	// wpoke(arr, i, v): write two bytes, little-endian (0-65535).
	register("wpoke", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, v, err := pokeArgs("wpoke", args, at, 2)
		if err != nil {
			return Null(), err
		}
		arr.Elems[i] = Int(v & 0xFF)
		arr.Elems[i+1] = Int((v >> 8) & 0xFF)
		return Null(), nil
	})

	// lpoke(arr, i, v): write four bytes, little-endian (0..2^32-1).
	register("lpoke", 3, 3, func(in *Interp, args []Value, at Pos) (Value, error) {
		arr, i, v, err := pokeArgs("lpoke", args, at, 4)
		if err != nil {
			return Null(), err
		}
		for k := 0; k < 4; k++ {
			arr.Elems[i+k] = Int((v >> (8 * k)) & 0xFF)
		}
		return Null(), nil
	})
}

// peekArgs validates (array, index) and bounds for n bytes.
// Elements must already be bytes; out-of-range reads are errors.
func peekArgs(name string, args []Value, at Pos, n int) (*Array, int, error) {
	arr, err := needArray(name, args, 0, at)
	if err != nil {
		return nil, 0, err
	}
	i, err := needInt(name, args, 1, at)
	if err != nil {
		return nil, 0, err
	}
	if i < 0 || int(i)+n > len(arr.Elems) {
		return nil, 0, argErr(name, 1, at, "バイト %d から %d は範囲外です（長さ %d）", i, i+int64(n)-1, len(arr.Elems))
	}
	for k := 0; k < n; k++ {
		e := arr.Elems[int(i)+k]
		if e.K != KInt || e.I < 0 || e.I > 255 {
			return nil, 0, rtErrf(at, "%s：要素 %d はバイト（0 から 255）ではありません", name, int(i)+k)
		}
	}
	return arr, int(i), nil
}

// pokeArgs validates (array, index, value) for an n-byte write.
func pokeArgs(name string, args []Value, at Pos, n int) (*Array, int, int64, error) {
	arr, err := needArray(name, args, 0, at)
	if err != nil {
		return nil, 0, 0, err
	}
	i, err := needInt(name, args, 1, at)
	if err != nil {
		return nil, 0, 0, err
	}
	v, err := needInt(name, args, 2, at)
	if err != nil {
		return nil, 0, 0, err
	}
	max := int64(1)<<(8*n) - 1
	if v < 0 || v > max {
		return nil, 0, 0, argErr(name, 2, at, "値は 0 から %d の範囲で指定してください。%d が指定されました", max, v)
	}
	if i < 0 || int(i)+n > len(arr.Elems) {
		return nil, 0, 0, argErr(name, 1, at, "バイト %d から %d は範囲外です（長さ %d）", i, i+int64(n)-1, len(arr.Elems))
	}
	return arr, int(i), v, nil
}
