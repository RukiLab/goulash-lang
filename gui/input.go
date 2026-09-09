//go:build gui

package gui

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Key-repeat timing in ticks (@60fps): first fire is immediate on a new
// press, then repDelay ticks later, then every repInterval ticks.
const (
	repDelay    = 24
	repInterval = 4
)

// repCodes is the keyrep scan set (same numbering as getkey),
// ascending: the first pressed code wins. Modifier codes (16/17/18)
// are deferred: they fire only when no other key is pressed, so
// Shift+A reports the letter (shifted), not the modifier.
var repCodes []int

// isModCode reports modifier codes deferred by the keyrep scan.
func isModCode(code int) bool {
	return code == 16 || code == 17 || code == 18
}

func init() {
	for _, c := range []int{8, 9, 13, 16, 17, 18, 27, 32, 33, 34, 35, 36, 37, 38, 39, 40, 45, 46} {
		repCodes = append(repCodes, c)
	}
	for c := 48; c <= 57; c++ {
		repCodes = append(repCodes, c)
	}
	for c := 65; c <= 90; c++ {
		repCodes = append(repCodes, c)
	}
	for c := 112; c <= 123; c++ {
		repCodes = append(repCodes, c)
	}
	for _, c := range []int{186, 187, 188, 189, 190, 191, 192, 219, 220, 221, 222} {
		repCodes = append(repCodes, c)
	}
}

// repState tracks one repeat session.
type repState struct {
	code int
	dur  int64
	next int64
}

// repStep advances the repeat state machine over the observed
// (code, duration) pair and returns the firing code, or 0.
// A new key fires at once; a re-press after release (duration running
// backwards) fires at once too; a held key refires after repDelay
// ticks, then every repInterval ticks.
func repStep(st *repState, code int, dur int64) int {
	if code == 0 {
		st.code, st.dur, st.next = 0, 0, 0
		return 0
	}
	if code != st.code || dur < st.dur {
		st.code, st.dur = code, dur
		st.next = dur + repDelay
		return code
	}
	st.dur = dur
	if dur >= st.next {
		st.next = dur + repInterval
		return code
	}
	return 0
}

// modPressDur returns the press duration of a modifier code,
// either side (left or right); 0 when neither is held.
func modPressDur(code int) int64 {
	var l, r ebiten.Key
	switch code {
	case 16:
		l, r = ebiten.KeyShiftLeft, ebiten.KeyShiftRight
	case 17:
		l, r = ebiten.KeyControlLeft, ebiten.KeyControlRight
	case 18:
		l, r = ebiten.KeyAltLeft, ebiten.KeyAltRight
	default:
		return 0
	}
	dl := int64(inpututil.KeyPressDuration(l))
	dr := int64(inpututil.KeyPressDuration(r))
	if dl > dr {
		return dl
	}
	return dr
}

// scanRepKeys returns the first pressed code (and its duration),
// skipping modifier codes unless mods is true.
func scanRepKeys(mods bool) (int, int64) {
	for _, c := range repCodes {
		if isModCode(c) != mods {
			continue
		}
		var d int64
		if mods {
			d = modPressDur(c)
		} else {
			k, ok := keyFor(c)
			if !ok {
				continue
			}
			d = int64(inpututil.KeyPressDuration(k))
		}
		if d > 0 {
			return c, d
		}
	}
	return 0, 0
}

