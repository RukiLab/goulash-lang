// Builtin function showcase (HSP-derived, all functions).
randomize(1234)

mes("1d6 x5:")
repeat 5 as i {
    mes("  " + str(rnd(6) + 1))
}

s = "  Hello, HSP World!  "
mes("len=" + str(strlen(s)))
mes("mid=" + strmid(s, 2, 5))
mes("pos=" + str(instr(s, "HSP")))
mes("trim=[" + strtrim(s) + "]")
words = split("a,b,c", ",")
mes("split:", words, "n=" + str(length(words)))

mes(strf("%04d", 42))
mes("pi=" + str(3.14159))
mes("abs=" + str(abs(-7)) + " sqrt=" + str(sqrt(2)))
mes("clamp=" + str(limit(99, 0, 10)))

mes("path base=" + getpath("C:\\work\\app.gsh", 9))
mes("path ext=" + getpath("C:\\work\\app.gsh", 2))
mes("year=" + str(gettime(0)) + " month=" + str(gettime(1)))

t = "one\ntwo\nthree"
mes("lines=" + str(notemax(t)) + " line2=" + noteget(t, 1))
mes(noteadd(t, 3, "four"))

buf = [0, 0, 0, 0]
poke(buf, 0, 65)
wpoke(buf, 1, 0 + 66 + 67 * 256)
mes("peek=" + str(peek(buf, 0)) + " wpeek=" + str(wpeek(buf, 1)))
mes("types:", vartype(1), vartype("s"), vartype(buf))

stack = []
push(stack, 1, 2, 3)
mes("stack:", join(stack, "-"), "pop=" + str(pop(stack)))
m = {"name": "Alice", "score": 98}
mes("user:", m["name"], m["score"])
total = 0
repeat [1, 2, 3] as x {
    total += x
}
mes("total=" + str(total))
try {
    mes(1 / 0)
} catch(e) {
    mes("caught!")
}

sleep(10)
mes("done")
