buttonImg = picload("examples/assets/mino.png")
button(0, "", 0, 0, 16, 16, buttonImg)
while true {
	if pressed(0) {
		mes("ボタンが押されました")
	}
}