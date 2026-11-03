package web

// The config page's API.
//
// The SPA posts the form back with the TOML names as JSON keys, so a service
// config is mirrored into `Config_Body` with explicit json tags rather than
// being renamed somewhere in the UI. Anything the daemon cannot honour is
// rejected here with the message the form shows in its banner.

import "base:runtime"
import "core:encoding/json"
import "core:fmt"
import "core:net"
import "core:strings"

import "../../lib/config"
import "../../lib/cron"

Config_Body :: struct {
	WORK_DIR:       string            `json:"WORK_DIR"`,
	EXEC_CMD:       string            `json:"EXEC_CMD"`,
	RESTART_CMD:    string            `json:"RESTART_CMD"`,
	RESTART_POLICY: string            `json:"RESTART_POLICY"`,
	BACK_OFF:       int               `json:"BACK_OFF"`,
	CHECK_PORT:     int               `json:"CHECK_PORT"`,
	PRE_ACTION:     string            `json:"PRE_ACTION"`,
	STDOUT:         string            `json:"STDOUT"`,
	STDERR:         string            `json:"STDERR"`,
	DEPENDS_ON:     []string          `json:"DEPENDS_ON"`,
	CRON:           string            `json:"CRON"`,
	CRON_TIMEOUT:   int               `json:"CRON_TIMEOUT"`,
	ENV_VARS:       map[string]string `json:"ENV_VARS"`,
	PID_FILE:       string            `json:"PID_FILE"`,
}

Create_Body :: struct {
	name: string      `json:"name"`,
	svc:  Config_Body `json:"svc"`,
}

body_of :: proc(spec: config.ServiceConfig) -> Config_Body {
	return Config_Body {
		WORK_DIR = spec.work_dir,
		EXEC_CMD = spec.exec_cmd,
		RESTART_CMD = spec.restart_cmd,
		RESTART_POLICY = spec.restart_policy,
		BACK_OFF = spec.back_off,
		CHECK_PORT = spec.check_port,
		PRE_ACTION = spec.pre_action,
		STDOUT = spec.stdout,
		STDERR = spec.stderr,
		DEPENDS_ON = spec.depends_on,
		CRON = spec.cron,
		CRON_TIMEOUT = spec.cron_timeout,
		ENV_VARS = spec.env_vars,
		PID_FILE = spec.pid_file,
	}
}

spec_of :: proc(b: Config_Body) -> config.ServiceConfig {
	policy := b.RESTART_POLICY
	if len(policy) == 0 {
		policy = "never"
	}
	return config.ServiceConfig {
		work_dir = b.WORK_DIR,
		exec_cmd = b.EXEC_CMD,
		restart_cmd = b.RESTART_CMD,
		restart_policy = policy,
		back_off = b.BACK_OFF,
		check_port = b.CHECK_PORT,
		pre_action = b.PRE_ACTION,
		stdout = b.STDOUT,
		stderr = b.STDERR,
		depends_on = b.DEPENDS_ON,
		cron = b.CRON,
		cron_timeout = b.CRON_TIMEOUT,
		env_vars = b.ENV_VARS,
		pid_file = b.PID_FILE,
	}
}

// request_body returns what follows the header block. The connection reader has
// already waited for Content-Length, so the whole body is present.
request_body :: proc(raw: string) -> string {
	if idx := strings.index(raw, "\r\n\r\n"); idx >= 0 {
		return raw[idx + 4:]
	}
	return ""
}

send_body :: proc(conn: net.TCP_Socket, code: int, b: Config_Body) {
	data, err := json.marshal(b, {}, context.temp_allocator)
	if err != nil {
		send_json_error(conn, 500, fmt.tprintf("cannot encode config: {}", err))
		return
	}
	send_json_ok(conn, code, string(data))
}

// ---- handlers ----

handle_service_config :: proc(
	srv: ^Server,
	conn: net.TCP_Socket,
	method, name, raw: string,
) {
	switch method {
	case "GET":
		spec, ok := current_spec(srv, name)
		if !ok {
			send_json_error(conn, 404, "service not found")
			return
		}
		send_body(conn, 200, body_of(spec))

	case "PUT":
		body: Config_Body
		if err := json.unmarshal_string(request_body(raw), &body, {}, runtime.default_allocator()); err != nil {
			send_json_error(conn, 400, fmt.tprintf("invalid JSON: {}", err))
			return
		}
		if _, ok := current_spec(srv, name); !ok {
			send_json_error(conn, 404, "service not found")
			return
		}
		spec := spec_of(body)
		if len(spec.exec_cmd) == 0 && len(spec.cron) == 0 {
			send_json_error(conn, 400, "EXEC_CMD is required unless CRON is set")
			return
		}
		if !config.is_valid_restart_policy(spec.restart_policy) {
			send_json_error(conn, 400, fmt.tprintf("invalid RESTART_POLICY: {}", spec.restart_policy))
			return
		}
		if err := apply_spec(srv, name, spec); len(err) > 0 {
			send_json_error(conn, 400, err)
			return
		}
		send_json_ok(conn, 200, fmt.tprintf(`{{"name":{},"applied":true}}`, quoted(name)))

	case:
		send_json_error(conn, 405, "method not allowed")
	}
}

