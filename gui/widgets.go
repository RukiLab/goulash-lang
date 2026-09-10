//go:build gui

package gui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"

	"github.com/ebitenui/ebitenui"
	euimage "github.com/ebitenui/ebitenui/image"
	"github.com/ebitenui/ebitenui/widget"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Widgets (G3) on ebitenui: buttons, text inputs, lists, modal dialogs.
// Script threads enqueue mutations; the game thread applies them.
// HSP-style absolute positioning via fixedLayout.

// fixedLayout places children at exact rectangles (HSP-like coordinates).
type fixedLayout struct {
	w, h  int
	rects map[widget.PreferredSizeLocateableWidget]image.Rectangle
}

func (l *fixedLayout) PreferredSize(widgets []widget.PreferredSizeLocateableWidget) (int, int) {
	return l.w, l.h
}

func (l *fixedLayout) Layout(widgets []widget.PreferredSizeLocateableWidget, rect image.Rectangle) {
	for _, w := range widgets {
		if r, ok := l.rects[w]; ok {
			w.SetLocation(r)
		}
	}
}

type widgetKind int

const (
	wButton widgetKind = iota
	wInput
	wList
	wCheck
	wCombo
	wArea
	wToggle
)

// toggleState is a custom-drawn on/off switch (no ebitenui child;
// drawn in Draw, flipped in Update). All fields guarded by backend mu.
type toggleState struct {
	rect   image.Rectangle
	label  string
	on     bool
	onImg  *ebiten.Image // nil = default pill art
	offImg *ebiten.Image
}

// toggleFlip reports the state after a click edge at (x, y).
// Pure logic, unit-testable.
func toggleFlip(on bool, r image.Rectangle, x, y int, clicked, disabled bool) bool {
	if clicked && !disabled && image.Pt(x, y).In(r) {
		return !on
	}
	return on
}

type widgetEntry struct {
	id     int
	kind   widgetKind
	child  widget.PreferredSizeLocateableWidget
	remove widget.RemoveChildFunc
	// disabled freezes interaction (objprm "enable"; guarded by mu).
	disabled bool
	// button latch + list state (guarded by backend mu).
	pressed bool
	items   []string
	selIdx  int
	// cached mirrors the widget's text, refreshed on the game thread
	// every Update so InputText never blocks the script (a blocking
	// read mid-frame would stall cls/mes redraws and flicker).
	cached string
	// form widgets (G5): checkbox, combo, textarea mirrors.
	check   *widget.Checkbox
	combo   *widget.ListComboButton
	area    *widget.TextArea
	checked bool
	list    *widget.List
	// custom-drawn toggle switch (wToggle; no ebitenui child).
	toggle *toggleState
	// custom-drawn single-line editor (wInput; no ebitenui child).
	edit *inputState
}

// ensureUI builds the retained UI on the game thread.
func (b *WindowBackend) ensureUI() {
	if b.ui != nil {
		return
	}
	b.mu.Lock()
	w, h := b.w, b.h
	b.mu.Unlock()
	fix := &fixedLayout{w: w, h: h, rects: map[widget.PreferredSizeLocateableWidget]image.Rectangle{}}
	root := widget.NewContainer(widget.ContainerOpts.Layout(fix))
	b.fixLayout = fix
	b.root = root
	b.widgets = map[int]*widgetEntry{}
	b.ui = &ebitenui.UI{Container: root}
}

// placeChild adds a child at an absolute rect on the game thread.
func (b *WindowBackend) placeChild(id int, e *widgetEntry, child widget.PreferredSizeLocateableWidget, x, y, w, h int) {
	b.ensureUI()
	if old, ok := b.widgets[id]; ok {
		old.remove()
		delete(b.fixLayout.rects, old.child)
	}
	b.root.AddChild(child)
	b.fixLayout.rects[child] = image.Rect(x, y, x+w, y+h)
	remove := func() {
		b.root.RemoveChild(child)
		delete(b.fixLayout.rects, child)
	}
	e.child = child
	e.remove = remove
	b.widgets[id] = e
	b.root.RequestRelayout()
}

// placeCustom registers a custom-drawn entry (toggle/input: no
// ebitenui child), replacing any entry under id. Game thread only.
func (b *WindowBackend) placeCustom(id int, e *widgetEntry) {
	b.ensureUI()
	if old, ok := b.widgets[id]; ok {
		old.remove()
		if old.child != nil {
			delete(b.fixLayout.rects, old.child)
		}
	}
	e.child = nil
	e.remove = func() {}
	b.widgets[id] = e
	if b.root != nil {
		b.root.RequestRelayout()
	}
}

// removeWidgetLocked drops a widget; caller runs on the game thread.
func (b *WindowBackend) removeWidgetLocked(id int) error {
	e, ok := b.widgets[id]
	if !ok {
		return fmt.Errorf("不明なウィジェット %d です", id)
	}
	e.remove()
	delete(b.widgets, id)
	return nil
}

// ---------- theme (plain dark) ----------

