package main

// gorch entry point (Odin rewrite).
//
// What runs today: config loading, the runtime directory, a supervisor that forks
// the configured processes and applies restart policy, and a web server that serves
// the SPA plus the service API. Still only in the Go implementation on the `main`
// branch: log rotation/streaming, cron execution, IPC and TOTP.

import "core:fmt"
import "core:os"
import "core:strings"
import "core:sync"
import "core:sys/posix"
import "core:thread"
import "core:time"

import cfg "../../lib/config"
import glog "../../lib/log"
import "../../lib/logfile"
import paths "../../lib/paths"
import status "../../lib/status"
import supervisor "../../lib/supervisor"
import web "../../lib/web"

USAGE :: `gorch - process supervisor

Usage:
  gorch [options]

Options:
  -c, --config <path>   path to gorch.toml
  -h, --help            show this help
  -v, --version         show version

Without -c the config is looked up in $GORCH_CONFIG, ./gorch.toml,
~/.config/gorch/gorch.toml and /etc/gorch/gorch.toml, in that order.
`

// halt is set by the SIGTERM/SIGINT handler; the main thread watches it and then
// stops the children. An atomic store is the only thing safe to do in a handler.
halt: u32

on_signal :: proc "c" (sig: posix.Signal) {
	sync.atomic_store(&halt, 1)
}

install_signal_handlers :: proc() {
	action: posix.sigaction_t
	action.sa_handler = on_signal
	posix.sigemptyset(&action.sa_mask)
	action.sa_flags = {}
	posix.sigaction(.SIGTERM, &action, nil)
	posix.sigaction(.SIGINT, &action, nil)
}

main :: proc() {
	explicit_config, stop := parse_args()
	if stop {
		return
	}

	glog.init()
	install_utc_offset()

	if err := paths.ensure_runtime_dir(); err != nil {
		glog.error("cannot create runtime dir", "dir", paths.RuntimeDir, "err", err)
		os.exit(1)
	}
	glog.info("startup", "runtime_dir", paths.RuntimeDir)

	config_path, found := find_config(explicit_config)
	if !found {
		if len(explicit_config) > 0 {
			glog.error("config file not found", "path", explicit_config)
		} else {
			glog.error("no gorch.toml found; pass -c <path> or set GORCH_CONFIG")
		}
		os.exit(1)
	}

	config, cerr := cfg.load(config_path)
	if cerr != "" {
		glog.error("config error", "path", config_path, "err", cerr)
		os.exit(1)
	}
	glog.info(
		"config loaded",
		"path",
		config.config_path,
		"services",
		len(config.services),
		"web",
		config.web.web_enable,
	)

	install_signal_handlers()

	state_path := state_file_path(&config)
	sup := new(supervisor.Supervisor)
	if ierr := supervisor.init(sup, &config, state_path); len(ierr) > 0 {
		glog.error("supervisor init failed", "err", ierr)
		os.exit(1)
	}
	supervisor.start_all(sup)
	loop := supervisor.spawn_loop(sup)
	glog.info("supervisor running", "services", len(config.services), "state", state_path)

	if config.web.web_enable {
		provider := web.Provider {
			user       = sup,
			list       = provider_list,
			get        = provider_get,
			action     = provider_action,
			logs       = provider_logs,
			clear_logs = provider_clear_logs,
		}
		// The provider outgrew the inline poly-data slot, so the arguments live
		// on the heap for the lifetime of the process - like the server itself.
		serve_args := new(Serve_Args)
		serve_args^ = Serve_Args {
			cfg        = &config,
			static_dir = resolve_static_dir(&config),
			provider   = provider,
		}
		thread.create_and_start_with_poly_data(serve_args, serve_thread, context, .Normal, true)
	} else {
		glog.warn("WEB_ENABLE is false, running without the dashboard")
	}

	// Stay resident until a signal arrives. Everything else runs on its own thread.
	for sync.atomic_load(&halt) == 0 {
		time.sleep(200 * time.Millisecond)
	}

	glog.info("shutting down, stopping services")
	supervisor.halt_loop(loop)
	supervisor.shutdown(sup)
	glog.info("bye")
}

// ---- web provider ----

provider_list :: proc(user: rawptr) -> []status.ServiceStatus {
	sup := cast(^supervisor.Supervisor)user
	return supervisor.snapshot(sup)
}

provider_get :: proc(user: rawptr, name: string) -> (status.ServiceStatus, bool) {
	sup := cast(^supervisor.Supervisor)user
	for ss in supervisor.snapshot(sup) {
		if ss.name == name {
			return ss, true
		}
	}
	return {}, false
}

