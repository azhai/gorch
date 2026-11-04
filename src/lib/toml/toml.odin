package toml

import "core:strings"

// Kind discriminates a Value.
Kind :: enum { String, Int, Bool, Array, Table }

// Value is a parsed TOML value. Slice/map members are reference types so the
// struct may recursively contain itself.
Value :: struct {
	kind: Kind,
	s:    string,
	i:    i64,
	b:    bool,
	arr:  []Value,
	tbl:  ^Table,
}

Table :: map[string]Value
Error :: struct {
	msg:  string,
	line: int,
}

str_value :: proc(s: string) -> Value { return Value{kind = .String, s = s} }
int_value :: proc(n: i64) -> Value { return Value{kind = .Int, i = n} }
bool_value :: proc(b: bool) -> Value { return Value{kind = .Bool, b = b} }
arr_value :: proc(a: []Value) -> Value { return Value{kind = .Array, arr = a} }
tbl_value :: proc(t: ^Table) -> Value { return Value{kind = .Table, tbl = t} }

new_table :: proc() -> ^Table {
	t := new(Table)
	t^ = make(map[string]Value)
	return t
}

// parse_text parses a TOML subset into a root table. Supported: comments,
// [a.b] headers, key = value, quoted/bare strings with escapes, integers,
// booleans, string arrays, and inline tables. All allocations use the ambient
// context allocator; callers should pass a long-lived arena.
parse_text :: proc(text: string) -> (root: ^Table, err: Error) {
	root = new_table()
	prefix := ""
	i := 0
	line := 1
	n := len(text)

	for i < n {
		if text[i] == '\n' {
			line += 1
			i += 1
			continue
		}
		if text[i] == ' ' || text[i] == '\t' || text[i] == '\r' {
			i += 1
			continue
		}
		if text[i] == '#' {
			for i < n && text[i] != '\n' {
				i += 1
			}
			continue
		}
		if text[i] == '[' {
			i += 1
			start := i
			for i < n && text[i] != ']' {
				i += 1
			}
			if i >= n {
				return nil, Error{"unterminated table header", line}
			}
			prefix = strings.trim_space(text[start:i])
			i += 1
			get_or_make_table(root, prefix)
			continue
		}

		key_start := i
		for i < n && text[i] != '=' && text[i] != '\n' {
			i += 1
		}
		if i >= n || text[i] != '=' {
			return nil, Error{"expected '=' in key/value pair", line}
		}
		key := strings.trim_space(text[key_start:i])
		if len(key) >= 2 && (key[0] == '"' || key[0] == '\'') {
			key = key[1 : len(key)-1]
		}
		i += 1
		for i < n && (text[i] == ' ' || text[i] == '\t') {
			i += 1
		}
		val, e := parse_value(text, &i, &line, n)
		if e.msg != "" {
			return nil, e
		}
		tbl := get_or_make_table(root, prefix)
		tbl^[key] = val
		for i < n && text[i] != '\n' {
			if text[i] == '#' {
				for i < n && text[i] != '\n' {
					i += 1
				}
				break
			}
			i += 1
		}
	}
	return root, Error{}
}

// get_or_make_table walks a dotted path creating tables as needed. Table is a
// reference type (map), so nested mutations propagate to the root.
get_or_make_table :: proc(root: ^Table, path: string) -> ^Table {
	if len(path) == 0 {
		return root
	}
	parts, _ := strings.split(path, ".")
	defer delete(parts)
	cur := root
	for p in parts {
		q := strings.trim_space(p)
		if len(q) == 0 {
			continue
		}
		v, ok := cur^[q]
		if ok && v.kind == .Table {
			cur = v.tbl
		} else {
			nt := new_table()
			cur^[q] = tbl_value(nt)
			cur = nt
		}
	}
	return cur
}

parse_value :: proc(text: string, i: ^int, line: ^int, n: int) -> (Value, Error) {
	if i^ >= n {
		return str_value(""), Error{"empty value", line^}
	}
	c := text[i^]
	if c == '"' || c == '\'' {
		s := parse_string(text, i, line, n)
		return str_value(s), Error{}
	}
	if c == '[' {
		return parse_array(text, i, line, n)
	}
	if c == '{' {
		return parse_inline_table(text, i, line, n)
	}
	start := i^
	for i^ < n && text[i^] != ',' && text[i^] != ']' && text[i^] != '}' &&
		text[i^] != '\n' && text[i^] != '\r' && text[i^] != '#' {
		i^ += 1
	}
	tok := strings.trim_space(text[start:i^])
	if tok == "true" {
		return bool_value(true), Error{}
	}
	if tok == "false" {
		return bool_value(false), Error{}
	}
	if num, ok := parse_int_str(tok); ok {
		return int_value(num), Error{}
	}
	return str_value(tok), Error{}
}