func nine(c color.NRGBA) *euimage.NineSlice {
	return euimage.NewNineSliceColor(c)
}

// nineImage stretches src across the widget (pure-stretch nine-slice,
// zero borders). Snapshots are already button-sized, so it is exact.
func nineImage(src *ebiten.Image) *euimage.NineSlice {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	return euimage.NewNineSlice(src, [3]int{0, w, 0}, [3]int{0, h, 0})
}

// uiFace adapts our face to ebitenui's *text.Face (pointer to interface).
// The face swaps on font(), so read it under mu.
func (b *WindowBackend) uiFace() *text.Face {
	b.mu.Lock()
	defer b.mu.Unlock()
	f := text.Face(b.face)
	return &f
}

func (b *WindowBackend) buttonImage() *widget.ButtonImage {
	idle := nine(color.NRGBA{0x3A, 0x3F, 0x4A, 0xFF})
	return &widget.ButtonImage{
		Idle: idle, Hover: nine(color.NRGBA{0x4A, 0x50, 0x5C, 0xFF}),
		Pressed: nine(color.NRGBA{0x2A, 0x2E, 0x36, 0xFF}), Disabled: idle,
	}
}

func (b *WindowBackend) inputImage() *widget.TextInputImage {
	idle := nine(color.NRGBA{0x22, 0x26, 0x2E, 0xFF})
	return &widget.TextInputImage{Idle: idle, Disabled: idle}
}

// checkBoxSize is the checkbox art edge length, tracking font().
// Even and >= 12 so the nine-slice faces below reassemble pixel-perfect.
func (b *WindowBackend) checkBoxSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := int(b.fontSize*1.25 + 0.5)
	if s < 12 {
		s = 12
	}
	if s > 48 {
		s = 48
	}
	return s &^ 1
}

// checkArtRGBA renders one size×size checkbox face into plain pixels:
// solid body with an optional check glyph. Pure Go, so tests can
// inspect the art without a game loop.
func checkArtRGBA(size int, body color.NRGBA, check bool, mark color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, body)
		}
	}
	if check {
		drawCheckMark(img, size, mark)
	}
	return img
}

// checkArt uploads one checkbox face. Created on the game thread
// (like all images; ebiten.NewImage also works headless in tests).
func checkArt(size int, body color.NRGBA, check bool, mark color.NRGBA) *ebiten.Image {
	return ebiten.NewImageFromImage(checkArtRGBA(size, body, check, mark))
}

// drawCheckMark paints a check from (0.28,0.55)->(0.45,0.72)->(0.74,0.30)
// with a square brush, so the checked state reads at any box size.
func drawCheckMark(img *image.NRGBA, size int, c color.NRGBA) {
	f := float64(size)
	pts := [][2]float64{{0.28 * f, 0.55 * f}, {0.45 * f, 0.72 * f}, {0.74 * f, 0.30 * f}}
	r := size / 8
	if r < 1 {
		r = 1
	}
	dot := func(x, y int) {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				img.SetNRGBA(x+dx, y+dy, c)
			}
		}
	}
	for i := 0; i+1 < len(pts); i++ {
		x0, y0, x1, y1 := pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]
		dx, dy := x1-x0, y1-y0
		steps := int(absf(dx) + absf(dy) + 1)
		for s := 0; s <= steps; s++ {
			t := float64(s) / float64(steps)
			dot(int(x0+dx*t+0.5), int(y0+dy*t+0.5))
		}
	}
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// checkImage builds the checkbox faces with a real minimum size
// (checked = filled theme blue + check glyph).
//
// NOTE: solid-color nine slices (NewNineSliceColor) report MinSize 0,
// and ebitenui draws the *labeled* box at checkboxPreferredSize, so the
// old faces rendered 0x0 (invisible box, label text only). Sized art
// reassembles pixel-perfect at MinSize == size and fixes it.
func (b *WindowBackend) checkImage() *widget.CheckboxImage {
	s := b.checkBoxSize()
	half := s / 2
	mk := func(body color.NRGBA, check bool, mark color.NRGBA) *euimage.NineSlice {
		return euimage.NewNineSliceSimple(checkArt(s, body, check, mark), half, 0)
	}
	box := color.NRGBA{0x22, 0x26, 0x2E, 0xFF}
	boxHover := color.NRGBA{0x33, 0x39, 0x45, 0xFF}
	boxDis := color.NRGBA{0x1A, 0x1D, 0x24, 0xFF}
	fill := color.NRGBA{0x2D, 0x4A, 0x7A, 0xFF}
	fillHover := color.NRGBA{0x3A, 0x5C, 0x96, 0xFF}
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	grey := color.NRGBA{0x77, 0x77, 0x77, 0xFF}
	return &widget.CheckboxImage{
		Unchecked:         mk(box, false, white),
		UncheckedHovered:  mk(boxHover, false, white),
		UncheckedDisabled: mk(boxDis, false, grey),
		Checked:           mk(fill, true, white),
		CheckedHovered:    mk(fillHover, true, white),
		CheckedDisabled:   mk(boxDis, true, grey),
	}
}