// KeyRepeat returns the firing key code with key-repeat semantics,
// or 0 when nothing fires. A newly pressed (or switched) key fires at
// once; a held key refires after repDelay ticks, then every
// repInterval ticks. Letters fire lowercase (97-122) unless shift is
// held (65-90); shifted digits fire their US-layout symbols.
// Modifiers fire only when pressed alone, so Shift+A reports 65.
// Script-thread safe.
func (b *WindowBackend) KeyRepeat() int {
	code, dur := scanRepKeys(false)
	if code == 0 {
		code, dur = scanRepKeys(true)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	st := &repState{code: b.repCode, dur: b.repDur, next: b.repNext}
	fired := repStep(st, code, dur)
	b.repCode, b.repDur, b.repNext = st.code, st.dur, st.next
	if fired == 0 {
		return 0
	}
	return shiftCode(fired, shiftHeld())
}

// Polling input (G2). Key/mouse queries hit ebiten directly (thread-safe);
// click edges are latched in Update because "just pressed" needs per-tick
// observation.

// keyCodes maps script key codes to ebiten keys, following Windows
// virtual-key numbering: 8 backspace, 9 tab, 13 enter, 16 shift, 17
// ctrl, 18 alt, 27 esc, 32 space, 33-36 pageup/pagedown/end/home,
// 37-40 arrows, 45 insert, 46 delete, 48-57 digits, 65-90 A-Z,
// 112-123 F1-F12, 186-192/219-222 punctuation.
func keyFor(code int) (ebiten.Key, bool) {
	switch code {
	case 8:
		return ebiten.KeyBackspace, true
	case 9:
		return ebiten.KeyTab, true
	case 13:
		return ebiten.KeyEnter, true
	case 16:
		return ebiten.KeyShiftLeft, true
	case 17:
		return ebiten.KeyControlLeft, true
	case 18:
		return ebiten.KeyAltLeft, true
	case 27:
		return ebiten.KeyEscape, true
	case 32:
		return ebiten.KeySpace, true
	case 33:
		return ebiten.KeyPageUp, true
	case 34:
		return ebiten.KeyPageDown, true
	case 35:
		return ebiten.KeyEnd, true
	case 36:
		return ebiten.KeyHome, true
	case 37:
		return ebiten.KeyArrowLeft, true
	case 38:
		return ebiten.KeyArrowUp, true
	case 39:
		return ebiten.KeyArrowRight, true
	case 40:
		return ebiten.KeyArrowDown, true
	case 45:
		return ebiten.KeyInsert, true
	case 46:
		return ebiten.KeyDelete, true
	case 186:
		return ebiten.KeySemicolon, true
	case 187:
		return ebiten.KeyEqual, true
	case 188:
		return ebiten.KeyComma, true
	case 189:
		return ebiten.KeyMinus, true
	case 190:
		return ebiten.KeyPeriod, true
	case 191:
		return ebiten.KeySlash, true
	case 192:
		return ebiten.KeyBackquote, true
	case 219:
		return ebiten.KeyBracketLeft, true
	case 220:
		return ebiten.KeyBackslash, true
	case 221:
		return ebiten.KeyBracketRight, true
	case 222:
		return ebiten.KeyQuote, true
	}
	if code >= 48 && code <= 57 {
		return ebiten.Key0 + ebiten.Key(code-48), true
	}
	if code >= 65 && code <= 90 {
		return ebiten.KeyA + ebiten.Key(code-65), true
	}
	if code >= 112 && code <= 123 {
		return ebiten.KeyF1 + ebiten.Key(code-112), true
	}
	return 0, false
}

// shiftHeld reports either shift key; ctrlHeld / altHeld likewise.
func shiftHeld() bool {
	return ebiten.IsKeyPressed(ebiten.KeyShiftLeft) || ebiten.IsKeyPressed(ebiten.KeyShiftRight)
}

// shiftDigit maps 0-9 to their shifted (US-layout) symbol codes:
// ) ! @ # $ % ^ & * (.
func shiftDigit(code int) int {
	switch code {
	case 48:
		return 41
	case 49:
		return 33
	case 50:
		return 64
	case 51:
		return 35
	case 52:
		return 36
	case 53:
		return 37
	case 54:
		return 94
	case 55:
		return 38
	case 56:
		return 42
	case 57:
		return 40
	}
	return code
}

// shiftCode translates a firing keyrep code for a held shift key:
// letters become uppercase (65-90), digits become symbols, anything
// else passes through. Without shift, letters fire lowercase (97-122).
func shiftCode(code int, shift bool) int {
	if code >= 65 && code <= 90 {
		if !shift {
			return code + 32
		}
		return code
	}
	if code >= 48 && code <= 57 {
		if shift {
			return shiftDigit(code)
		}
		return code
	}
	return code
}

// clickButton maps script button numbers to ebiten buttons.
func clickButton(btn int) (ebiten.MouseButton, bool) {
	switch btn {
	case 0:
		return ebiten.MouseButtonLeft, true
	case 1:
		return ebiten.MouseButtonMiddle, true
	case 2:
		return ebiten.MouseButtonRight, true
	}
	return 0, false
}

// pumpInput latches click rising edges and accumulates wheel motion.
// Called from Update. ebiten.Wheel reports per-tick deltas (0 when
// idle), so plain accumulation never loses motion.
func (b *WindowBackend) pumpInput() {
	pressed := [3]bool{
		ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft),
		ebiten.IsMouseButtonPressed(ebiten.MouseButtonMiddle),
		ebiten.IsMouseButtonPressed(ebiten.MouseButtonRight),
	}
	_, wy := ebiten.Wheel()
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range pressed {
		if pressed[i] && !b.mouseDown[i] {
			b.clickFlag[i] = true
		}
		b.mouseDown[i] = pressed[i]
	}
	b.wheelAccum += wy
}

// MouseWheel returns pending vertical wheel detents since the last
// call (consumed; fractions carry over).
func (b *WindowBackend) MouseWheel() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	v := int(b.wheelAccum)
	b.wheelAccum -= float64(v)
	return v
}

// MousePos returns the cursor position in window pixels.
func (b *WindowBackend) MousePos() (int, int) {
	return ebiten.CursorPosition()
}

// PadCount reports connected gamepads.
func (b *WindowBackend) PadCount() int {
	return len(ebiten.AppendGamepadIDs(nil))
}

// TouchCount reports active touches.
func (b *WindowBackend) TouchCount() int {
	return len(ebiten.AppendTouchIDs(nil))
}

