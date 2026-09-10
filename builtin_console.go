// P4 builtins: console input/output.
package main

func init() {
	// print(args...): mes without the trailing newline.
	register("print", 0, -1, func(in *Interp, args []Value, at Pos) (Value, error) {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = Stringify(a)
		}
		out := ""
		for i, p := range parts {
			if i > 0 {
				out += " "
			}
			out += p
		}
		in.be.Print(out)
		return Null(), nil
	})

	// cls(): clear the screen.
	register("cls", 0, 0, func(in *Interp, args []Value, at Pos) (Value, error) {
		in.be.Clear()
		return Null(), nil
	})

	// color(r, g, b [, a]): set text/draw color (0-255 each).
	// a is the alpha (0 transparent .. 255 opaque, default 255).
	// Terminals cannot render text alpha, so CUI ignores it.
	// color() resets the color.
	register("color", 0, 4, func(in *Interp, args []Value, at Pos) (Value, error) {
		if len(args) == 0 {
			in.be.ResetColor()
			return Null(), nil
		}
		if len(args) != 3 && len(args) != 4 {
			return Null(), rtErrf(at, "color は 0 個、3 個または 4 個の引数が必要ですが、%d 個が渡されました", len(args))
		}
		rgba := []int{0, 0, 0, 255}
		for i := range args {
			v, err := needInt("color", args, i, at)
			if err != nil {
				return Null(), err
			}
			if v < 0 || v > 255 {
				return Null(), argErr("color", i, at, "0 から 255 の範囲で指定してください。%d が指定されました", v)
			}
			rgba[i] = int(v)
		}
		in.be.SetColor(rgba[0], rgba[1], rgba[2], rgba[3])
		return Null(), nil
	})

	// title(s): ask the terminal to change its title (best effort).
	register("title", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		s, err := needString("title", args, 0, at)
		if err != nil {
			return Null(), err
		}
		in.be.SetTitle(s)
		return Null(), nil
	})

	// pos(x, y): move the cursor. CUIでは文字セル、GUIではピクセル基準。負値も許可し画面外への描画に利用できます。
	register("pos", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		x, err := needInt("pos", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("pos", args, 1, at)
		if err != nil {
			return Null(), err
		}
		in.be.MoveTo(int(x), int(y))
		return Null(), nil
	})

	// input([prompt]): read one line, returned as a string.
	// EOF yields null.
	register("input", 0, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		prompt := ""
		if len(args) == 1 {
			var err error
			prompt, err = needString("input", args, 0, at)
			if err != nil {
				return Null(), err
			}
		}
		line, ok := in.be.ReadLine(prompt)
		if !ok {
			return Null(), nil
		}
		return Str(line), nil
	})
}
