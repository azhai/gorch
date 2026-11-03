package web

// Minimal HTTP surface for the Odin rewrite.
//
// This is deliberately not the finished server: the supervisor, log streaming
// and TOTP endpoints still live only in the Go implementation. What is here is
// enough for the SPA in web/ to load and render: static files, the URL-prefix
// injection, a JWT login, the service list and the SSE heartbeat.

import "core:fmt"
import "core:mem/virtual"
import "core:net"
import "core:os"
import "core:strings"
import "core:thread"
import "core:time"

import "../../lib/config"
import "../../lib/jwt"
import glog "../../lib/log"
import "../../lib/logfile"
import "../../lib/status"

// Provider is how the web layer reads and drives services. The supervisor package
// fills it in; leaving the procs nil keeps the UI read-only (everything shows as
// stopped), which is what a build without a supervisor should do.
Provider :: struct {
	user:   rawptr,
	list:   proc(user: rawptr) -> []status.ServiceStatus,
	get:    proc(user: rawptr, name: string) -> (status.ServiceStatus, bool),
	action: proc(user: rawptr, name, action: string) -> string, // "" on success
	// logs hands back a tail the caller must destroy, plus the path it came
	// from so the UI can show where a service is writing.
	logs:       proc(user: rawptr, name, kind: string, lines: int) -> (logfile.Tail, string, string),
	clear_logs: proc(user: rawptr, name, kind: string) -> string,
}

Server :: struct {
	cfg:        ^config.Config,
	static_dir: string, // empty when serving the embedded copy
	embedded:   map[string][]byte,
	provider:   Provider,
}

Conn_Args :: struct {
	srv:  ^Server,
	conn: net.TCP_Socket,
}

str_bytes :: proc(s: string) -> []byte {
	return transmute([]byte)s
}

// serve binds WEB_ADDR and accepts connections forever; each connection is
// handled on its own detached thread so a long-lived SSE stream never blocks
// the dashboard.
serve :: proc(cfg: ^config.Config, static_dir: string, provider: Provider) -> string {
	endpoint, ok := net.parse_endpoint(cfg.web.web_addr)
	if !ok {
		return fmt.tprintf("invalid WEB_ADDR '{}'", cfg.web.web_addr)
	}
	listener, lerr := net.listen_tcp(endpoint)
	if lerr != nil {
		return fmt.tprintf("cannot listen on {}: {}", cfg.web.web_addr, lerr)
	}
	defer net.close(listener)

	srv := Server{cfg = cfg, provider = provider}
	source := ""
	if len(static_dir) > 0 && os.exists(fmt.tprintf("{}/index.html", static_dir)) {
		srv.static_dir = static_dir
		source = static_dir
	} else {
		// No usable directory next to the binary or the config: fall back to the
		// copy baked in at build time rather than refusing to start.
		srv.embedded = embedded_assets()
		source = "embedded"
		glog.info("serving the embedded frontend", "files", len(srv.embedded))
	}

	glog.info("web ui listening",
		"addr", cfg.web.web_addr,
		"prefix", normalize_prefix(cfg.web.url_prefix),
		"auth", cfg.web.web_auth,
		"assets", source)

	for {
		conn, _, aerr := net.accept_tcp(listener)
		if aerr != nil {
			continue
		}
		args := Conn_Args{srv = &srv, conn = conn}
		thread.create_and_start_with_poly_data(args, handle_conn, context, .Normal, true)
	}
	return ""
}

// ---- connection handling ----

