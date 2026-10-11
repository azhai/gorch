package config

import "core:os"
import "core:strings"
import "core:fmt"
import "core:path/filepath"
import "../../lib/paths"
import "../../lib/toml"

// RestartPolicy is stored as the raw TOML string to keep Save() faithful.
// Recognised values: "always", "on-failure", "never" (empty == "never").
ServiceConfig :: struct {
	work_dir:       string,
	exec_cmd:       string,
	restart_cmd:    string,
	restart_policy: string,
	back_off:       int,
	check_port:     int,
	pre_action:     string,
	stdout:         string,
	stderr:         string,
	depends_on:     []string,
	cron:           string,
	cron_timeout:   int,
	env_vars:       map[string]string,
	pid_file:       string,
}

WebConfig :: struct {
	web_enable:  bool,
	web_addr:    string,
	web_auth:    bool,
	web_user:    string,
	web_pass:    string,
	url_prefix:  string,
	totp_enable: bool,
	totp_secret: string,
	totp_db:     string,
}

Config :: struct {
	services:     map[string]ServiceConfig,
	web:          WebConfig,
	log_dir:      string,
	pid_file:     string,
	services_lock: string,
	config_dir:   string,  // directory containing the config file
	config_path:  string,  // path to the config file
	topo_order:   []string,
}

is_valid_restart_policy :: proc(p: string) -> bool {
	return p == "always" || p == "on-failure" || p == "never" || p == ""
}

// ---- load ----

load :: proc(path: string) -> (cfg: Config, err: string) {
	data, ferr := os.read_entire_file_from_path(path, context.allocator)
	if ferr != nil {
		return Config{}, fmt.tprintf("cannot read config {}: {}", path, ferr)
	}
	root, perr := toml.parse_text(string(data))
	if perr.msg != "" {
		return Config{}, fmt.tprintf("toml parse error at line {}: {}", perr.line, perr.msg)
	}

	cfg = Config{}
	cfg.services = make(map[string]ServiceConfig)

	cfg.config_path = path
	cfg.config_dir = filepath.dir(path)

	if s, ok := toml.get_string(root, "LOG_DIR"); ok {
		cfg.log_dir = s
	}
	if s, ok := toml.get_string(root, "PID_FILE"); ok {
		cfg.pid_file = s
	}
	if s, ok := toml.get_string(root, "SERVICES_LOCK"); ok {
		cfg.services_lock = s
	}

	if web, ok := toml.get_table(root, "web"); ok {
		load_web(web, &cfg.web)
	}

	if svcs, ok := toml.get_table(root, "services"); ok {
		load_services(svcs, &cfg)
	}

	if e := validate_and_fill_defaults(&cfg); e != "" {
		return Config{}, e
	}
	if e := expand_env_in_config(&cfg); e != "" {
		return Config{}, e
	}
	if order, e := topological_sort(&cfg); e != "" {
		return Config{}, e
	} else {
		cfg.topo_order = order
	}
	return cfg, ""
}

load_web :: proc(tbl: ^toml.Table, w: ^WebConfig) {
	if b, ok := toml.get_bool(tbl, "WEB_ENABLE"); ok {
		w.web_enable = b
	}
	if s, ok := toml.get_string(tbl, "WEB_ADDR"); ok {
		w.web_addr = s
	}
	if b, ok := toml.get_bool(tbl, "WEB_AUTH"); ok {
		w.web_auth = b
	}
	if s, ok := toml.get_string(tbl, "WEB_USER"); ok {
		w.web_user = s
	}
	if s, ok := toml.get_string(tbl, "WEB_PASS"); ok {
		w.web_pass = s
	}
	if s, ok := toml.get_string(tbl, "URL_PREFIX"); ok {
		w.url_prefix = s
	}
	if b, ok := toml.get_bool(tbl, "TOTP_ENABLE"); ok {
		w.totp_enable = b
	}
	if s, ok := toml.get_string(tbl, "TOTP_SECRET"); ok {
		w.totp_secret = s
	}
	if s, ok := toml.get_string(tbl, "TOTP_DB"); ok {
		w.totp_db = s
	}
}