handle_create_service :: proc(srv: ^Server, conn: net.TCP_Socket, raw: string) {
	body: Create_Body
	if err := json.unmarshal_string(request_body(raw), &body, {}, runtime.default_allocator()); err != nil {
		send_json_error(conn, 400, fmt.tprintf("invalid JSON: {}", err))
		return
	}
	name := strings.trim_space(body.name)
	if len(name) == 0 {
		send_json_error(conn, 400, "service name is required")
		return
	}
	if _, exists := current_spec(srv, name); exists {
		send_json_error(conn, 409, fmt.tprintf("service already exists: {}", name))
		return
	}
	if err := add_service(srv, name, spec_of(body.svc)); len(err) > 0 {
		send_json_error(conn, 400, err)
		return
	}
	send_json_ok(conn, 201, fmt.tprintf(`{{"name":{}}}`, quoted(name)))
}

handle_delete_service :: proc(srv: ^Server, conn: net.TCP_Socket, name: string) {
	if err := remove_service(srv, name); len(err) > 0 {
		send_json_error(conn, 404, err)
		return
	}
	send_json_ok(conn, 200, fmt.tprintf(`{{"name":{},"deleted":true}}`, quoted(name)))
}

handle_save_config :: proc(srv: ^Server, conn: net.TCP_Socket) {
	if srv.provider.save != nil {
		if err := srv.provider.save(srv.provider.user); len(err) > 0 {
			send_json_error(conn, 400, err)
			return
		}
	} else if err := config.save_config(srv.cfg); len(err) > 0 {
		send_json_error(conn, 400, err)
		return
	}
	send_json_ok(conn, 200, `{"saved":true}`)
}

handle_cron_validate :: proc(srv: ^Server, conn: net.TCP_Socket, raw: string) {
	body: struct {
		expression: string `json:"expression"`,
	}
	if err := json.unmarshal_string(request_body(raw), &body, {}, runtime.default_allocator()); err != nil {
		send_json_error(conn, 400, fmt.tprintf("invalid JSON: {}", err))
		return
	}
	if srv.provider.validate_cron == nil {
		send_json_error(conn, 503, "supervisor is not running")
		return
	}
	check := srv.provider.validate_cron(srv.provider.user, body.expression)
	defer delete(check.next)
	b := strings.builder_make()
	fmt.sbprintf(&b, `{{"valid":{}`, check.valid)
	if !check.valid {
		strings.write_string(&b, `,"message":`)
		write_json_string(&b, check.message)
	} else {
		if len(check.next) > 0 {
			strings.write_string(&b, `,"nextRun":`)
			write_json_string(&b, cron.format_time(check.next[0]))
		}
		if len(check.next) > 1 {
			strings.write_string(&b, `,"nextRun2":`)
			write_json_string(&b, cron.format_time(check.next[1]))
		}
	}
	strings.write_byte(&b, '}')
	send_json_ok(conn, 200, strings.to_string(b))
}

// handle_cron_history answers the "recent runs" list of a scheduled service.
// The entries come from the scheduler rather than from the config, so a service
// that has never fired answers with an empty list: "no runs yet" is the truth,
// not an error.
handle_cron_history :: proc(srv: ^Server, conn: net.TCP_Socket, name: string) {
	if len(name) == 0 {
		send_json_error(conn, 404, "not found")
		return
	}
	if _, ok := current_spec(srv, name); !ok {
		send_json_error(conn, 404, "service not found")
		return
	}
	runs: []Cron_Run
	if srv.provider.cron_history != nil {
		runs = srv.provider.cron_history(srv.provider.user, name)
	}

	// Oldest first, as recorded. The panel reverses it for display so the
	// newest run is on top.
	b := strings.builder_make()
	strings.write_byte(&b, '[')
	for r, i in runs {
		if i > 0 {
			strings.write_byte(&b, ',')
		}
		fmt.sbprintf(&b, `{{"startedAt":{}`, r.started_at)
		if r.has_ended {
			fmt.sbprintf(&b, `,"endedAt":{}`, r.ended_at)
		}
		if r.has_exit {
			fmt.sbprintf(&b, `,"exitCode":{}`, r.exit_code)
		}
		fmt.sbprintf(&b, `,"pid":{},"status":`, r.pid)
		write_json_string(&b, r.status)
		strings.write_byte(&b, '}')
	}
	strings.write_byte(&b, ']')
	send_json_ok(conn, 200, strings.to_string(b))
}

// ---- provider indirection ----

quoted :: proc(s: string) -> string {
	b := strings.builder_make(context.temp_allocator)
	write_json_string(&b, s)
	return strings.to_string(b)
}

// current_spec reads through the supervisor when there is one, so the caller
// sees the config that is actually in force rather than the file on disk.
current_spec :: proc(srv: ^Server, name: string) -> (config.ServiceConfig, bool) {
	if srv.provider.config_get != nil {
		return srv.provider.config_get(srv.provider.user, name)
	}
	spec, ok := srv.cfg.services[name]
	return spec, ok
}

apply_spec :: proc(srv: ^Server, name: string, spec: config.ServiceConfig) -> string {
	if srv.provider.config_set != nil {
		return srv.provider.config_set(srv.provider.user, name, spec)
	}
	srv.cfg.services[name] = spec
	return ""
}

add_service :: proc(srv: ^Server, name: string, spec: config.ServiceConfig) -> string {
	if srv.provider.add != nil {
		return srv.provider.add(srv.provider.user, name, spec)
	}
	srv.cfg.services[name] = spec
	return ""
}

remove_service :: proc(srv: ^Server, name: string) -> string {
	if srv.provider.remove != nil {
		return srv.provider.remove(srv.provider.user, name)
	}
	if _, ok := srv.cfg.services[name]; !ok {
		return fmt.tprintf("unknown service: {}", name)
	}
	delete_key(&srv.cfg.services, name)
	return ""
}
