#mode cli
// enum（連番定数）の作例
enum Color { Red, Green, Blue }
mes(Color_Red)
mes(Color_Green)
mes(Color_Blue)

enum { Low, High = 10, Max }
mes(Low)
mes(High)
mes(Max)

enum Dir {
    North,
    South,
    East,
    West,
}
mes(Dir_West)

d = Dir_South
switch d {
    case Dir_North {
        mes("north")
    }
    case Dir_South {
        mes("south")
    }
    default {
        mes("other")
    }
}

// 字句解析の作例（エディタ支援向け）
t = lextokens("x = 1")
mes(t[0].type)
mes(t[0].text)