handle_conn :: proc(args: Conn_Args) {
	// Every allocation a request makes is released when the thread ends, which
	// keeps the shared heap out of the picture entirely. Each connection needs
	// its own arena: Odin's temp allocator belongs to the thread that owns it.
	arena: virtual.Arena
	if err := virtual.arena_init_growing(&arena); err != nil {
		return
	}
	defer virtual.arena_destroy(&arena)
	context.allocator = virtual.arena_allocator(&arena)

	conn := args.conn
	defer net.close(conn)

	buf: [8192]u8
	total := 0
	header_end := -1
	for total < len(buf) {
		n, rerr := net.recv_tcp(conn, buf[total:])
		if n <= 0 || rerr != nil {
			break
		}
		total += n
		if idx := strings.index(string(buf[:total]), "\r\n\r\n"); idx >= 0 {
			header_end = idx + 4
			break
		}
	}
	if header_end < 0 && total > 0 {
		// Headers never terminated: answer 400 rather than parsing garbage.
		send_text(conn, 400, "bad request")
		return
	}
	if total == 0 {
		return
	}

	// The body can arrive in a later segment than the headers (a POST split
	// across two writes is common), so keep reading until Content-Length bytes
	// are in hand. Without this, login raced and intermittently saw "".
	need := content_length(string(buf[:header_end]))
	for header_end + need > total && total < len(buf) {
		n, rerr := net.recv_tcp(conn, buf[total:])
		if n <= 0 || rerr != nil {
			break
		}
		total += n
	}
	handle_request(args.srv, conn, string(buf[:total]))
}

// content_length parses the Content-Length header, returning 0 when absent or
// unparsable.
content_length :: proc(raw: string) -> int {
	value, ok := find_header(raw, "content-length")
	if !ok {
		return 0
	}
	return parse_int(value)
}

// parse_int reads a leading run of digits, which is all the query strings and
// headers here need.
parse_int :: proc(value: string) -> int {
	n := 0
	for i in 0 ..< len(value) {
		if value[i] < '0' || value[i] > '9' {
			break
		}
		n = n * 10 + int(value[i] - '0')
	}
	return n
}

handle_request :: proc(srv: ^Server, conn: net.TCP_Socket, raw: string) {
	eol := strings.index(raw, "\r\n")
	line := raw if eol < 0 else raw[:eol]
	fields := strings.fields(line)
	if len(fields) < 2 {
		send_text(conn, 400, "bad request")
		return
	}
	method := fields[0]
	target := fields[1]

	path := target
	query := ""
	if qi := strings.index_byte(target, '?'); qi >= 0 {
		path = target[:qi]
		query = target[qi+1:]
	}

	prefix := normalize_prefix(srv.cfg.web.url_prefix)
	rest := path
	if prefix != "" {
		if path == prefix {
			send_redirect(conn, fmt.tprintf("{}/", prefix))
			return
		}
		if !strings.has_prefix(path, fmt.tprintf("{}/", prefix)) {
			send_text(conn, 404, "not found")
			return
		}
		rest = path[len(prefix):]
	}

	if rest == "/api" || strings.has_prefix(rest, "/api/") {
		handle_api(srv, conn, method, rest, query, raw)
		return
	}
	serve_static(srv, conn, rest, prefix)
}

// ---- api ----

handle_api :: proc(
	srv: ^Server,
	conn: net.TCP_Socket,
	method, path, query, raw: string,
) {
	if !authorized(srv, path, query, raw) {
		send_json_error(conn, 401, "authentication required")
		return
	}

	switch {
	case path == "/api/health":
		if method != "GET" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		send_json_ok(conn, 200, `{"status":"ok"}`)

	case path == "/api/services":
		if method != "GET" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		send_json_ok(conn, 200, services_json(srv))

	case strings.has_prefix(path, "/api/services/"):
		handle_service(srv, conn, method, path, query, raw)

	case path == "/api/events":
		handle_sse(srv, conn, query)

	case path == "/api/auth/login":
		if method != "POST" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		handle_login(srv, conn, raw)

	case:
		send_json_error(conn, 404, "not found")
	}
}

authorized :: proc(srv: ^Server, path, query, raw: string) -> bool {
	cfg := srv.cfg.web
	if !cfg.web_auth {
		return true
	}
	// Logging in cannot require a token, and the SPA probes /services without
	// one to learn whether auth is on at all.
	if strings.has_prefix(path, "/api/auth/") {
		return true
	}
	// EventSource cannot set headers, so the stream takes the token as a query
	// parameter - same convention as the Go server.
	token: string
	if path == "/api/events" {
		token, _ = query_param(query, "token")
	} else if h, ok := find_header(raw, "authorization"); ok {
		token = strings.trim_prefix(h, "Bearer ")
		token = strings.trim_space(token)
	}
	if len(token) == 0 {
		return false
	}
	_, valid := jwt.validate(token, cfg.web_pass)
	return valid
}

