package web

import (
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/azhai/gorch/webui"
	"github.com/gofiber/fiber/v3"
)

var webuiDist = webui.DistFS

var (
	strippedFS fs.FS
	fsOnce     sync.Once
)

func getFileSystem() fs.FS {
	fsOnce.Do(func() {
		sub, err := fs.Sub(webuiDist, "dist")
		if err != nil {
			panic("failed to create sub filesystem: " + err.Error())
		}
		strippedFS = sub
	})
	return strippedFS
}

func staticAssetHandler(c fiber.Ctx) error {
	filePath := c.Params("*")
	if filePath == "" {
		return c.SendStatus(404)
	}

	cleaned := path.Clean(filePath)
	if strings.Contains(cleaned, "..") {
		return c.SendStatus(403)
	}

	fsys := getFileSystem()
	data, err := fs.ReadFile(fsys, "assets/"+cleaned)
	if err != nil {
		return c.SendStatus(404)
	}

	ext := path.Ext(cleaned)
	if ext != "" {
		c.Type(strings.TrimPrefix(ext, "."))
	}

	return c.Send(data)
}

func spaFallbackHandler(c fiber.Ctx) error {
	reqPath := c.Path()
	cleaned := path.Clean(reqPath)
	if strings.Contains(cleaned, "..") {
		return c.SendStatus(403)
	}

	if cleaned != "/" && cleaned != "" {
		fileName := strings.TrimPrefix(cleaned, "/")
		fsys := getFileSystem()
		data, err := fs.ReadFile(fsys, fileName)
		if err == nil {
			ext := path.Ext(fileName)
			if ext != "" {
				c.Type(strings.TrimPrefix(ext, "."))
			}
			return c.Send(data)
		}
	}

	fsys := getFileSystem()
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return c.Status(500).SendString("index.html not found")
	}

	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(data)
}
