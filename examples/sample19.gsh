// HSP後継言語 v0.1 サンプル (§19)
def greet(p) {
    mes("Hello, " + p.name)
}

people = [
    {
        "name": "Alice",
        "age": 20,
    },
    {
        "name": "Bob",
        "age": 25,
    },
]

repeat 2 as i {
    greet(people[i])
}

x = 15

if x % 2 == 0 {
    mes("even")
} else {
    mes("odd")
}
