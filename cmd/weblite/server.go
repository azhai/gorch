package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/azhai/gorch/internal/common"
	"github.com/gofiber/fiber/v3"
)

type Server struct {
	app     *fiber.App
	rootDir string
	addr    string
}

func NewServer(opts *Options) *Server {
	s := &Server{
		rootDir: opts.Dir,
		addr:    fmt.Sprintf("%s:%d", opts.Bind, opts.Port),
	}

	app := fiber.New(fiber.Config{
		AppName: "gorch-weblite",
	})

	app.Use(s.accessLogMiddleware())
	app.Use(s.pathGuardMiddleware())
	app.Get("/*", s.handleRequest)

	s.app = app
	return s
}

func (s *Server) Start() error {
	absDir, _ := filepath.Abs(s.rootDir)
	slog.Info("Serving directory", "dir", absDir, "addr", "http://"+s.addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	common.SetupSignalHandler(func(sig os.Signal) {
		if sig == syscall.SIGHUP {
			return
		}
		slog.Info("shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		s.app.Shutdown()
		<-shutdownCtx.Done()
		cancel()
	})

	if err := s.app.Listen(s.addr); err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}

// ── Middlewares ──────────────────────────────────────────

func (s *Server) accessLogMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		slog.Info("request",
			"time", start.Format("2006-01-02 15:04:05"),
			"method", c.Method(),
			"path", c.Path(),
			"status", c.Response().StatusCode(),
			"bytes", len(c.Response().Body()),
		)
		return err
	}
}

func (s *Server) pathGuardMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		decoded, err := url.PathUnescape(c.Path())
		if err != nil {
			return c.SendStatus(403)
		}

		if strings.Contains(decoded, "..") {
			return c.SendStatus(403)
		}

		localPath := filepath.Join(s.rootDir, filepath.Clean(decoded))
		if !isPathWithinRoot(s.rootDir, localPath) {
			return c.SendStatus(403)
		}

		return c.Next()
	}
}

func isPathWithinRoot(root, target string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}

	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	// Fast path: check prefix before expensive symlink resolution
	if strings.HasPrefix(absTarget, absRoot+string(os.PathSeparator)) {
		return true
	}
	if absTarget == absRoot {
		return true
	}

	// Slow path: resolve symlinks for accuracy
	evalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		evalRoot = absRoot
	}
	evalTarget, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		// Target doesn't exist; fall back to cleaned path comparison
		cleaned := filepath.Clean(target)
		return strings.HasPrefix(cleaned, root+string(os.PathSeparator)) || cleaned == root
	}

	return strings.HasPrefix(evalTarget, evalRoot+string(os.PathSeparator)) || evalTarget == evalRoot
}

// ── Request handlers ─────────────────────────────────────

func (s *Server) handleRequest(c fiber.Ctx) error {
	relPath := c.Params("*")
	if relPath == "" {
		relPath = "."
	}

	localPath := filepath.Join(s.rootDir, filepath.Clean(relPath))

	info, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.Status(404).SendString("Not Found")
		}
		if os.IsPermission(err) {
			return c.Status(403).SendString("Forbidden")
		}
		return c.Status(500).SendString("Internal Server Error")
	}

	if info.IsDir() {
		indexPath := filepath.Join(localPath, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			return s.serveFile(c, indexPath)
		}
		return s.serveDirectoryListing(c, localPath, relPath)
	}

	return s.serveFile(c, localPath)
}

func (s *Server) serveFile(c fiber.Ctx, path string) error {
	ext := filepath.Ext(path)
	if ext != "" {
		c.Type(strings.TrimPrefix(ext, "."))
	}

	stat, err := os.Stat(path)
	if err == nil {
		c.Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	}

	return c.SendFile(path)
}

func (s *Server) serveDirectoryListing(c fiber.Ctx, dirPath, relPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return c.Status(500).SendString("Internal Server Error")
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\">")
	sb.WriteString("<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	sb.WriteString(fmt.Sprintf("<title>Index of %s</title>", html.EscapeString("/"+relPath)))
	sb.WriteString(`<style>body{font-family:system-ui,sans-serif;margin:2rem}table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:.4rem .8rem;border-bottom:1px solid #eee}th{color:#666;font-weight:600;font-size:.85rem}td a{text-decoration:none;color:#0366d6}td a:hover{text-decoration:underline}</style>`)
	sb.WriteString("</head><body>")
	sb.WriteString(fmt.Sprintf("<h1>Index of %s</h1>", html.EscapeString("/"+relPath)))
	sb.WriteString("<table><thead><tr><th>Name</th><th>Size</th><th>Modified</th></tr></thead><tbody>")

	if relPath != "." && relPath != "" {
		sb.WriteString("<tr><td><a href=\"../\">../</a></td><td>-</td><td>-</td></tr>")
	}

	for _, entry := range entries {
		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			continue
		}

		displayName := html.EscapeString(name)
		href := url.PathEscape(name)

		if entry.IsDir() {
			sb.WriteString(fmt.Sprintf("<tr><td><a href=\"%s/\">%s/</a></td><td>-</td><td>%s</td></tr>",
				href, displayName, info.ModTime().Format("2006-01-02 15:04:05")))
		} else {
			sb.WriteString(fmt.Sprintf("<tr><td><a href=\"%s\">%s</a></td><td>%s</td><td>%s</td></tr>",
				href, displayName, formatSize(info.Size()), info.ModTime().Format("2006-01-02 15:04:05")))
		}
	}

	sb.WriteString("</tbody></table></body></html>")

	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(sb.String())
}

func formatSize(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