provider_action :: proc(user: rawptr, name, action: string) -> string {
	sup := cast(^supervisor.Supervisor)user
	switch action {
	case "start":
		return supervisor.start(sup, name)
	case "stop":
		return supervisor.stop(sup, name)
	case "restart":
		return supervisor.restart(sup, name)
	}
	return fmt.tprintf("unknown action: {}", action)
}

provider_logs :: proc(
	user: rawptr,
	name, kind: string,
	lines: int,
) -> (logfile.Tail, string, string) {
	sup := cast(^supervisor.Supervisor)user
	return supervisor.read_logs(sup, name, kind, lines)
}

provider_clear_logs :: proc(user: rawptr, name, kind: string) -> string {
	sup := cast(^supervisor.Supervisor)user
	return supervisor.clear_logs(sup, name, kind)
}

Serve_Args :: struct {
	cfg:        ^cfg.Config,
	static_dir: string,
	provider:   web.Provider,
}

serve_thread :: proc(args: ^Serve_Args) {
	if serr := web.serve(args.cfg, args.static_dir, args.provider); len(serr) > 0 {
		glog.error("web server failed", "err", serr)
		sync.atomic_store(&halt, 1)
	}
}

// ---- helpers ----

// state_file_path puts the runtime state next to the logs when LOG_DIR is set, so
// a deployment keeps one directory to back up; otherwise the default location.
state_file_path :: proc(config: ^cfg.Config) -> string {
	if len(config.log_dir) > 0 {
		return fmt.tprintf("{}/gorch.state", config.log_dir)
	}
	return paths.DefaultStateFile
}

// parse_args returns the explicit config path and whether main should stop
// before doing any work (help/version printed).
parse_args :: proc() -> (config_path: string, stop: bool) {
	args := os.args
	i := 1
	for i < len(args) {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			fmt.print(USAGE)
			return "", true
		case "-v", "--version":
			fmt.println("gorch (odin rewrite) - no release version yet")
			return "", true
		case "-c", "--config":
			if i + 1 >= len(args) {
				fmt.println("error: -c needs a path")
				os.exit(2)
			}
			return args[i + 1], false
		case:
			if len(arg) > 9 && arg[:9] == "--config=" {
				return arg[9:], false
			}
			fmt.printfln("error: unknown argument '{}'", arg)
			fmt.print(USAGE)
			os.exit(2)
		}
		i += 1
	}
	return "", false
}

find_config :: proc(explicit: string) -> (string, bool) {
	if len(explicit) > 0 {
		return explicit, os.exists(explicit)
	}
	if from_env, ok := os.lookup_env_alloc("GORCH_CONFIG", context.allocator); ok && len(from_env) > 0 {
		return from_env, os.exists(from_env)
	}
	if os.exists("gorch.toml") {
		return "gorch.toml", true
	}
	if home, ok := os.lookup_env_alloc("HOME", context.allocator); ok {
		path := fmt.tprintf("{}/.config/gorch/gorch.toml", home)
		if os.exists(path) {
			return path, true
		}
	}
	if os.exists("/etc/gorch/gorch.toml") {
		return "/etc/gorch/gorch.toml", true
	}
	return "", false
}

// resolve_static_dir finds the SPA directory. An empty result means "use the
// copy embedded in the binary", which is what a relocated binary relies on.
resolve_static_dir :: proc(config: ^cfg.Config) -> string {
	candidates := [dynamic]string{}
	defer delete(candidates)

	if from_env, ok := os.lookup_env_alloc("GORCH_WEB_DIR", context.allocator); ok && len(from_env) > 0 {
		append(&candidates, from_env)
	}
	// config_dir is empty for a bare "gorch.toml"; joining it blindly would
	// produce "/web".
	if len(config.config_dir) > 0 {
		append(&candidates, fmt.tprintf("{}/web", config.config_dir))
	}
	append(&candidates, "web")
	// `bin/gorch` and an installed copy both live one level below the repo root,
	// so the binary's own location is the most reliable anchor.
	if exe_dir, err := os.get_executable_directory(context.allocator); err == nil {
		append(&candidates, fmt.tprintf("{}/web", exe_dir))
		append(&candidates, fmt.tprintf("{}/../web", exe_dir))
	}

	for cand in candidates {
		if os.exists(fmt.tprintf("{}/index.html", cand)) {
			glog.info("frontend assets", "dir", cand)
			return cand
		}
	}
	// Not an error: the binary carries its own copy of web/.
	glog.info(
		"web/index.html not found on disk, using the embedded frontend",
		"tried",
		strings.join(candidates[:], ", "),
	)
	return ""
}