// TouchPos reports the i-th touch position (window pixels).
func (b *WindowBackend) TouchPos(i int) (int, int, error) {
	ids := ebiten.AppendTouchIDs(nil)
	if i < 0 || i >= len(ids) {
		return 0, 0, fmt.Errorf("不明なタッチ %d です（接触数 %d）", i, len(ids))
	}
	x, y := ebiten.TouchPosition(ids[i])
	return x, y, nil
}

// padID resolves a script pad index or reports an error.
func (b *WindowBackend) padID(id int) (ebiten.GamepadID, error) {
	ids := ebiten.AppendGamepadIDs(nil)
	if id < 0 || id >= len(ids) {
		return 0, fmt.Errorf("不明なパッド %d です（接続数 %d）", id, len(ids))
	}
	return ids[id], nil
}

// PadButton reports a standard-layout button (0-16: A,B,X,Y,LB,RB,
// LT,RT,Select,Start,L3,R3,Up,Down,Left,Right,Home).
func (b *WindowBackend) PadButton(id, btn int) (bool, error) {
	if btn < 0 || btn > 16 {
		return false, fmt.Errorf("パッドボタンは 0 から 16 の範囲で指定してください。%d が指定されました", btn)
	}
	gid, err := b.padID(id)
	if err != nil {
		return false, err
	}
	return ebiten.StandardGamepadButtonValue(gid, ebiten.StandardGamepadButton(btn)) > 0.5, nil
}

// PadAxis reports a stick axis 0-3 (left X/Y, right X/Y) in -1..1.
func (b *WindowBackend) PadAxis(id, axis int) (float64, error) {
	if axis < 0 || axis > 3 {
		return 0, fmt.Errorf("パッド軸は 0 から 3 の範囲で指定してください。%d が指定されました", axis)
	}
	gid, err := b.padID(id)
	if err != nil {
		return 0, err
	}
	return ebiten.StandardGamepadAxisValue(gid, ebiten.StandardGamepadAxis(axis)), nil
}

// PadName reports a gamepad name.
func (b *WindowBackend) PadName(id int) (string, error) {
	gid, err := b.padID(id)
	if err != nil {
		return "", err
	}
	return ebiten.GamepadName(gid), nil
}

// Clicked reports a click since the last call (consumed).
// Kept for compatibility; ClickedButton(0) is the same.
func (b *WindowBackend) Clicked() bool {
	c, _ := b.ClickedButton(0)
	return c
}

// ClickedButton reports button 0/1/2 (left/middle/right) since the
// last call (consumed).
func (b *WindowBackend) ClickedButton(btn int) (bool, error) {
	if _, ok := clickButton(btn); !ok {
		return false, fmt.Errorf("マウスボタンは 0 から 2 の範囲で指定してください。%d が指定されました", btn)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.clickFlag[btn]
	b.clickFlag[btn] = false
	return c, nil
}

// CursorVisible reports whether the system cursor is shown.
func (b *WindowBackend) CursorVisible() bool {
	return ebiten.CursorMode() == ebiten.CursorModeVisible
}

// SetCursorVisible shows (true) or hides (false) the system cursor.
func (b *WindowBackend) SetCursorVisible(on bool) {
	if on {
		ebiten.SetCursorMode(ebiten.CursorModeVisible)
	} else {
		ebiten.SetCursorMode(ebiten.CursorModeHidden)
	}
}

// KeyDown reports whether a script key code is held. Modifier codes
// 16/17/18 match either side (left or right).
func (b *WindowBackend) KeyDown(code int) (bool, bool) {
	switch code {
	case 16:
		return shiftHeld(), true
	case 17:
		return ebiten.IsKeyPressed(ebiten.KeyControlLeft) || ebiten.IsKeyPressed(ebiten.KeyControlRight), true
	case 18:
		return ebiten.IsKeyPressed(ebiten.KeyAltLeft) || ebiten.IsKeyPressed(ebiten.KeyAltRight), true
	}
	k, ok := keyFor(code)
	if !ok {
		return false, false
	}
	return ebiten.IsKeyPressed(k), true
}

// Stick builds the direction/action bitmask:
// 1 left, 2 up, 4 right, 8 down, 16 ok (space/Z/enter),
// 32 cancel (esc/X), 64 left click, 128 right click.
func (b *WindowBackend) Stick() int {
	s := 0
	if ebiten.IsKeyPressed(ebiten.KeyArrowLeft) {
		s |= 1
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowUp) {
		s |= 2
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowRight) {
		s |= 4
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowDown) {
		s |= 8
	}
	if ebiten.IsKeyPressed(ebiten.KeySpace) || ebiten.IsKeyPressed(ebiten.KeyZ) || ebiten.IsKeyPressed(ebiten.KeyEnter) {
		s |= 16
	}
	if ebiten.IsKeyPressed(ebiten.KeyEscape) || ebiten.IsKeyPressed(ebiten.KeyX) {
		s |= 32
	}
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		s |= 64
	}
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonRight) {
		s |= 128
	}
	return s
}