load_services :: proc(svcs: ^toml.Table, cfg: ^Config) {
	// Iterate the map of services; each value must be a Table.
	for name, val in svcs^ {
		if val.kind != .Table || val.tbl == nil {
			continue
		}
		sc := ServiceConfig{}
		sc.env_vars = make(map[string]string)
		st := val.tbl
		if s, ok := toml.get_string(st, "WORK_DIR"); ok {
			sc.work_dir = s
		}
		if s, ok := toml.get_string(st, "EXEC_CMD"); ok {
			sc.exec_cmd = s
		}
		if s, ok := toml.get_string(st, "RESTART_CMD"); ok {
			sc.restart_cmd = s
		}
		if s, ok := toml.get_string(st, "RESTART_POLICY"); ok {
			sc.restart_policy = s
		}
		if n, ok := toml.get_int(st, "BACK_OFF"); ok {
			sc.back_off = n
		}
		if n, ok := toml.get_int(st, "CHECK_PORT"); ok {
			sc.check_port = n
		}
		if s, ok := toml.get_string(st, "PRE_ACTION"); ok {
			sc.pre_action = s
		}
		if s, ok := toml.get_string(st, "STDOUT"); ok {
			sc.stdout = s
		}
		if s, ok := toml.get_string(st, "STDERR"); ok {
			sc.stderr = s
		}
		if s, ok := toml.get_string(st, "CRON"); ok {
			sc.cron = s
		}
		if n, ok := toml.get_int(st, "CRON_TIMEOUT"); ok {
			sc.cron_timeout = n
		}
		if s, ok := toml.get_string(st, "PID_FILE"); ok {
			sc.pid_file = s
		}
		if arr, ok := toml.get_array(st, "DEPENDS_ON"); ok {
			dep := [dynamic]string{}
			for e in arr {
				if e.kind == .String {
					append(&dep, e.s)
				}
			}
			sc.depends_on = dep[:]
		}
		if env, ok := toml.get_table(st, "ENV_VARS"); ok {
			for k, ev in env^ {
				if ev.kind == .String {
					sc.env_vars[k] = ev.s
				}
			}
		}
		cfg.services[name] = sc
	}
}

// ---- defaults ----

validate_and_fill_defaults :: proc(cfg: ^Config) -> string {
	if len(cfg.log_dir) == 0 {
		cfg.log_dir = paths.DefaultLogDir
	}
	if len(cfg.pid_file) == 0 {
		cfg.pid_file = paths.DefaultPIDFile
	}
	if len(cfg.services_lock) == 0 {
		cfg.services_lock = paths.DefaultServicesLock
	}
	if len(cfg.web.web_addr) == 0 {
		cfg.web.web_addr = "127.0.0.1:8080"
	}
	if len(cfg.web.totp_db) == 0 {
		cfg.web.totp_db = "/var/lib/gorch/totp.db"
	}
	if len(cfg.web.totp_secret) == 0 {
		if s, ok := os.lookup_env_alloc("GORCH_TOTP_SECRET", context.allocator); ok {
			cfg.web.totp_secret = s
		}
	}

	for name, sc in cfg.services {
		svc := sc // copy (value semantics)
		if len(svc.work_dir) == 0 {
			svc.work_dir = cfg.config_dir
		}
		if len(svc.restart_policy) == 0 {
			svc.restart_policy = "never"
		}
		if !is_valid_restart_policy(svc.restart_policy) {
			return fmt.tprintf("service {}: invalid RESTART_POLICY '{}'", name, svc.restart_policy)
		}
		if len(svc.exec_cmd) == 0 && len(svc.cron) == 0 {
			return fmt.tprintf("service {}: EXEC_CMD is required", name)
		}
		if len(svc.stdout) == 0 {
			svc.stdout = fmt.tprintf("{}/{}.out.log", cfg.log_dir, name)
		}
		if len(svc.stderr) == 0 {
			svc.stderr = fmt.tprintf("{}/{}.err.log", cfg.log_dir, name)
		}
		cfg.services[name] = svc
	}
	return ""
}

