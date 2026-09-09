#define MINO_SIZE 16
#define COLS 10
#define VISIBLE_ROWS 20
#define BUFFER_ROWS 2
#define ROWS 22  // 0, 1行目が22段目・21段目（画面外）、2〜21行目が画面内20行

#define RIGHT 1
#define LEFT -1
#define DAS 11
#define ARR 3
#define MAX_LOCK_RESETS 15

// ゲーム状態定義
#define STATE_PLAY 0
#define STATE_PAUSE 1
#define STATE_GAMEOVER 2

// ミノの種類
#define PIECE_I 0
#define PIECE_O 1
#define PIECE_T 2
#define PIECE_S 3
#define PIECE_Z 4
#define PIECE_J 5
#define PIECE_L 6

// ガイドライン準拠 落下速度テーブル (Lv 1 〜 Lv 15+) [フレーム数]
speedTable = [60, 50, 42, 34, 27, 21, 16, 12, 9, 6, 5, 4, 3, 2, 1]

// SRS準拠 各ミノの4回転状態 [type][rot][y][x]
pieceShapes = [
    // 0: I
    [
        [[0,0,0,0],[1,1,1,1],[0,0,0,0],[0,0,0,0]],
        [[0,0,1,0],[0,0,1,0],[0,0,1,0],[0,0,1,0]],
        [[0,0,0,0],[0,0,0,0],[1,1,1,1],[0,0,0,0]],
        [[0,1,0,0],[0,1,0,0],[0,1,0,0],[0,1,0,0]]
    ],
    // 1: O
    [
        [[0,1,1,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,1,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,1,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,1,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]]
    ],
    // 2: T
    [
        [[0,1,0,0],[1,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,0,0],[0,1,1,0],[0,1,0,0],[0,0,0,0]],
        [[0,0,0,0],[1,1,1,0],[0,1,0,0],[0,0,0,0]],
        [[0,1,0,0],[1,1,0,0],[0,1,0,0],[0,0,0,0]]
    ],
    // 3: S
    [
        [[0,1,1,0],[1,1,0,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,0,0],[0,1,1,0],[0,0,1,0],[0,0,0,0]],
        [[0,0,0,0],[0,1,1,0],[1,1,0,0],[0,0,0,0]],
        [[1,0,0,0],[1,1,0,0],[0,1,0,0],[0,0,0,0]]
    ],
    // 4: Z
    [
        [[1,1,0,0],[0,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,0,1,0],[0,1,1,0],[0,1,0,0],[0,0,0,0]],
        [[0,0,0,0],[1,1,0,0],[0,1,1,0],[0,0,0,0]],
        [[0,1,0,0],[1,1,0,0],[1,0,0,0],[0,0,0,0]]
    ],
    // 5: J
    [
        [[1,0,0,0],[1,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,1,0],[0,1,0,0],[0,1,0,0],[0,0,0,0]],
        [[0,0,0,0],[1,1,1,0],[0,0,1,0],[0,0,0,0]],
        [[0,1,0,0],[0,1,0,0],[1,1,0,0],[0,0,0,0]]
    ],
    // 6: L
    [
        [[0,0,1,0],[1,1,1,0],[0,0,0,0],[0,0,0,0]],
        [[0,1,0,0],[0,1,0,0],[0,1,1,0],[0,0,0,0]],
        [[0,0,0,0],[1,1,1,0],[1,0,0,0],[0,0,0,0]],
        [[1,1,0,0],[0,1,0,0],[0,1,0,0],[0,0,0,0]]
    ]
]