handle_login :: proc(srv: ^Server, conn: net.TCP_Socket, raw: string) {
	cfg := srv.cfg.web
	body_idx := strings.index(raw, "\r\n\r\n")
	body := "" if body_idx < 0 else raw[body_idx+4:]

	username, _ := json_string_field(body, "username")
	password, _ := json_string_field(body, "password")

	if username != cfg.web_user || password != cfg.web_pass {
		send_json_error(conn, 401, "invalid credentials")
		return
	}

	token := jwt.generate(username, cfg.web_pass)
	b := strings.builder_make()
	strings.write_string(&b, `{"token":`)
	write_json_string(&b, token)
	strings.write_string(&b, `}`)
	send_json_ok(conn, 200, strings.to_string(b))
}

services_json :: proc(srv: ^Server) -> string {
	b := strings.builder_make()
	strings.write_byte(&b, '[')

	list: []status.ServiceStatus
	if srv.provider.list != nil {
		list = srv.provider.list(srv.provider.user)
	}
	if len(list) > 0 {
		for ss, i in list {
			if i > 0 {
				strings.write_byte(&b, ',')
			}
			write_service_json(&b, ss)
		}
	} else {
		// No supervisor wired in: still list what is configured so the dashboard
		// has rows instead of an empty page.
		for name, i in srv.cfg.topo_order {
			if i > 0 {
				strings.write_byte(&b, ',')
			}
			write_service_json(&b, status.ServiceStatus{name = name, status = .Stopped})
		}
	}

	strings.write_byte(&b, ']')
	return strings.to_string(b)
}

write_service_json :: proc(b: ^strings.Builder, ss: status.ServiceStatus) {
	strings.write_string(b, `{"name":`)
	write_json_string(b, ss.name)
	fmt.sbprintf(
		b,
		`,"status":"{}","pid":{},"uptime":{},"memoryMB":{:.2f},"startedAt":{}}}`,
		status.status_to_string(ss.status),
		ss.pid,
		ss.uptime,
		ss.memory_mb,
		ss.started_at,
	)
}

// service_status returns one service, falling back to a stopped entry for a
// configured name when no supervisor is wired in.
service_status :: proc(srv: ^Server, name: string) -> (status.ServiceStatus, bool) {
	if srv.provider.get != nil {
		return srv.provider.get(srv.provider.user, name)
	}
	for configured in srv.cfg.topo_order {
		if configured == name {
			return status.ServiceStatus{name = name, status = .Stopped}, true
		}
	}
	return {}, false
}

// handle_service covers /api/services/<name> and /api/services/<name>/<action>.
handle_service :: proc(
	srv: ^Server,
	conn: net.TCP_Socket,
	method, path, query, raw: string,
) {
	rest := strings.trim_prefix(path, "/api/services/")
	name := rest
	action := ""
	if slash := strings.index_byte(rest, '/'); slash >= 0 {
		name = rest[:slash]
		action = rest[slash + 1:]
	}
	if len(name) == 0 {
		send_json_error(conn, 404, "not found")
		return
	}

	if len(action) == 0 {
		if method != "GET" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		ss, ok := service_status(srv, name)
		if !ok {
			send_json_error(conn, 404, "service not found")
			return
		}
		b := strings.builder_make()
		write_service_json(&b, ss)
		send_json_ok(conn, 200, strings.to_string(b))
		return
	}

	if action == "logs" {
		if method != "GET" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		handle_logs(srv, conn, name, query)
		return
	}
	if action == "logs/clear" {
		if method != "POST" {
			send_json_error(conn, 405, "method not allowed")
			return
		}
		handle_clear_logs(srv, conn, name, query)
		return
	}

	if method != "POST" {
		send_json_error(conn, 405, "method not allowed")
		return
	}
	if action != "start" && action != "stop" && action != "restart" {
		send_json_error(conn, 404, "not found")
		return
	}
	if srv.provider.action == nil {
		send_json_error(conn, 503, "supervisor is not running")
		return
	}
	// The body is unused for these actions; `raw` is accepted for symmetry with
	// the other POST handlers and so future options (grace period) can be added.
	_ = raw
	if err := srv.provider.action(srv.provider.user, name, action); len(err) > 0 {
		send_json_error(conn, 400, err)
		return
	}
	b := strings.builder_make()
	strings.write_string(&b, `{"name":`)
	write_json_string(&b, name)
	fmt.sbprintf(&b, `,"action":"{}"}`, action)
	send_json_ok(conn, 200, strings.to_string(b))
}