// listEntryColors defines every list state. Press/hover backgrounds must
// never stay nil (transparent): with SelectPressed the entry is selected
// on press, and a nil Pressed background would hide the highlight until
// mouse release even though selected() already flipped.
func listEntryColors() *widget.ListEntryColor {
	return &widget.ListEntryColor{
		Unselected:                 color.NRGBA{0xDD, 0xDD, 0xDD, 0xFF},
		Selected:                   color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF},
		DisabledUnselected:         color.NRGBA{0x77, 0x77, 0x77, 0xFF},
		DisabledSelected:           color.NRGBA{0x99, 0x99, 0x99, 0xFF},
		SelectingBackground:        color.NRGBA{0x2D, 0x4A, 0x7A, 0xFF},
		SelectedBackground:         color.NRGBA{0x2D, 0x4A, 0x7A, 0xFF},
		FocusedBackground:          color.NRGBA{0x33, 0x39, 0x45, 0xFF},
		SelectingFocusedBackground: color.NRGBA{0x2D, 0x4A, 0x7A, 0xFF},
		SelectedFocusedBackground:  color.NRGBA{0x3A, 0x5C, 0x96, 0xFF},
		DisabledSelectedBackground: color.NRGBA{0x22, 0x26, 0x30, 0xFF},
	}
}

// ---------- script-thread API (mutations block until applied) ----------

// AddButton creates (or replaces) a button. imgs holds up to 4
// picload/dropload buffer ids for the Idle/Hover/Pressed/Disabled
// states (0 = slot unused): missing Hover/Pressed fall back to Idle,
// missing Disabled falls back to a darkened Idle.
func (b *WindowBackend) AddButton(id int, label string, x, y, w, h int, imgs ...int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("button のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	if len(imgs) > 4 {
		return fmt.Errorf("button：画像は最大 4 個（通常/ホバー/押下/無効）です。%d 個が指定されました", len(imgs))
	}
	for _, imgID := range imgs {
		if imgID != 0 && !b.hasBuf(imgID) {
			return fmt.Errorf("button：不明な画像バッファ %d です", imgID)
		}
	}
	var opErr error
	b.runOnLoop(func() {
		face := b.uiFace()
		e := &widgetEntry{id: id, kind: wButton, selIdx: -1}
		opts := []widget.ButtonOpt{
			widget.ButtonOpts.Image(b.buttonImage()),
			widget.ButtonOpts.TextPadding(widget.NewInsetsSimple(4)),
			widget.ButtonOpts.ClickedHandler(func(args *widget.ButtonClickedEventArgs) {
				b.mu.Lock()
				e.pressed = true
				b.mu.Unlock()
			}),
		}
		white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
		if len(imgs) > 0 && imgs[0] != 0 {
			// Snapshot each state into button-sized images: Graphic
			// centers its image, so a raw window-sized buffer would
			// show the wrong (usually transparent) region. Exact-fit
			// buffers keep the live reference.
			snap := func(src *ebiten.Image) *ebiten.Image {
				sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
				if sw == w && sh == h {
					return src
				}
				out := ebiten.NewImage(w, h)
				op := &ebiten.DrawImageOptions{}
				op.GeoM.Scale(float64(w)/float64(sw), float64(h)/float64(sh))
				out.DrawImage(src, op)
				return out
			}
			srcs := make([]*ebiten.Image, 4)
			for i, imgID := range imgs {
				if imgID == 0 {
					continue
				}
				src := b.targets[imgID]
				if src == nil {
					opErr = fmt.Errorf("button：画像バッファ %d はまだ準備できていません", imgID)
					return
				}
				srcs[i] = snap(src)
			}
			idle := srcs[0]
			hover := srcs[1]
			if hover == nil {
				hover = idle
			}
			pressed := srcs[2]
			if pressed == nil {
				pressed = idle
			}
			disabled := srcs[3]
			if disabled == nil {
				disabled = dimImage(idle)
			}
			if label != "" {
				// Labeled: the icon row is centered by design, and
				// TextAndImage enables ebitenui's per-state swap
				// (separate Text+Graphic opts leave it frozen).
				opts = append(opts, widget.ButtonOpts.TextAndImage(label, face, &widget.GraphicImage{
					Idle: idle, Hover: hover, Pressed: pressed, Disabled: disabled,
				}, &widget.ButtonTextColor{Idle: white}))
			} else {
				// Icon-only: state faces fill the button exactly.
				// (A Graphic row would add an empty-text member and
				// shift the icon a few pixels off center.)
				opts = append(opts, widget.ButtonOpts.Image(&widget.ButtonImage{
					Idle: nineImage(idle), Hover: nineImage(hover),
					Pressed: nineImage(pressed), Disabled: nineImage(disabled),
				}))
			}
		} else if label != "" {
			opts = append(opts, widget.ButtonOpts.Text(label, face, &widget.ButtonTextColor{
				Idle: white,
			}))
		}
		b.placeChild(id, e, widget.NewButton(opts...), x, y, w, h)
	})
	return opErr
}