// SRS ウォールキックデータ
kickJLSTZ = [
    [[0,0], [-1,0], [-1,-1], [0,2], [-1,2]],  // 0 -> 1
    [[0,0], [1,0], [1,1], [0,-2], [1,-2]],    // 1 -> 0
    [[0,0], [1,0], [1,1], [0,-2], [1,-2]],    // 1 -> 2
    [[0,0], [-1,0], [-1,-1], [0,2], [-1,2]],  // 2 -> 1
    [[0,0], [1,0], [1,-1], [0,2], [1,2]],     // 2 -> 3
    [[0,0], [-1,0], [-1,1], [0,-2], [-1,-2]], // 3 -> 2
    [[0,0], [-1,0], [-1,1], [0,-2], [-1,-2]], // 3 -> 0
    [[0,0], [1,0], [1,-1], [0,2], [1,2]]      // 0 -> 3
]

kickI = [
    [[0,0], [-2,0], [1,0], [-2,1], [1,-2]],   // 0 -> 1
    [[0,0], [2,0], [-1,0], [2,-1], [-1,2]],   // 1 -> 0
    [[0,0], [-1,0], [2,0], [-1,-2], [2,1]],   // 2 -> 1
    [[0,0], [1,0], [-2,0], [1,2], [-2,-1]],   // 2 -> 3
    [[0,0], [2,0], [-1,0], [2,-1], [-1,2]],   // 3 -> 2
    [[0,0], [-2,0], [1,0], [-2,1], [1,-2]],   // 3 -> 0
    [[0,0], [1,0], [-2,0], [1,2], [-2,-1]],   // 0 -> 3
    [[0,0], [-1,0], [2,0], [-1,-2], [2,1]]
]

// フィールド
field = dim(ROWS, COLS)
repeat ROWS as r {
    repeat COLS as c {
        field[r][c] = 0
    }
}

// 消去ラインフラグ
clearingLines = dim(ROWS)
repeat ROWS as r {
    clearingLines[r] = 0
}
clearTimer = 0

// 7-Bag 乱数生成器
bag = dim(7)
bagIndex = 7

// ゲーム変数
gameState = STATE_PLAY
score = 0
linesCleared = 0
level = 1
isRunning = true

// ピース情報
piece = {
    "type": 0,
    "rot": 0,
    "col": 3,
    "row": 0,
    "blocks": pieceShapes[0][0]
}

// タイマー・カウンタ
rightDAS = 0
leftDAS = 0
ARRTimer = 0
gravityTimer = 0
gravityInterval = 60
lockDelay = 30
lockResetCount = 0
lowestRow = 0
minoImg = 0
sfxMove = 0

// キーフラグ
wasRightPressed = false
wasLeftPressed = false
wasUpPressed = false
wasDownPressed = false
wasZPressed = false
wasXPressed = false
wasSpacePressed = false
wasEscPressed = false

def getKickId(fromRot, toRot) {
    if fromRot == 0 && toRot == 1 { return 0 }
    if fromRot == 1 && toRot == 0 { return 1 }
    if fromRot == 1 && toRot == 2 { return 2 }
    if fromRot == 2 && toRot == 1 { return 3 }
    if fromRot == 2 && toRot == 3 { return 4 }
    if fromRot == 3 && toRot == 2 { return 5 }
    if fromRot == 3 && toRot == 0 { return 6 }
    if fromRot == 0 && toRot == 3 { return 7 }
    return 0
}

def getNextPieceType() {
    if bagIndex >= 7 {
        repeat 7 as i {
            bag[i] = i
        }
        repeat 7 as i {
            swapIdx = rnd(7 - i) + i
            tmp = bag[i]
            bag[i] = bag[swapIdx]
            bag[swapIdx] = tmp
        }
        bagIndex = 0
    }
    nextType = bag[bagIndex]
    bagIndex += 1
    return nextType
}

def updateSpeed() {
    idx = level - 1
    if idx > 14 {
        idx = 14
    }
    gravityInterval = speedTable[idx]
}

def initGame() {
    repeat ROWS as r {
        clearingLines[r] = 0
        repeat COLS as c {
            field[r][c] = 0
        }
    }
    clearTimer = 0
    score = 0
    linesCleared = 0
    level = 1
    updateSpeed()
    gravityTimer = 0
    bagIndex = 7
    gameState = STATE_PLAY
    spawnPiece()
}

