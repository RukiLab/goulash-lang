//go:build gui

// GUI-only builtins (G4 drawing extension: pngsave, paint, font).
// Registered only in -tags gui builds; console builds report these
// names as undefined functions.
package main

func init() {
	// pngsave(path [, id]): save a draw buffer as PNG (id defaults to
	// the current target).
	register("pngsave", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "pngsave")
		if err != nil {
			return Null(), err
		}
		p, err := needString("pngsave", args, 0, at)
		if err != nil {
			return Null(), err
		}
		id := int64(wb.CurrentSel())
		if len(args) == 2 {
			id, err = needInt("pngsave", args, 1, at)
			if err != nil {
				return Null(), err
			}
		}
		if err := wb.SavePNG(p, int(id)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// paint(x, y): flood-fill the seed region with the current color.
	register("paint", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "paint")
		if err != nil {
			return Null(), err
		}
		x, err := needInt("paint", args, 0, at)
		if err != nil {
			return Null(), err
		}
		y, err := needInt("paint", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.FloodFill(int(x), int(y), wb.Foreground()); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// galpha(a): global drawing alpha 0-255 (shapes and blits; text stays
	// opaque). galpha(255) restores full opacity.
	register("galpha", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "galpha")
		if err != nil {
			return Null(), err
		}
		a, err := needInt("galpha", args, 0, at)
		if err != nil {
			return Null(), err
		}
		if err := wb.SetAlpha(int(a)); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})

	// font(size) / font(spec) / font(spec, size): text face size in
	// pixels (cell grid scales with it). spec is a font file path or a
	// system font name (e.g. "YuGothR.ttc", "meiryo.ttc").
	register("font", 1, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		wb, err := guiBE(in, at, "font")
		if err != nil {
			return Null(), err
		}
		if len(args) == 1 && args[0].K == KInt {
			if err := wb.SetFontSize(int(args[0].I)); err != nil {
				return Null(), rtErrf(at, "%s", err.Error())
			}
			return Null(), nil
		}
		spec, err := needString("font", args, 0, at)
		if err != nil {
			return Null(), err
		}
		size := 0
		if len(args) == 2 {
			v, err := needInt("font", args, 1, at)
			if err != nil {
				return Null(), err
			}
			size = int(v)
		} else {
			size = wb.FontSize()
		}
		if err := wb.SetFontFile(spec, size); err != nil {
			return Null(), rtErrf(at, "%s", err.Error())
		}
		return Null(), nil
	})
}