// dimImage returns a darkened copy for the Disabled state.
// Call on the game thread (creates an image).
func dimImage(src *ebiten.Image) *ebiten.Image {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	out := ebiten.NewImage(w, h)
	op := &ebiten.DrawImageOptions{}
	op.ColorScale.Scale(0.45, 0.45, 0.45, 1)
	out.DrawImage(src, op)
	return out
}

// Pressed reports a click since the last call (consumed).
func (b *WindowBackend) Pressed(id int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wButton {
		return false, fmt.Errorf("pressed：不明なボタン %d です", id)
	}
	p := e.pressed
	e.pressed = false
	return p, nil
}

// AddInput lives in edit.go (custom-drawn editor).

// syncWidgetCache mirrors widget runtime state (game thread only;
// called from Update). Script reads use the mirrors so getters never
// block the script mid-frame (cf. the gettext flicker fix).
func (b *WindowBackend) syncWidgetCache() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.widgets {
		switch {
		case e.kind == wArea && e.area != nil:
			e.cached = e.area.GetText()
		case e.kind == wCheck && e.check != nil:
			e.checked = e.check.State() == widget.WidgetChecked
		case e.kind == wToggle && e.toggle != nil:
			e.checked = e.toggle.on
		case e.kind == wCombo && e.combo != nil:
			sel := e.combo.SelectedEntry()
			e.selIdx = -1
			for i, s := range e.items {
				if s == sel {
					e.selIdx = i
					break
				}
			}
		}
	}
}

// InputText lives in edit.go (custom editor state).

// AddList creates (or replaces) a list box.
func (b *WindowBackend) AddList(id, x, y, w, h int, items []string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("listbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	entries := make([]any, len(items))
	for i, s := range items {
		entries[i] = s
	}
	b.runOnLoop(func() {
		face := b.uiFace()
		e := &widgetEntry{id: id, kind: wList, selIdx: -1}
		e.items = append([]string(nil), items...)
		list := widget.NewList(
			widget.ListOpts.ContainerOpts(widget.ContainerOpts.BackgroundImage(
				nine(color.NRGBA{0x1A, 0x1D, 0x24, 0xFF}))),
			widget.ListOpts.ScrollContainerImage(&widget.ScrollContainerImage{
				Idle: nine(color.NRGBA{0x1A, 0x1D, 0x24, 0xFF}),
				Mask: nine(color.NRGBA{0x1A, 0x1D, 0x24, 0xFF}),
			}),
			widget.ListOpts.HideHorizontalSlider(),
			widget.ListOpts.HideVerticalSlider(),
			// Select on press (not release): native listboxes highlight
			// the instant the button goes down; release-to-select
			// feels like a laggy click.
			widget.ListOpts.SelectPressed(),
			widget.ListOpts.Entries(entries),
			widget.ListOpts.EntryLabelFunc(func(en any) string {
				if s, ok := en.(string); ok {
					return s
				}
				return ""
			}),
			widget.ListOpts.EntryFontFace(face),
			widget.ListOpts.EntryColor(listEntryColors()),
			widget.ListOpts.EntryTextPadding(widget.NewInsetsSimple(2)),
			widget.ListOpts.EntrySelectedHandler(func(args *widget.ListEntrySelectedEventArgs) {
				b.mu.Lock()
				e.selIdx = -1
				for i, s := range e.items {
					if s == args.Entry {
						e.selIdx = i
						break
					}
				}
				b.mu.Unlock()
			}),
		)
		b.placeChild(id, e, list, x, y, w, h)
		e.list = list
	})
	return nil
}

// SelectedIndex returns the list/combo selection (-1 when none).
func (b *WindowBackend) SelectedIndex(id int) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || (e.kind != wList && e.kind != wCombo) {
		return 0, fmt.Errorf("selected：不明な listbox %d です", id)
	}
	return e.selIdx, nil
}

// AddCheck creates (or replaces) a checkbox. Non-positive w/h fall back
// to the preferred size (box + label), since the box art no longer
// scales with the layout rect.
func (b *WindowBackend) AddCheck(id int, label string, x, y, w, h int, checked bool) error {
	b.runOnLoop(func() {
		face := b.uiFace()
		white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
		grey := color.NRGBA{0x77, 0x77, 0x77, 0xFF}
		init := widget.WidgetUnchecked
		if checked {
			init = widget.WidgetChecked
		}
		cb := widget.NewCheckbox(
			widget.CheckboxOpts.Image(b.checkImage()),
			widget.CheckboxOpts.Text(label, face, &widget.LabelColor{Idle: white, Disabled: grey}),
			widget.CheckboxOpts.Spacing(6),
			widget.CheckboxOpts.InitialState(init),
		)
		// Validate now (fail fast): PreferredSize needs computedParams,
		// which ebitenui otherwise fills lazily on first UI update.
		cb.Validate()
		if pw, ph := cb.PreferredSize(); w <= 0 || h <= 0 {
			if w <= 0 {
				w = pw
			}
			if h <= 0 {
				h = ph
			}
		}
		e := &widgetEntry{id: id, kind: wCheck, selIdx: -1, check: cb, checked: checked}
		b.placeChild(id, e, cb, x, y, w, h)
	})
	return nil
}

