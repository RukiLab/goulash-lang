#mode cli
a = [[[[[1, 2]]]]]
mes(a[0][0][0][0][1])
a[0][0][0][0][0] = 99
mes(a[0][0][0][0][0])

data = [["Zed", 77]]
mes(data[0][0])
mes(data[0][1])

total = 0
repeat 5 {
    total = total + 1
}
mes(total)

def max(a, b) {
    if a > b {
        return a
    } else {
        return b
    }
}
mes(max(3, 7))
mes(max(9, 2))

f = 2.5
mes(f * 4)
mes(10 == 10.0)
mes(true && !false)