// 21段目・22段目へのスポーン処理
def spawnPiece() {
    newType = getNextPieceType()
    piece = {
        "type": newType,
        "rot": 0,
        "col": 3,
        "row": 0, // row 0: 22段目, row 1: 21段目
        "blocks": pieceShapes[newType][0]
    }
    gravityTimer = 0
    lockDelay = 30
    lockResetCount = 0

    // Block Out: スポーン位置に置けない場合はゲームオーバー
    if canPut(piece.blocks, piece.col, piece.row) == false {
        gameState = STATE_GAMEOVER
        return 0
    }

    // ガイドライン仕様: 生成直後、障害物がなければ即座に1マス落下して視界へ入る
    if canMove(0, 1) {
        piece.row += 1
    }

    lowestRow = piece.row
}

def canPut(blocks, c, r) {
    repeat 4 as y {
        repeat 4 as x {
            if blocks[y][x] == 1 {
                targetCol = c + x
                targetRow = r + y
                if (targetCol < 0) || (COLS <= targetCol) {
                    return false
                }
                if (targetRow < 0) || (ROWS <= targetRow) {
                    return false
                }
                if field[targetRow][targetCol] != 0 {
                    return false
                }
            }
        }
    }
    return true
}

def canMove(dx, dy) {
    return canPut(piece.blocks, piece.col + dx, piece.row + dy)
}

def notifyLockReset() {
    if canMove(0, 1) == false {
        if lockResetCount < MAX_LOCK_RESETS {
            lockDelay = 30
            lockResetCount += 1
        }
    }
}

def dropPiece() {
    piece.row += 1
    if piece.row > lowestRow {
        lowestRow = piece.row
        lockResetCount = 0
    }
}

def softDrop() {
    if canMove(0, 1) {
        dropPiece()
        score += 1
        lockDelay = 30
    }
}

def hardDrop() {
    dropDist = 0
    while canMove(0, 1) {
        dropPiece()
        dropDist += 1
    }
    score += dropDist * 2
    lockPiece()
}

def rotate(dir) {
    if piece.type == PIECE_O {
        return 0
    }

    oldRot = piece.rot
    newRot = (oldRot + dir + 4) % 4
    nextBlocks = pieceShapes[piece.type][newRot]
    kickId = getKickId(oldRot, newRot)

    repeat 5 as i {
        testDx = 0
        testDy = 0
        if piece.type == PIECE_I {
            testDx = kickI[kickId][i][0]
            testDy = kickI[kickId][i][1]
        } else {
            testDx = kickJLSTZ[kickId][i][0]
            testDy = kickJLSTZ[kickId][i][1]
        }

        if canPut(nextBlocks, piece.col + testDx, piece.row + testDy) {
            piece.col += testDx
            piece.row += testDy
            piece.rot = newRot
            piece.blocks = nextBlocks
            notifyLockReset()
            break
        }
    }
}

def lockPiece() {
    // Lock Out 判定: 全ブロックが画面外（21段目以上: r < BUFFER_ROWS）で固定されたらゲームオーバー
    allAbove = true
    repeat 4 as y {
        repeat 4 as x {
            if piece.blocks[y][x] == 1 {
                if piece.row + y >= BUFFER_ROWS {
                    allAbove = false
                }
            }
        }
    }
    if allAbove {
        gameState = STATE_GAMEOVER
        return 0
    }

    fixpiece()
    
    // 揃っているラインの検出
    hasLine = false
    repeat ROWS as r {
        clearingLines[r] = 0
        isFull = true
        repeat COLS as c {
            if field[r][c] == 0 {
                isFull = false
                break
            }
        }
        if isFull {
            clearingLines[r] = 1
            hasLine = true
        }
    }

    if hasLine {
        clearTimer = 10
    } else {
        spawnPiece()
    }
}