// Checked reports a checkbox/toggle state (script-thread safe, never
// blocks: it reads the Update-time mirror).
func (b *WindowBackend) Checked(id int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || (e.kind != wCheck && e.kind != wToggle) {
		return false, fmt.Errorf("checked：不明な checkbox/toggle %d です", id)
	}
	return e.checked, nil
}

// AddToggle creates (or replaces) a toggle switch. Non-positive w/h
// auto-size from the font (pill h = line height, w = 2h). imgs holds
// up to 2 picload/dropload buffer ids for the on/off states (0 =
// slot unused): missing off falls back to a darkened on; both
// missing draws the default pill. checked(id) reads the state.
func (b *WindowBackend) AddToggle(id int, label string, x, y, w, h int, imgs ...int) error {
	if len(imgs) > 2 {
		return fmt.Errorf("toggle：画像は最大 2 個（オン/オフ）です。%d 個が指定されました", len(imgs))
	}
	for _, imgID := range imgs {
		if imgID != 0 && !b.hasBuf(imgID) {
			return fmt.Errorf("toggle：不明な画像バッファ %d です", imgID)
		}
	}
	b.mu.Lock()
	lh := b.lineH
	if lh <= 0 {
		lh = 20
	}
	b.mu.Unlock()
	if h <= 0 {
		h = int(lh + 0.5)
	}
	if w <= 0 {
		w = 2 * h
	}
	if w <= 0 || h <= 0 {
		return fmt.Errorf("toggle のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	var opErr error
	b.runOnLoop(func() {
		var onImg, offImg *ebiten.Image
		if len(imgs) > 0 && imgs[0] != 0 {
			src := b.targets[imgs[0]]
			if src == nil {
				opErr = fmt.Errorf("toggle：画像バッファ %d はまだ準備できていません", imgs[0])
				return
			}
			onImg = snapImage(src, w, h)
		}
		if len(imgs) > 1 && imgs[1] != 0 {
			src := b.targets[imgs[1]]
			if src == nil {
				opErr = fmt.Errorf("toggle：画像バッファ %d はまだ準備できていません", imgs[1])
				return
			}
			offImg = snapImage(src, w, h)
		}
		if onImg != nil && offImg == nil {
			offImg = dimImage(onImg)
		}
		st := &toggleState{
			rect: image.Rect(x, y, x+w, y+h), label: label,
			onImg: onImg, offImg: offImg,
		}
		b.placeCustom(id, &widgetEntry{id: id, kind: wToggle, selIdx: -1, toggle: st})
	})
	return opErr
}

// snapImage scales src to exactly w×h (buttons and toggles draw
// state faces edge-to-edge).
func snapImage(src *ebiten.Image, w, h int) *ebiten.Image {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw == w && sh == h {
		return src
	}
	out := ebiten.NewImage(w, h)
	op := &ebiten.DrawImageOptions{}
	if sw > 0 && sh > 0 {
		op.GeoM.Scale(float64(w)/float64(sw), float64(h)/float64(sh))
	}
	out.DrawImage(src, op)
	return out
}

// pumpToggles flips toggle switches on left-click edges (game thread;
// called from Update after pumpInput). Clicks are observed, not
// consumed: window-level clicked(0) still fires (buttons behave so).
func (b *WindowBackend) pumpToggles() {
	x, y := ebiten.CursorPosition()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.widgets == nil || !b.clickFlag[0] {
		return
	}
	for _, e := range b.widgets {
		if e.kind != wToggle || e.toggle == nil {
			continue
		}
		if next := toggleFlip(e.toggle.on, e.toggle.rect, x, y, true, e.disabled); next != e.toggle.on {
			e.toggle.on = next
			e.checked = next
			return // one click flips one switch
		}
	}
}

// drawToggles paints custom switches above the UI (game thread;
// called from Draw). Snapshots ride mu; images stay game-local.
func (b *WindowBackend) drawToggles(screen *ebiten.Image) {
	type snap struct {
		st       toggleState
		disabled bool
		face     *text.GoTextFace
		ascent   float64
	}
	b.mu.Lock()
	var ss []snap
	for _, e := range b.widgets {
		if e.kind == wToggle && e.toggle != nil {
			s := snap{st: *e.toggle, disabled: e.disabled, face: b.face}
			if b.face != nil {
				s.ascent = b.face.Metrics().HAscent
			}
			ss = append(ss, s)
		}
	}
	b.mu.Unlock()
	for _, s := range ss {
		drawToggle(screen, &s.st, s.disabled, s.face, s.ascent)
	}
}

// drawToggle paints one switch: images when provided, else the pill
// (rounded body + sliding knob) with the label at its right.
func drawToggle(screen *ebiten.Image, st *toggleState, disabled bool, face *text.GoTextFace, ascent float64) {
	r := st.rect
	img := st.offImg
	if st.on {
		img = st.onImg
	}
	dim := func(c color.NRGBA) color.NRGBA {
		if !disabled {
			return c
		}
		return color.NRGBA{R: c.R / 2, G: c.G / 2, B: c.B / 2, A: c.A}
	}
	if img != nil {
		op := &ebiten.DrawImageOptions{}
		sw, sh := img.Bounds().Dx(), img.Bounds().Dy()
		if sw > 0 && sh > 0 {
			op.GeoM.Scale(float64(r.Dx())/float64(sw), float64(r.Dy())/float64(sh))
		}
		op.GeoM.Translate(float64(r.Min.X), float64(r.Min.Y))
		if disabled {
			op.ColorScale.Scale(0.5, 0.5, 0.5, 1)
		}
		screen.DrawImage(img, op)
	} else {
		h := float64(r.Dy())
		rad := float32(h / 2)
		body := dim(color.NRGBA{0x3A, 0x3F, 0x4A, 0xFF})
		if st.on {
			body = dim(color.NRGBA{0x2D, 0x4A, 0x7A, 0xFF})
		}
		cx0, cx1 := float32(r.Min.X)+rad, float32(r.Max.X)-rad
		cy := float32(r.Min.Y) + rad
		vector.DrawFilledCircle(screen, cx0, cy, rad, body, false)
		vector.DrawFilledCircle(screen, cx1, cy, rad, body, false)
		vector.DrawFilledRect(screen, cx0, float32(r.Min.Y), cx1-cx0, float32(h), body, false)
		knob := dim(color.NRGBA{0xF2, 0xF2, 0xF2, 0xFF})
		kx := cx0
		if st.on {
			kx = cx1
		}
		vector.DrawFilledCircle(screen, kx, cy, rad-3, knob, false)
	}
	if st.label != "" && face != nil {
		op := &text.DrawOptions{}
		op.GeoM.Translate(float64(r.Max.X)+6, float64(r.Min.Y)+ascent)
		op.ColorScale.Reset()
		fg := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
		if disabled {
			fg = color.NRGBA{0x77, 0x77, 0x77, 0xFF}
		}
		op.ColorScale.ScaleWithColor(fg)
		text.Draw(screen, st.label, face, op)
	}
}

// comboSliderParams images the dropdown scrollbar. The handle reuses
// the button face so it stays visible at any theme.
func comboSliderParams() *widget.SliderParams {
	track := nine(color.NRGBA{0x1A, 0x1D, 0x24, 0xFF})
	idle := nine(color.NRGBA{0x4A, 0x50, 0x5C, 0xFF})
	hover := nine(color.NRGBA{0x5A, 0x60, 0x6E, 0xFF})
	pressed := nine(color.NRGBA{0x2A, 0x2E, 0x36, 0xFF})
	return &widget.SliderParams{
		TrackImage: &widget.SliderTrackImage{Idle: track, Hover: track, Disabled: track},
		HandleImage: &widget.ButtonImage{
			Idle: idle, Hover: hover, Pressed: pressed, Disabled: idle,
		},
	}
}

// AddCombo creates (or replaces) a dropdown list. sel is the initial
// index (-1 for none).
func (b *WindowBackend) AddCombo(id, x, y, w, h int, items []string, sel int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("combox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	if sel < -1 || sel >= len(items) {
		return fmt.Errorf("combox：選択 %d は範囲外です（%d 項目）", sel, len(items))
	}
	entries := make([]any, len(items))
	for i, s := range items {
		entries[i] = s
	}
	b.runOnLoop(func() {
		face := b.uiFace()
		white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
		pressed := true
		e := &widgetEntry{id: id, kind: wCombo, selIdx: -1}
		e.items = append([]string(nil), items...)
		labelOf := func(en any) string {
			if s, ok := en.(string); ok {
				return s
			}
			return ""
		}
		cb := widget.NewListComboButton(
			widget.ListComboButtonOpts.ButtonParams(&widget.ButtonParams{
				Image:       b.buttonImage(),
				TextColor:   &widget.ButtonTextColor{Idle: white},
				TextPadding: widget.NewInsetsSimple(4),
				TextFace:    face,
			}),
			widget.ListComboButtonOpts.ListParams(&widget.ListParams{
				EntryFace:        face,
				EntryColor:       listEntryColors(),
				EntryTextPadding: widget.NewInsetsSimple(2),
				SelectPressed:    &pressed,
				ScrollContainerImage: &widget.ScrollContainerImage{
					Idle: nine(color.NRGBA{0x1A, 0x1D, 0x24, 0xFF}),
					Mask: nine(color.NRGBA{0x1A, 0x1D, 0x24, 0xFF}),
				},
				// The dropdown keeps its vertical slider: without images
				// the slider handle button renders with a nil Image and
				// panics on open (Button.draw dereferences Image.Idle).
				Slider: comboSliderParams(),
			}),
			widget.ListComboButtonOpts.Text(face, nil, &widget.ButtonTextColor{Idle: white}),
			widget.ListComboButtonOpts.Entries(entries),
			widget.ListComboButtonOpts.EntryLabelFunc(labelOf, labelOf),
			widget.ListComboButtonOpts.EntrySelectedHandler(func(args *widget.ListComboButtonEntrySelectedEventArgs) {
				b.mu.Lock()
				e.selIdx = -1
				for i, s := range e.items {
					if s == args.Entry {
						e.selIdx = i
						break
					}
				}
				b.mu.Unlock()
			}),
			widget.ListComboButtonOpts.MaxContentHeight(h*4),
		)
		if sel >= 0 {
			cb.SetSelectedEntry(entries[sel])
			e.selIdx = sel
		}
		e.combo = cb
		b.placeChild(id, e, cb, x, y, w, h)
	})
	return nil
}

// AddArea creates (or replaces) a multiline text box.
func (b *WindowBackend) AddArea(id, x, y, w, h int, text string) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("mesbox のサイズは正の値である必要があります。%d x %d が指定されました", w, h)
	}
	b.runOnLoop(func() {
		face := b.uiFace()
		white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
		ta := widget.NewTextArea(
			widget.TextAreaOpts.ContainerOpts(widget.ContainerOpts.BackgroundImage(
				nine(color.NRGBA{0x22, 0x26, 0x2E, 0xFF}))),
			widget.TextAreaOpts.ScrollContainerImage(&widget.ScrollContainerImage{
				Idle: nine(color.NRGBA{0x1A, 0x1D, 0x24, 0xFF}),
				Mask: nine(color.NRGBA{0x1A, 0x1D, 0x24, 0xFF}),
			}),
			widget.TextAreaOpts.FontFace(face),
			widget.TextAreaOpts.FontColor(white),
			widget.TextAreaOpts.TextPadding(*widget.NewInsetsSimple(4)),
			widget.TextAreaOpts.Text(text),
		)
		e := &widgetEntry{id: id, kind: wArea, selIdx: -1, area: ta, cached: text}
		b.placeChild(id, e, ta, x, y, w, h)
	})
	return nil
}

