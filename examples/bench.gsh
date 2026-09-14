#mode cli
mes("計測中...")
let i = 0
let start_time = gettime(5) * 60 + gettime(6) + float(gettime(7)) / 1000
repeat 10000000 {
    i += 1
}
let end_time = gettime(5) * 60 + gettime(6) + float(gettime(7)) / 1000
let time = end_time - start_time
mes(time)
