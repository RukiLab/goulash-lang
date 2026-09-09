// Map builtins: inspection and deletion (creation is the {"k": v}
// literal; writes go through m["k"] = v).
package main

func needMap(name string, args []Value, i int, at Pos) (*MapVal, error) {
	if args[i].K != KMap {
		return nil, argErr(name, i, at, "map である必要があります。%s が指定されました", typeNameOf(args[i]))
	}
	return args[i].Mp, nil
}

func init() {
	// keys(m): key names in insertion order.
	register("keys", 1, 1, func(in *Interp, args []Value, at Pos) (Value, error) {
		m, err := needMap("keys", args, 0, at)
		if err != nil {
			return Null(), err
		}
		elems := make([]Value, len(m.Order))
		for i, k := range m.Order {
			elems[i] = Str(k)
		}
		return ArrayOf(elems), nil
	})

	// has(m, key): whether the key exists.
	register("has", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		m, err := needMap("has", args, 0, at)
		if err != nil {
			return Null(), err
		}
		key, err := needString("has", args, 1, at)
		if err != nil {
			return Null(), err
		}
		_, ok := m.Fields[key]
		return Bool(ok), nil
	})

	// del(m, key): remove a key (missing keys are a silent no-op).
	register("del", 2, 2, func(in *Interp, args []Value, at Pos) (Value, error) {
		m, err := needMap("del", args, 0, at)
		if err != nil {
			return Null(), err
		}
		key, err := needString("del", args, 1, at)
		if err != nil {
			return Null(), err
		}
		if _, ok := m.Fields[key]; ok {
			delete(m.Fields, key)
			for i, k := range m.Order {
				if k == key {
					m.Order = append(m.Order[:i], m.Order[i+1:]...)
					break
				}
			}
		}
		return Null(), nil
	})
}