// AreaText returns the multiline box content (script-thread safe,
// never blocks: it reads the Update-time mirror).
func (b *WindowBackend) AreaText(id int) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wArea || e.area == nil {
		return "", fmt.Errorf("getstr：不明な mesbox %d です", id)
	}
	return e.cached, nil
}

// SetAreaText replaces the multiline box content (game thread).
// ebitenui TextArea is display-only (no keyboard editing), so mesbox
// works as a program-driven log viewer: setstr() writes, getstr()
// reads. The mirror updates inline so a following getstr() is exact.
func (b *WindowBackend) SetAreaText(id int, text string) error {
	// Existence is checked up front (like AddButton/AddArea) so unknown
	// ids fail without blocking on the game thread.
	b.mu.Lock()
	e, ok := b.widgets[id]
	if !ok || e.kind != wArea || e.area == nil {
		b.mu.Unlock()
		return fmt.Errorf("setstr：不明な mesbox %d です", id)
	}
	b.mu.Unlock()
	b.runOnLoop(func() {
		e.area.SetText(text)
		b.mu.Lock()
		e.cached = text
		b.mu.Unlock()
	})
	return nil
}

// SetEnabled toggles widget interactivity; applied on the game thread.
// Custom-drawn entries (toggle/input) track it in e.disabled since
// they have no ebitenui child to grey out.
func (b *WindowBackend) SetEnabled(id int, on bool) error {
	b.mu.Lock()
	_, ok := b.widgets[id]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("objprm：不明なウィジェット %d です", id)
	}
	b.runOnLoop(func() {
		if e, ok := b.widgets[id]; ok {
			e.disabled = !on
			if e.child != nil {
				e.child.GetWidget().Disabled = !on
			}
		}
	})
	return nil
}

