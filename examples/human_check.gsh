// human_check.gsh — 人手検証テスト（GUI ビルド専用）。
//
// 実行: gsh run examples/human_check.gsh --gui
// 目視・操作が必要な項目を順に提示し、dialog() の Yes/No または
// 制限時間付きの自動判定で合否を記録します。最後に集計を表示します。

screen(640, 480)
title("human check")

passed = []
failed = []

def ok(name) {
    push(passed, name)
    mes("PASS: " + name)
}

def ng(name) {
    push(failed, name)
    mes("FAIL: " + name)
}

def ask(name, msg) {
    if dialog(msg, "yesno") == 1 {
        ok(name)
    } else {
        ng(name)
    }
}

// ---- T1: 描画・文字 ----
cls()
color(32, 32, 48)
boxf()
color(255, 255, 255)
boxf(10, 10, 100, 60)
color(255, 80, 80)
line(0, 0, 640, 480)
color(80, 255, 120)
circle(320, 240, 90, 1)
pset(320, 240)
color(255, 255, 255)
pos(16, 40)
mes("draw あいう 012")
ask("T1 draw+text", "図形（四角・対角線・円・点）と文字が正しく見えるか")

// ---- T2: フォント切替 ----
font("meiryo.ttc", 24)
cls()
pos(16, 40)
mes("フォント切替 あいう ABC")
ask("T2 font switch", "書体が游ゴシックから変わって見えるか")
font("YuGothR.ttc", 16)
cls()
pos(16, 40)
mes("16px に戻った")
sleep(800)

// ---- T3: ボタン画像4状態 ----
gsel(1)
color(200, 60, 60)
boxf(0, 0, 120, 40)
gsel(2)
color(60, 200, 60)
boxf(0, 0, 120, 40)
gsel(3)
color(60, 60, 200)
boxf(0, 0, 120, 40)
gsel(0)
cls()
button(10, "", 260, 200, 120, 40, 1, 2, 3)
pos(16, 40)
mes("10秒: 赤=通常、ホバー=緑、押下=青を確認")
sleep(10000)
ask("T3 button states", "通常赤・ホバー緑・押下青に変わったか")
objprm(10, "enable", 0)
sleep(500)
ask("T3 button disabled", "ボタンが減光（無効表示）されたか")
objprm(10, "enable", 1)
clrobj(10)

// ---- T4: ウィジェット表示・読取 ----
cls()
inputbox(11, 20, 60, 300, 36, "abc")
chkbox(12, "同意する", 20, 120, 1)
combox(13, 20, 170, 200, 36, ["red", "green", "blue"], 1)
mesbox(14, 340, 60, 280, 200, "one\ntwo")
if gettext(11) == "abc" {
    ok("T4 inputbox prefill")
} else {
    ng("T4 inputbox prefill")
}
if checked(12) {
    ok("T4 chkbox initial")
} else {
    ng("T4 chkbox initial")
}
if selected(13) == 1 {
    ok("T4 combox initial")
} else {
    ng("T4 combox initial")
}
if getstr(14) == "one\ntwo" {
    ok("T4 mesbox prefill")
} else {
    ng("T4 mesbox prefill")
}
pos(16, 420)
mes("10秒: 入力・チェック外し・選択変更を操作")
sleep(10000)
cls()
mes("memo=" + getstr(14))
mes("agree=" + str(checked(12)))
mes("color=" + str(selected(13)))
ask("T4 widget interactive", "操作が読取に反映されたか")
clrobj()

// ---- T5: ダイアログ ----
if dialog("Yes を押してください", "yesno") == 1 {
    ok("T5 dialog yes")
} else {
    ng("T5 dialog yes")
}
if dialog("No を押してください", "yesno") == 0 {
    ok("T5 dialog no")
} else {
    ng("T5 dialog no")
}

// ---- T6: IME ----
cls()
pos(16, 40)
mes("IME で日本語を入力して Enter")
ime(1)
s = input("にほんご> ")
ime(0)
cls()
mes("入力=" + s)
ask("T6 ime input", "入力した通りに確定されたか")

// ---- T7: 音声 ----
snd = mmload("examples/assets/sfx_move.mp3")
mmplay(snd)
ask("T7 audio", "音が鳴ったか")
mmstop()

// ---- T8: キー自動判定（各5秒） ----
cls()
pos(16, 40)
mes("5秒: 左矢印キーを押す")
t0 = tick()
hit = false
while tick() - t0 < 300 {
    if getkey(37) {
        hit = true
    }
    await()
}
if hit {
    ok("T8 arrow key")
} else {
    ng("T8 arrow key")
}

cls()
pos(16, 40)
mes("5秒: Shift キーを押す")
t0 = tick()
hit = false
while tick() - t0 < 300 {
    if getkey(16) {
        hit = true
    }
    await()
}
if hit {
    ok("T8 shift key")
} else {
    ng("T8 shift key")
}

cls()
pos(16, 40)
mes("離して→5秒: A 単体押し（小文字 97 が出る）")
sleep(1000)
t0 = tick()
code = 0
while tick() - t0 < 300 {
    c = keyrep()
    if c != 0 {
        code = c
    }
    await()
}
mes("code=" + str(code))
if code == 97 {
    ok("T8 keyrep lower")
} else {
    ng("T8 keyrep lower")
}

cls()
pos(16, 40)
mes("離して→5秒: Shift+A（大文字 65 が出る）")
sleep(1000)
t0 = tick()
code = 0
while tick() - t0 < 300 {
    c = keyrep()
    if c != 0 {
        code = c
    }
    await()
}
mes("code=" + str(code))
if code == 65 {
    ok("T8 keyrep upper")
} else {
    ng("T8 keyrep upper")
}

// ---- T9: マウス ----
cls()
pos(16, 40)
mes("5秒: 左クリックする")
t0 = tick()
hit = false
mx = 0
my = 0
while tick() - t0 < 300 {
    if clicked() {
        hit = true
        mx = mousex()
        my = mousey()
    }
    await()
}
mes("click=" + str(mx) + "," + str(my))
if hit {
    ok("T9 mouse click")
} else {
    ng("T9 mouse click")
}

// ---- 集計 ----
cls()
pos(16, 40)
mes("PASSED: " + str(length(passed)))
mes("FAILED: " + str(length(failed)))
repeat failed as f {
    mes("  miss: " + f)
}
dialog("終了", "ok")