// log_kind reads ?type=stdout|stderr, defaulting to stdout like the Go server.
log_kind :: proc(query: string) -> string {
	if v, ok := query_param(query, "type"); ok && v == "stderr" {
		return "stderr"
	}
	return "stdout"
}

handle_logs :: proc(srv: ^Server, conn: net.TCP_Socket, name, query: string) {
	if srv.provider.logs == nil {
		send_json_error(conn, 503, "supervisor is not running")
		return
	}
	lines := 500
	if v, ok := query_param(query, "lines"); ok {
		if parsed := parse_int(v); parsed > 0 {
			lines = parsed
		}
	}

	t, path, err := srv.provider.logs(srv.provider.user, name, log_kind(query), lines)
	defer logfile.tail_destroy(&t)
	if len(err) > 0 {
		send_json_error(conn, 400, err)
		return
	}

	b := strings.builder_make()
	strings.write_string(&b, `{"lines":[`)
	for line, i in t.lines {
		if i > 0 {
			strings.write_byte(&b, ',')
		}
		write_json_string(&b, line)
	}
	strings.write_string(&b, `],"logPath":`)
	write_json_string(&b, path)
	strings.write_byte(&b, '}')
	send_json_ok(conn, 200, strings.to_string(b))
}

handle_clear_logs :: proc(srv: ^Server, conn: net.TCP_Socket, name, query: string) {
	if srv.provider.clear_logs == nil {
		send_json_error(conn, 503, "supervisor is not running")
		return
	}
	if err := srv.provider.clear_logs(srv.provider.user, name, log_kind(query)); len(err) > 0 {
		send_json_error(conn, 400, err)
		return
	}
	send_json_ok(conn, 200, `{"message":"logs cleared"}`)
}

// handle_sse publishes the events the SPA listens for. Without a supervisor
// there is no status to report, so the tick carries an empty service map and
// only keeps the connection (and its "connected" indicator) alive.
handle_sse :: proc(srv: ^Server, conn: net.TCP_Socket, query: string) {
	interval := "5s"
	if v, ok := query_param(query, "interval"); ok && len(v) > 0 {
		interval = v
	}
	head := strings.builder_make()
	strings.write_string(&head, "HTTP/1.1 200 OK\r\n")
	strings.write_string(&head, "Content-Type: text/event-stream\r\n")
	strings.write_string(&head, "Cache-Control: no-cache\r\n")
	strings.write_string(&head, "Connection: keep-alive\r\n")
	strings.write_string(&head, "X-Accel-Buffering: no\r\n\r\n")
	if !send_all(conn, str_bytes(strings.to_string(head))) {
		return
	}

	hello := fmt.tprintf("event: connected\ndata: {{\"interval\":\"{}\"}}\n\n", interval)
	if !send_all(conn, str_bytes(hello)) {
		return
	}

	for {
		time.sleep(5 * time.Second)
		if !send_all(conn, str_bytes("event: uptime_tick\ndata: {\"services\":{}}\n\n")) {
			return
		}
	}
}

// ---- static files ----