// RemoveWidget drops one widget.
func (b *WindowBackend) RemoveWidget(id int) error {
	b.mu.Lock()
	_, ok := b.widgets[id]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("不明なウィジェット %d です", id)
	}
	var opErr error
	b.runOnLoop(func() {
		opErr = b.removeWidgetLocked(id)
	})
	return opErr
}

// ClearWidgets drops all widgets.
func (b *WindowBackend) ClearWidgets() {
	b.runOnLoop(func() {
		for id := range b.widgets {
			_ = b.removeWidgetLocked(id)
		}
	})
}

// HasWidget reports registry presence (tests).
func (b *WindowBackend) HasWidget(id int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.widgets[id]
	return ok
}

// Dialog shows a modal choice and blocks until answered.
// mode: "ok" -> 1, "okcancel" -> 1/0, "yesno" -> 1/0.
func (b *WindowBackend) Dialog(msg, mode string) (int, error) {
	var labels []string
	var values []int
	switch mode {
	case "ok":
		labels, values = []string{"OK"}, []int{1}
	case "okcancel":
		labels, values = []string{"OK", "Cancel"}, []int{1, 0}
	case "yesno":
		labels, values = []string{"Yes", "No"}, []int{1, 0}
	default:
		return 0, fmt.Errorf("dialog：不明なモード %q です（ok/okcancel/yesno が必要です）", mode)
	}
	result := make(chan int, 1)
	var once sync.Once
	finish := func(v int) {
		once.Do(func() { result <- v })
	}
	b.runOnLoop(func() {
		b.ensureUI()
		face := b.uiFace()
		white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
		inner := widget.NewContainer(
			widget.ContainerOpts.BackgroundImage(nine(color.NRGBA{0x26, 0x2B, 0x34, 0xFF})),
			widget.ContainerOpts.Layout(widget.NewRowLayout(
				widget.RowLayoutOpts.Direction(widget.DirectionVertical),
				widget.RowLayoutOpts.Spacing(12),
				widget.RowLayoutOpts.Padding(widget.NewInsetsSimple(16)))),
		)
		b.mu.Lock()
		ww, wh := b.w, b.h
		vface, lineH := b.face, b.lineH
		b.mu.Unlock()
		if lineH <= 0 {
			lineH = 20
		}
		maxW := float64(ww) - 128
		if maxW < 200 {
			maxW = 200
		}
		lines := wrapDialogText(msg, vface, maxW)
		for _, ln := range lines {
			inner.AddChild(widget.NewLabel(
				widget.LabelOpts.Text(ln, face, &widget.LabelColor{Idle: white}),
			))
		}
		btnRow := widget.NewContainer(
			widget.ContainerOpts.Layout(widget.NewRowLayout(
				widget.RowLayoutOpts.Direction(widget.DirectionHorizontal),
				widget.RowLayoutOpts.Spacing(12))),
		)
		win := widget.NewWindow(
			widget.WindowOpts.Contents(inner),
			widget.WindowOpts.Modal(),
			widget.WindowOpts.ClosedHandler(func(args *widget.WindowClosedEventArgs) {
				finish(0)
			}),
		)
		remove := b.ui.AddWindow(win)
		for i, lab := range labels {
			v := values[i]
			btnRow.AddChild(widget.NewButton(
				widget.ButtonOpts.Image(b.buttonImage()),
				widget.ButtonOpts.Text(lab, face, &widget.ButtonTextColor{Idle: white}),
				widget.ButtonOpts.TextPadding(widget.NewInsetsSimple(6)),
				widget.ButtonOpts.ClickedHandler(func(args *widget.ButtonClickedEventArgs) {
					remove()
					finish(v)
				}),
			))
		}
		inner.AddChild(btnRow)
		needW := 0.0
		for _, ln := range lines {
			if w, _ := text.Measure(ln, vface, 0); w > needW {
				needW = w
			}
		}
		bw := int(needW) + 96
		if bw < 240 {
			bw = 240
		}
		if bw > ww-32 {
			bw = ww - 32
		}
		bh := int(float64(len(lines))*lineH) + 140
		if bh > wh-32 {
			bh = wh - 32
		}
		win.SetLocation(rectCentered(ww, wh, bw, bh))
	})
	return <-result, nil
}