// ---- env expansion ----

expand_env :: proc(s: string) -> (string, string) {
	if !strings.contains(s, "${") {
		return s, ""
	}
	b := strings.builder_make()
	i := 0
	n := len(s)
	for i < n {
		if s[i] == '$' && i+1 < n && s[i+1] == '{' {
			end := i + 2
			for end < n && s[end] != '}' {
				end += 1
			}
			if end >= n {
				return "", fmt.tprintf("unterminated ${{ in '{}'", s)
			}
			var_name := s[i+2 : end]
			val, ok := os.lookup_env_alloc(var_name, context.allocator)
			if !ok {
				return "", fmt.tprintf("environment variable '{}' is not set", var_name)
			}
			strings.write_string(&b, val)
			i = end + 1
			continue
		}
		strings.write_byte(&b, s[i])
		i += 1
	}
	return strings.to_string(b), ""
}

expand_env_in_config :: proc(cfg: ^Config) -> string {
	for name, sc in cfg.services {
		svc := sc
		if r, e := expand_env(svc.exec_cmd); e != "" {
			return fmt.tprintf("service {}: {}", name, e)
		} else {
			svc.exec_cmd = r
		}
		if r, e := expand_env(svc.work_dir); e != "" {
			return fmt.tprintf("service {}: {}", name, e)
		} else {
			svc.work_dir = r
		}
		if r, e := expand_env(svc.restart_cmd); e != "" {
			return fmt.tprintf("service {}: {}", name, e)
		} else {
			svc.restart_cmd = r
		}
		if r, e := expand_env(svc.pre_action); e != "" {
			return fmt.tprintf("service {}: {}", name, e)
		} else {
			svc.pre_action = r
		}
		if r, e := expand_env(svc.stdout); e != "" {
			return fmt.tprintf("service {}: {}", name, e)
		} else {
			svc.stdout = r
		}
		if r, e := expand_env(svc.stderr); e != "" {
			return fmt.tprintf("service {}: {}", name, e)
		} else {
			svc.stderr = r
		}
		if r, e := expand_env(svc.pid_file); e != "" {
			return fmt.tprintf("service {}: {}", name, e)
		} else {
			svc.pid_file = r
		}
		cfg.services[name] = svc
	}
	return ""
}

// ---- dependency validation + topological sort ----

validate_dependencies :: proc(cfg: ^Config) -> string {
	for name, sc in cfg.services {
		for dep in sc.depends_on {
			if _, ok := cfg.services[dep]; !ok {
				return fmt.tprintf("service {} depends on unknown service '{}'", name, dep)
			}
			if dep == name {
				return fmt.tprintf("service {} depends on itself", name)
			}
		}
	}
	return ""
}

// topological_sort returns services in dependency order (DFS, cycle detected).
topological_sort :: proc(cfg: ^Config) -> ([]string, string) {
	if e := validate_dependencies(cfg); e != "" {
		return nil, e
	}
	state := make(map[string]int) // 0=unvisited,1=visiting,2=visited
	order := [dynamic]string{}

	names := sorted_service_names(cfg)
	defer delete(names)
	for n in names {
		if e := dfs_visit(n, cfg, &state, &order); e != "" {
			return nil, e
		}
	}
	return order[:], ""
}

dfs_visit :: proc(name: string, cfg: ^Config, state: ^map[string]int, order: ^[dynamic]string) -> string {
	s := state^[name]
	if s == 1 {
		return fmt.tprintf("dependency cycle detected at '{}'", name)
	}
	if s == 2 {
		return ""
	}
	state^[name] = 1
	sc, ok := cfg.services[name]
	if ok {
		for dep in sc.depends_on {
			if e := dfs_visit(dep, cfg, state, order); e != "" {
				return e
			}
		}
	}
	state^[name] = 2
	append(order, name)
	return ""
}