def fixpiece() {
    repeat 4 as y {
        repeat 4 as x {
            if piece.blocks[y][x] == 1 {
                targetY = piece.row + y
                targetX = piece.col + x
                if targetY >= 0 && targetY < ROWS && targetX >= 0 && targetX < COLS {
                    field[targetY][targetX] = 1
                }
            }
        }
    }
}

def finishLineClear() {
    clearedCount = 0
    r = ROWS - 1
    while r >= 0 {
        if clearingLines[r] == 1 {
            clearedCount += 1
            shiftR = r
            while shiftR > 0 {
                repeat COLS as c {
                    field[shiftR][c] = field[shiftR - 1][c]
                }
                clearingLines[shiftR] = clearingLines[shiftR - 1]
                shiftR -= 1
            }
            repeat COLS as c {
                field[0][c] = 0
            }
            clearingLines[0] = 0
        } else {
            r -= 1
        }
    }

    // 本家スコアリング (基本点 × 現在のレベル)
    if clearedCount == 1 { score += 100 * level }
    if clearedCount == 2 { score += 300 * level }
    if clearedCount == 3 { score += 500 * level }
    if clearedCount == 4 { score += 800 * level }
    
    linesCleared += clearedCount

    // 10ライン消去ごとにレベルアップ＆落下速度更新
    newLevel = (linesCleared / 10) + 1
    if newLevel != level {
        level = newLevel
        updateSpeed()
    }

    repeat ROWS as i {
        clearingLines[i] = 0
    }

    spawnPiece()
}

def handleInput() {
    // ESCキー (ポーズ / 終了)
    if getkey(27) {
        if wasEscPressed == false {
            if gameState == STATE_PLAY {
                gameState = STATE_PAUSE
            } else {
                if gameState == STATE_PAUSE {
                    gameState = STATE_PLAY
                } else {
                    if gameState == STATE_GAMEOVER {
                        isRunning = false
                    }
                }
            }
        }
        wasEscPressed = true
    } else {
        wasEscPressed = false
    }

    // ゲームオーバー画面操作
    if gameState == STATE_GAMEOVER {
        if getkey(82) { // Rキー
            initGame()
        }
        if getkey(81) { // Qキー
            isRunning = false
        }
        return 0
    }

    if gameState == STATE_PAUSE || clearTimer > 0 {
        return 0
    }

    // 右矢印キー
    if getkey(39) {
        if wasRightPressed == false {
            if canMove(1, 0) {
                piece.col += 1
                notifyLockReset()
                mmplay(sfxMove)
            }
            rightDAS = 0
        } else {
            rightDAS += 1
            if rightDAS >= DAS && (rightDAS - DAS) % ARR == 0 {
                if canMove(1, 0) {
                    piece.col += 1
                    notifyLockReset()
                    mmplay(sfxMove)
                }
            }
        }
        wasRightPressed = true
    } else {
        wasRightPressed = false
        rightDAS = 0
    }

    // 左矢印キー
    if getkey(37) {
        if wasLeftPressed == false {
            if canMove(-1, 0) {
                piece.col -= 1
                notifyLockReset()
                mmplay(sfxMove)
            }
            leftDAS = 0
        } else {
            leftDAS += 1
            if leftDAS >= DAS && (leftDAS - DAS) % ARR == 0 {
                if canMove(-1, 0) {
                    piece.col -= 1
                    notifyLockReset()
                    mmplay(sfxMove)
                }
            }
        }
        wasLeftPressed = true
    } else {
        wasLeftPressed = false
        leftDAS = 0
    }

    // 上矢印キー (右回転)
    if getkey(38) {
        if wasUpPressed == false {
            rotate(RIGHT)
        }
        wasUpPressed = true
    } else {
        wasUpPressed = false
    }

    // 下矢印キー (ソフトドロップ)
    if getkey(40) {
        ARRTimer += 1
        if ARRTimer >= ARR {
            ARRTimer = 0
            softDrop()
        }
        wasDownPressed = true
    } else {
        wasDownPressed = false
    }

    // Zキー (左回転)
    if getkey(90) {
        if wasZPressed == false {
            rotate(LEFT)
        }
        wasZPressed = true
    } else {
        wasZPressed = false
    }

    // Xキー (右回転)
    if getkey(88) {
        if wasXPressed == false {
            rotate(RIGHT)
        }
        wasXPressed = true
    } else {
        wasXPressed = false
    }

    // スペースキー (ハードドロップ)
    if getkey(32) {
        if wasSpacePressed == false {
            hardDrop()
            mmplay(sfxHardDrop)
        }
        wasSpacePressed = true
    } else {
        wasSpacePressed = false
    }
}