serve_static :: proc(srv: ^Server, conn: net.TCP_Socket, rest, prefix: string) {
	if strings.contains(rest, "..") {
		send_text(conn, 403, "forbidden")
		return
	}
	if rest == "/" || rest == "" {
		send_index(srv, conn, prefix)
		return
	}

	if data, ok := read_asset(srv, rest); ok {
		send(conn, 200, mime_for(rest), data)
		return
	}
	// Unknown paths fall back to the SPA so client-side routes survive a reload.
	send_index(srv, conn, prefix)
}

// read_asset prefers the web/ directory on disk and falls back to the embedded
// copy, so editing the frontend needs no rebuild but a moved binary still works.
read_asset :: proc(srv: ^Server, rest: string) -> ([]byte, bool) {
	if len(srv.static_dir) > 0 {
		full := fmt.tprintf("{}{}", srv.static_dir, rest)
		if os.exists(full) && !os.is_dir(full) {
			if data, err := os.read_entire_file_from_path(full, context.allocator); err == nil {
				return data, true
			}
		}
		return nil, false
	}
	if data, ok := srv.embedded[rest]; ok {
		return data, true
	}
	return nil, false
}

send_index :: proc(srv: ^Server, conn: net.TCP_Socket, prefix: string) {
	html, ok := read_asset(srv, "/index.html")
	if !ok {
		send_text(conn, 500, "index.html missing")
		return
	}
	send(conn, 200, "text/html; charset=utf-8", str_bytes(inject_prefix(string(html), prefix)))
}

// inject_prefix stamps window.__URL_PREFIX__ into the SPA so one binary can be
// mounted at any sub-path without rebuilding the frontend.
inject_prefix :: proc(html, prefix: string) -> string {
	if len(prefix) == 0 {
		// Nothing to mount; leave the document byte-identical.
		return html
	}
	marker := `<script type="module" src="./js/main.js"></script>`
	snippet := fmt.tprintf(
		"<script>window.__URL_PREFIX__ = \"{}\";</script>\n    {}",
		prefix, marker,
	)
	if strings.contains(html, marker) {
		out, _ := strings.replace(html, marker, snippet, 1)
		return out
	}
	if head := strings.index(html, "<head>"); head >= 0 {
		at := head + len("<head>")
		b := strings.builder_make()
		strings.write_string(&b, html[:at])
		strings.write_string(&b, "\n    <script>window.__URL_PREFIX__ = \"")
		strings.write_string(&b, prefix)
		strings.write_string(&b, "\";</script>")
		strings.write_string(&b, html[at:])
		return strings.to_string(b)
	}
	return html
}