parse_int_str :: proc(s: string) -> (i64, bool) {
	if len(s) == 0 {
		return 0, false
	}
	buf, _ := strings.replace_all(s, "_", "")
	j := 0
	neg := false
	if buf[0] == '-' || buf[0] == '+' {
		neg = buf[0] == '-'
		j = 1
	}
	if j >= len(buf) {
		return 0, false
	}
	v := i64(0)
	for ; j < len(buf); j += 1 {
		if buf[j] < '0' || buf[j] > '9' {
			return 0, false
		}
		v = v * 10 + i64(buf[j]-'0')
	}
	if neg {
		v = -v
	}
	return v, true
}

parse_string :: proc(text: string, i: ^int, line: ^int, n: int) -> string {
	q := text[i^]
	i^ += 1
	b := strings.builder_make()
	for i^ < n {
		ch := text[i^]
		if ch == '\n' {
			line^ += 1
		}
		if ch == q {
			i^ += 1
			break
		}
		if q == '"' && ch == '\\' && i^+1 < n {
			i^ += 1
			e := text[i^]
			if e == 'n' {
				strings.write_rune(&b, '\n')
			} else if e == 't' {
				strings.write_rune(&b, '\t')
			} else if e == 'r' {
				strings.write_rune(&b, '\r')
			} else if e == '"' {
				strings.write_rune(&b, '"')
			} else if e == '\\' {
				strings.write_rune(&b, '\\')
			} else {
				strings.write_byte(&b, e)
			}
			i^ += 1
			continue
		}
		strings.write_byte(&b, ch)
		i^ += 1
	}
	return strings.to_string(b)
}

parse_array :: proc(text: string, i: ^int, line: ^int, n: int) -> (Value, Error) {
	i^ += 1
	out := [dynamic]Value{}
	for i^ < n {
		for i^ < n && (text[i^] == ' ' || text[i^] == '\t' || text[i^] == ',' ||
			text[i^] == '\r' || text[i^] == '\n') {
			if text[i^] == '\n' {
				line^ += 1
			}
			i^ += 1
		}
		if i^ >= n {
			break
		}
		if text[i^] == ']' {
			i^ += 1
			break
		}
		v, e := parse_value(text, i, line, n)
		if e.msg != "" {
			return str_value(""), e
		}
		append(&out, v)
	}
	return arr_value(out[:]), Error{}
}

parse_inline_table :: proc(text: string, i: ^int, line: ^int, n: int) -> (Value, Error) {
	i^ += 1
	tbl := new_table()
	for i^ < n {
		for i^ < n && (text[i^] == ' ' || text[i^] == ',' || text[i^] == '\t') {
			i^ += 1
		}
		if i^ >= n {
			break
		}
		if text[i^] == '}' {
			i^ += 1
			break
		}
		ks := i^
		for i^ < n && text[i^] != '=' && text[i^] != '}' {
			i^ += 1
		}
		key := strings.trim_space(text[ks:i^])
		if i^ < n && text[i^] == '=' {
			i^ += 1
		}
		for i^ < n && (text[i^] == ' ' || text[i^] == '\t') {
			i^ += 1
		}
		v, e := parse_value(text, i, line, n)
		if e.msg != "" {
			return str_value(""), e
		}
		tbl^[key] = v
	}
	return tbl_value(tbl), Error{}
}

// ---- typed accessors ----

get_string :: proc(tbl: ^Table, key: string) -> (string, bool) {
	v, ok := tbl^[key]
	if !ok || v.kind != .String {
		return "", false
	}
	return v.s, true
}

get_int :: proc(tbl: ^Table, key: string) -> (int, bool) {
	v, ok := tbl^[key]
	if !ok || v.kind != .Int {
		return 0, false
	}
	return int(v.i), true
}

get_bool :: proc(tbl: ^Table, key: string) -> (bool, bool) {
	v, ok := tbl^[key]
	if !ok || v.kind != .Bool {
		return false, false
	}
	return v.b, true
}

get_array :: proc(tbl: ^Table, key: string) -> ([]Value, bool) {
	v, ok := tbl^[key]
	if !ok || v.kind != .Array {
		return nil, false
	}
	return v.arr, true
}

get_table :: proc(tbl: ^Table, key: string) -> (^Table, bool) {
	v, ok := tbl^[key]
	if !ok || v.kind != .Table {
		return nil, false
	}
	return v.tbl, true
}
