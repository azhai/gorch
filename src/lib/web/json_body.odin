package web

// The config form's request bodies.
//
// `Config_Body` is the shape the SPA posts: a service config with the TOML
// names as JSON keys. It is read member by member and written field by field,
// because the alternative - reflection - is what `lib/json` exists to avoid.
// Unknown members are skipped, so the frontend can add a field without
// breaking an older daemon.
//
// The parsed strings end up in the supervisor's service table, which outlives
// the request, so these bodies are read with the default allocator rather than
// from the connection arena.

import "base:runtime"
import "core:strings"

import "../../lib/json"

// json_read_config fills the members it knows and skips the rest. Numbers are
// left at zero when absent, which is what the form does when a field is
// cleared.
json_read_config :: proc(c: ^json.Cursor, body: ^Config_Body) -> bool {
	if !json.object_begin(c) {
		return false
	}
	first := true
	for {
		name, more := json.key(c, &first)
		if !more {
			break
		}
		ok := true
		switch name {
		case "WORK_DIR":
			body.WORK_DIR, ok = json.read_string(c)
		case "EXEC_CMD":
			body.EXEC_CMD, ok = json.read_string(c)
		case "RESTART_CMD":
			body.RESTART_CMD, ok = json.read_string(c)
		case "RESTART_POLICY":
			body.RESTART_POLICY, ok = json.read_string(c)
		case "BACK_OFF":
			body.BACK_OFF, ok = json.read_int(c)
		case "CHECK_PORT":
			body.CHECK_PORT, ok = json.read_int(c)
		case "PRE_ACTION":
			body.PRE_ACTION, ok = json.read_string(c)
		case "STDOUT":
			body.STDOUT, ok = json.read_string(c)
		case "STDERR":
			body.STDERR, ok = json.read_string(c)
		case "DEPENDS_ON":
			body.DEPENDS_ON, ok = json.read_string_array(c)
		case "CRON":
			body.CRON, ok = json.read_string(c)
		case "CRON_TIMEOUT":
			body.CRON_TIMEOUT, ok = json.read_int(c)
		case "ENV_VARS":
			body.ENV_VARS, ok = json.read_string_map(c)
		case "PID_FILE":
			body.PID_FILE, ok = json.read_string(c)
		case:
			ok = json.skip(c)
		}
		if !ok {
			return false
		}
	}
	return len(c.err) == 0
}

// json_parse_config parses a `Config_Body` and returns the message the form
// shows when the body is not what it claims to be.
json_parse_config :: proc(src: string, allocator: runtime.Allocator) -> (Config_Body, string) {
	c := json.owning_cursor(src, allocator)
	body: Config_Body
	if !json_read_config(&c, &body) {
		return {}, json.error_message(&c)
	}
	return body, ""
}

// json_parse_create parses `{"name":..., "svc":{...}}`.
json_parse_create :: proc(src: string, allocator: runtime.Allocator) -> (Create_Body, string) {
	c := json.owning_cursor(src, allocator)
	body: Create_Body
	if !json.object_begin(&c) {
		return {}, json.error_message(&c)
	}
	first := true
	for {
		name, more := json.key(&c, &first)
		if !more {
			break
		}
		ok: bool
		switch name {
		case "name":
			body.name, ok = json.read_string(&c)
		case "svc":
			ok = json_read_config(&c, &body.svc)
		case:
			ok = json.skip(&c)
		}
		if !ok {
			return {}, json.error_message(&c)
		}
	}
	if len(c.err) > 0 {
		return {}, c.err
	}
	return body, ""
}

// json_parse_expression parses `{"expression":"..."}`. It runs on every
// keystroke in the cron field, so it reads from the request arena: the value
// is parsed into a schedule immediately and never kept.
json_parse_expression :: proc(src: string) -> (string, string) {
	c := json.cursor(src)
	expression: string
	if !json.object_begin(&c) {
		return "", json.error_message(&c)
	}
	first := true
	for {
		name, more := json.key(&c, &first)
		if !more {
			break
		}
		ok: bool
		switch name {
		case "expression":
			expression, ok = json.read_string(&c)
		case:
			ok = json.skip(&c)
		}
		if !ok {
			return "", json.error_message(&c)
		}
	}
	if len(c.err) > 0 {
		return "", c.err
	}
	return expression, ""
}

// write_config_body mirrors `Config_Body` field for field, in declaration
// order, with the keys the form reads back.
write_config_body :: proc(b: ^strings.Builder, c: Config_Body) {
	strings.write_string(b, `{"WORK_DIR":`)
	json.write_string(b, c.WORK_DIR)
	strings.write_string(b, `,"EXEC_CMD":`)
	json.write_string(b, c.EXEC_CMD)
	strings.write_string(b, `,"RESTART_CMD":`)
	json.write_string(b, c.RESTART_CMD)
	strings.write_string(b, `,"RESTART_POLICY":`)
	json.write_string(b, c.RESTART_POLICY)
	strings.write_string(b, `,"BACK_OFF":`)
	json.write_int(b, c.BACK_OFF)
	strings.write_string(b, `,"CHECK_PORT":`)
	json.write_int(b, c.CHECK_PORT)
	strings.write_string(b, `,"PRE_ACTION":`)
	json.write_string(b, c.PRE_ACTION)
	strings.write_string(b, `,"STDOUT":`)
	json.write_string(b, c.STDOUT)
	strings.write_string(b, `,"STDERR":`)
	json.write_string(b, c.STDERR)
	strings.write_string(b, `,"DEPENDS_ON":[`)
	for name, i in c.DEPENDS_ON {
		if i > 0 {
			strings.write_byte(b, ',')
		}
		json.write_string(b, name)
	}
	strings.write_string(b, `],"CRON":`)
	json.write_string(b, c.CRON)
	strings.write_string(b, `,"CRON_TIMEOUT":`)
	json.write_int(b, c.CRON_TIMEOUT)
	strings.write_string(b, `,"ENV_VARS":{`)
	separator := false
	for key, value in c.ENV_VARS {
		if separator {
			strings.write_byte(b, ',')
		}
		separator = true
		json.write_string(b, key)
		strings.write_byte(b, ':')
		json.write_string(b, value)
	}
	strings.write_string(b, `},"PID_FILE":`)
	json.write_string(b, c.PID_FILE)
	strings.write_byte(b, '}')
}