mime_for :: proc(path: string) -> string {
	dot := strings.last_index_byte(path, '.')
	if dot < 0 {
		return "application/octet-stream"
	}
	switch path[dot:] {
	case ".html", ".htm": return "text/html; charset=utf-8"
	case ".css":          return "text/css; charset=utf-8"
	case ".js", ".mjs":   return "text/javascript; charset=utf-8"
	case ".json":         return "application/json; charset=utf-8"
	case ".svg":          return "image/svg+xml"
	case ".png":          return "image/png"
	case ".jpg", ".jpeg": return "image/jpeg"
	case ".ico":          return "image/x-icon"
	case ".woff2":        return "font/woff2"
	case ".map":          return "application/json; charset=utf-8"
	case ".txt":          return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// ---- response helpers ----

send_all :: proc(conn: net.TCP_Socket, data: []byte) -> bool {
	n, err := net.send_tcp(conn, data)
	return err == nil && n == len(data)
}

send :: proc(conn: net.TCP_Socket, code: int, content_type: string, body: []byte) {
	head := strings.builder_make()
	fmt.sbprintf(&head, "HTTP/1.1 {} {}\r\n", code, status_text(code))
	fmt.sbprintf(&head, "Content-Type: {}\r\n", content_type)
	fmt.sbprintf(&head, "Content-Length: {}\r\n", len(body))
	strings.write_string(&head, "Cache-Control: no-store\r\nConnection: close\r\n\r\n")
	if !send_all(conn, str_bytes(strings.to_string(head))) {
		return
	}
	if len(body) > 0 {
		send_all(conn, body)
	}
}

send_text :: proc(conn: net.TCP_Socket, code: int, msg: string) {
	send(conn, code, "text/plain; charset=utf-8", str_bytes(msg))
}

send_redirect :: proc(conn: net.TCP_Socket, location: string) {
	head := strings.builder_make()
	fmt.sbprintf(&head, "HTTP/1.1 301 Moved Permanently\r\nLocation: {}\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", location)
	send_all(conn, str_bytes(strings.to_string(head)))
}

send_json_ok :: proc(conn: net.TCP_Socket, code: int, data_json: string) {
	b := strings.builder_make()
	strings.write_string(&b, `{"success":true,"data":`)
	strings.write_string(&b, data_json)
	strings.write_byte(&b, '}')
	send(conn, code, "application/json; charset=utf-8", str_bytes(strings.to_string(b)))
}

send_json_error :: proc(conn: net.TCP_Socket, code: int, msg: string) {
	b := strings.builder_make()
	strings.write_string(&b, `{"success":false,"message":`)
	write_json_string(&b, msg)
	strings.write_byte(&b, '}')
	send(conn, code, "application/json; charset=utf-8", str_bytes(strings.to_string(b)))
}

status_text :: proc(code: int) -> string {
	switch code {
	case 200: return "OK"
	case 301: return "Moved Permanently"
	case 400: return "Bad Request"
	case 401: return "Unauthorized"
	case 403: return "Forbidden"
	case 404: return "Not Found"
	case 405: return "Method Not Allowed"
	case 500: return "Internal Server Error"
	}
	return "OK"
}

// ---- small parsers ----

write_json_string :: proc(b: ^strings.Builder, s: string) {
	strings.write_byte(b, '"')
	for i in 0 ..< len(s) {
		switch s[i] {
		case '"':  strings.write_string(b, "\\\"")
		case '\\': strings.write_string(b, "\\\\")
		case '\n': strings.write_string(b, "\\n")
		case '\r': strings.write_string(b, "\\r")
		case '\t': strings.write_string(b, "\\t")
		case:      strings.write_byte(b, s[i])
		}
	}
	strings.write_byte(b, '"')
}

normalize_prefix :: proc(p: string) -> string {
	s := strings.trim_space(p)
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[0] != '/' {
		return fmt.tprintf("/{}", s)
	}
	return s
}

find_header :: proc(raw: string, name: string) -> (string, bool) {
	idx := strings.index(raw, "\r\n")
	if idx < 0 {
		return "", false
	}
	rest := raw[idx+2:]
	for len(rest) > 0 {
		e := strings.index(rest, "\r\n")
		line := rest if e < 0 else rest[:e]
		if len(line) == 0 {
			break
		}
		if colon := strings.index_byte(line, ':'); colon > 0 {
			if strings.equal_fold(strings.trim_space(line[:colon]), name) {
				return strings.trim_space(line[colon+1:]), true
			}
		}
		if e < 0 {
			break
		}
		rest = rest[e+2:]
	}
	return "", false
}

query_param :: proc(query, name: string) -> (string, bool) {
	rest := query
	for len(rest) > 0 {
		amp := strings.index_byte(rest, '&')
		pair := rest if amp < 0 else rest[:amp]
		if eq := strings.index_byte(pair, '='); eq > 0 {
			if pair[:eq] == name {
				return pair[eq+1:], true
			}
		} else if pair == name {
			return "", true
		}
		if amp < 0 {
			break
		}
		rest = rest[amp+1:]
	}
	return "", false
}

json_string_field :: proc(js, key: string) -> (string, bool) {
	needle := fmt.tprintf(`"{}"`, key)
	i := strings.index(js, needle)
	if i < 0 {
		return "", false
	}
	i += len(needle)
	for i < len(js) && (js[i] == ' ' || js[i] == '\t' || js[i] == ':' || js[i] == '\n' || js[i] == '\r') {
		i += 1
	}
	if i >= len(js) || js[i] != '"' {
		return "", false
	}
	i += 1
	start := i
	for i < len(js) && js[i] != '"' {
		i += 1
	}
	return js[start:i], true
}