def update() {
    if gameState != STATE_PLAY {
        return 0
    }

    if clearTimer > 0 {
        clearTimer -= 1
        if clearTimer == 0 {
            finishLineClear()
        }
        return 0
    }

    // 接地中のロック猶予カウント
    if canMove(0, 1) == false {
        lockDelay -= 1
        if lockDelay <= 0 {
            lockPiece()
            return 0
        }
    }

    // 通常落下
    gravityTimer += 1
    if gravityTimer >= gravityInterval {
        gravityTimer = 0
        if canMove(0, 1) {
            dropPiece()
        }
    }
}

// 画面内（r >= BUFFER_ROWS）のみ描画
def drawBlock(c, r) {
    if r < BUFFER_ROWS {
        return 0
    }
    px = c * MINO_SIZE
    py = (r - BUFFER_ROWS) * MINO_SIZE
    pos(px, py)
    gcopy(minoImg, 0, 0, MINO_SIZE, MINO_SIZE)
}

def drawField() {
    repeat ROWS as r {
        if r >= BUFFER_ROWS {
            if clearTimer > 0 && clearingLines[r] == 1 {
                color(255, 255, 255)
                boxf(0, (r - BUFFER_ROWS) * MINO_SIZE, COLS * MINO_SIZE, (r - BUFFER_ROWS + 1) * MINO_SIZE)
            } else {
                repeat COLS as c {
                    if field[r][c] != 0 {
                        drawBlock(c, r)
                    }
                }
            }
        }
    }
}

def drawPiece() {
    repeat 4 as y {
        repeat 4 as x {
            if piece.blocks[y][x] == 1 {
                drawBlock(piece.col + x, piece.row + y)
            }
        }
    }
}

def draw() {
    cls()
    drawField()

    if gameState == STATE_PLAY && clearTimer == 0 {
        drawPiece()
    }

    // 常時画面最上部にステータスを小さく表示
    color(255, 255, 255)
    pos(4, 2)
    mes("Lv:" + level + " Sc:" + score)

    // ポーズ表示
    if gameState == STATE_PAUSE {
        color(255, 255, 255)
        pos(36, 120)
        mes("== PAUSED ==")
        pos(20, 150)
        mes("Press ESC to Resume")
    }

    // ゲームオーバー表示
    if gameState == STATE_GAMEOVER {
        color(255, 60, 60)
        pos(38, 80)
        mes("GAME OVER")

        color(255, 255, 255)
        pos(32, 110)
        mes("Level: " + level)
        pos(32, 130)
        mes("Score: " + score)
        pos(32, 150)
        mes("Lines: " + linesCleared)

        pos(32, 190)
        mes("[R] : Retry")
        pos(32, 210)
        mes("[Q] : Quit")
    }
}

def main() {
    // 画面サイズは拡張せず、元の 10×20 マス（160×320）を維持
    screen(MINO_SIZE * COLS, MINO_SIZE * VISIBLE_ROWS)
    title("Tetris")
    minoImg = picload("examples/assets/mino.png")
    sfxMove = mmload("examples/assets/sfx_move.mp3")
    initGame()

    while isRunning {
        handleInput()
        update()
        draw()
        await(1)
    }
}

main()
