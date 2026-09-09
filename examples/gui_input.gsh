screen(640, 480)
title("Goulash input demo")

repeat 100000 {
    cls()
    mes("move the mouse, press arrows...")
    mes("click or ESC to exit")
    mes("")
    mes("mouse=" + str(mousex()) + "," + str(mousey()))
    mes("stick=" + str(stick()))
    if clicked() {
        break
    }
    if getkey(27) {
        break
    }
    await()
}

cls()
mes("bye!")
sleep(800)
end(0)