sorted_service_names :: proc(cfg: ^Config) -> []string {
	d := [dynamic]string{}
	for name in cfg.services {
		append(&d, name)
	}
	insertion_sort_strings(d[:])
	return d[:]
}

insertion_sort_strings :: proc(a: []string) {
	for i := 1; i < len(a); i += 1 {
		j := i
		for j > 0 && a[j] < a[j-1] {
			a[j], a[j-1] = a[j-1], a[j]
			j -= 1
		}
	}
}

// ---- save / serialize ----

// toml_quote renders s as a TOML basic string with escapes.
toml_quote :: proc(s: string) -> string {
	b := strings.builder_make()
	strings.write_byte(&b, '"')
	for i in 0..<len(s) {
		ch := s[i]
		if ch == '"' {
			strings.write_string(&b, "\\\"")
		} else if ch == '\\' {
			strings.write_string(&b, "\\\\")
		} else if ch == '\n' {
			strings.write_string(&b, "\\n")
		} else if ch == '\t' {
			strings.write_string(&b, "\\t")
		} else {
			strings.write_byte(&b, ch)
		}
	}
	strings.write_byte(&b, '"')
	return strings.to_string(b)
}

string_keys_sorted :: proc(m: ^map[string]string) -> []string {
	d := [dynamic]string{}
	for k in m^ {
		append(&d, k)
	}
	insertion_sort_strings(d[:])
	return d[:]
}