// isCJKBreakable reports runes that may start a wrapped line anywhere
// (Japanese kana/kanji, Hangul, CJK symbols, fullwidth forms).
func isCJKBreakable(r rune) bool {
	switch {
	case r >= 0x3040 && r <= 0x30FF: // hiragana + katakana
		return true
	case r >= 0x3400 && r <= 0x9FFF: // CJK ext-A .. ideographs
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // hangul syllables
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // fullwidth forms
		return true
	}
	return false
}

// wrapDialogText breaks s into lines fitting maxW pixels (measured
// with face). Explicit newlines are honored; Latin prefers word
// boundaries while CJK breaks anywhere.
func wrapDialogText(s string, face *text.GoTextFace, maxW float64) []string {
	if face == nil {
		return strings.Split(s, "\n")
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		rs := []rune(para)
		for len(rs) > 0 {
			n := len(rs)
			for n > 0 {
				w, _ := text.Measure(string(rs[:n]), face, 0)
				if w <= maxW {
					break
				}
				n--
			}
			if n == 0 {
				n = 1 // single rune wider than the window
			}
			if n < len(rs) {
				// Back to the last space when cutting inside a
				// Latin word (both sides non-space, non-CJK).
				tail := rs[:n]
				sp := -1
				for i := len(tail) - 1; i >= 0; i-- {
					if tail[i] == ' ' {
						sp = i
						break
					}
				}
				if sp > 0 && !isCJKBreakable(rs[sp-1]) && !isCJKBreakable(rs[n]) {
					n = sp
				}
			}
			lines = append(lines, strings.TrimRight(string(rs[:n]), " "))
			rs = rs[n:]
			for len(rs) > 0 && rs[0] == ' ' {
				rs = rs[1:]
			}
		}
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

func rectCentered(ww, wh, bw, bh int) image.Rectangle {
	x := (ww - bw) / 2
	if x < 0 {
		x = 0
	}
	y := (wh - bh) / 2
	if y < 0 {
		y = 0
	}
	return image.Rect(x, y, x+bw, y+bh)
}