serialize_config :: proc(cfg: ^Config) -> string {
	b := strings.builder_make()

	if cfg.log_dir != paths.DefaultLogDir {
		fmt.sbprintf(&b, "LOG_DIR = {}\n", toml_quote(cfg.log_dir))
	}
	if cfg.pid_file != paths.DefaultPIDFile {
		fmt.sbprintf(&b, "PID_FILE = {}\n", toml_quote(cfg.pid_file))
	}
	if cfg.services_lock != paths.DefaultServicesLock {
		fmt.sbprintf(&b, "SERVICES_LOCK = {}\n", toml_quote(cfg.services_lock))
	}

	// web table (only emit non-default fields)
	web := cfg.web
	web_has := web.web_enable || web.web_addr != "127.0.0.1:8080" || web.web_auth ||
		len(web.web_user) > 0 || len(web.web_pass) > 0 || len(web.url_prefix) > 0 ||
		web.totp_enable || len(web.totp_secret) > 0 || len(web.totp_db) > 0
	if web_has {
		if len(cfg.log_dir) + len(cfg.pid_file) + len(cfg.services_lock) > 0 {
			strings.write_byte(&b, '\n')
		}
		strings.write_string(&b, "[web]\n")
		if web.web_enable {
			strings.write_string(&b, "WEB_ENABLE = true\n")
		}
		if web.web_addr != "" && web.web_addr != "127.0.0.1:8080" {
			fmt.sbprintf(&b, "WEB_ADDR = {}\n", toml_quote(web.web_addr))
		}
		if web.web_auth {
			strings.write_string(&b, "WEB_AUTH = true\n")
		}
		if len(web.web_user) > 0 {
			fmt.sbprintf(&b, "WEB_USER = {}\n", toml_quote(web.web_user))
		}
		if len(web.web_pass) > 0 {
			fmt.sbprintf(&b, "WEB_PASS = {}\n", toml_quote(web.web_pass))
		}
		if len(web.url_prefix) > 0 {
			fmt.sbprintf(&b, "URL_PREFIX = {}\n", toml_quote(web.url_prefix))
		}
		if web.totp_enable {
			strings.write_string(&b, "TOTP_ENABLE = true\n")
		}
		if len(web.totp_secret) > 0 {
			fmt.sbprintf(&b, "TOTP_SECRET = {}\n", toml_quote(web.totp_secret))
		}
		if len(web.totp_db) > 0 && web.totp_db != "/var/lib/gorch/totp.db" {
			fmt.sbprintf(&b, "TOTP_DB = {}\n", toml_quote(web.totp_db))
		}
	}

	names := sorted_service_names(cfg)
	defer delete(names)
	for name in names {
		sc := cfg.services[name]
		strings.write_string(&b, "\n[services.")
		strings.write_string(&b, name)
		strings.write_string(&b, "]\n")
		if sc.back_off != 0 {
			fmt.sbprintf(&b, "BACK_OFF = {}\n", sc.back_off)
		}
		if sc.check_port != 0 {
			fmt.sbprintf(&b, "CHECK_PORT = {}\n", sc.check_port)
		}
		if len(sc.cron) > 0 {
			fmt.sbprintf(&b, "CRON = {}\n", toml_quote(sc.cron))
		}
		if sc.cron_timeout != 0 {
			fmt.sbprintf(&b, "CRON_TIMEOUT = {}\n", sc.cron_timeout)
		}
		if len(sc.depends_on) > 0 {
			arr := strings.builder_make()
			strings.write_string(&arr, "[")
			for i in 0..<len(sc.depends_on) {
				if i > 0 {
					strings.write_string(&arr, ", ")
				}
				strings.write_string(&arr, toml_quote(sc.depends_on[i]))
			}
			strings.write_string(&arr, "]")
			fmt.sbprintf(&b, "DEPENDS_ON = {}\n", strings.to_string(arr))
		}
		if len(sc.env_vars) > 0 {
			keys := string_keys_sorted(&sc.env_vars)
			defer delete(keys)
			inline := strings.builder_make()
			strings.write_string(&inline, "{")
			for i in 0..<len(keys) {
				if i > 0 {
					strings.write_string(&inline, ", ")
				}
				fmt.sbprintf(&inline, "{} = {}", keys[i], toml_quote(sc.env_vars[keys[i]]))
			}
			strings.write_string(&inline, "}")
			fmt.sbprintf(&b, "ENV_VARS = {}\n", strings.to_string(inline))
		}
		if len(sc.exec_cmd) > 0 {
			fmt.sbprintf(&b, "EXEC_CMD = {}\n", toml_quote(sc.exec_cmd))
		}
		if len(sc.pid_file) > 0 {
			fmt.sbprintf(&b, "PID_FILE = {}\n", toml_quote(sc.pid_file))
		}
		if len(sc.pre_action) > 0 {
			fmt.sbprintf(&b, "PRE_ACTION = {}\n", toml_quote(sc.pre_action))
		}
		if len(sc.restart_cmd) > 0 {
			fmt.sbprintf(&b, "RESTART_CMD = {}\n", toml_quote(sc.restart_cmd))
		}
		if len(sc.restart_policy) > 0 && sc.restart_policy != "never" {
			fmt.sbprintf(&b, "RESTART_POLICY = {}\n", toml_quote(sc.restart_policy))
		}
		if len(sc.stderr) > 0 {
			fmt.sbprintf(&b, "STDERR = {}\n", toml_quote(sc.stderr))
		}
		if len(sc.stdout) > 0 {
			fmt.sbprintf(&b, "STDOUT = {}\n", toml_quote(sc.stdout))
		}
		if len(sc.work_dir) > 0 && sc.work_dir != cfg.config_dir && sc.work_dir != "." {
			fmt.sbprintf(&b, "WORK_DIR = {}\n", toml_quote(sc.work_dir))
		}
	}
	return strings.to_string(b)
}

save_config :: proc(cfg: ^Config) -> string {
	text := serialize_config(cfg)
	err := os.write_entire_file_from_string(cfg.config_path, text)
	if err != nil {
		return fmt.tprintf("cannot write config: {}", err)
	}
	return ""
}
