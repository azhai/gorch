# gorch Makefile
#
# Two binaries are produced from the same source (./cmd/orchard):
#   gorch       — Echo HTTP server + the embedded Mithril SPA from $(ADMIN_DIR)
#   orchard     — built with -tags noweb: no HTTP server, no embedded SPA
#
# Layout:
#   cmd/orchard — gorch CLI/supervisor (package main)
#   cmd/weblite — static file server
#   server/     — Go HTTP layer (Echo, REST API, SSE, auth)
#   web/        — Mithril SPA (embedded as github.com/azhai/gorch/web)

# ── Project config ────────────────────────────────────────

# main package source directory
APP_DIR   = ./cmd/orchard
# binary with the web UI baked in
APP       = gorch
# binary without the web UI
APP_LITE  = orchard
# build tag that strips the web layer (server/ + embedded SPA)
NOWEB_TAG = noweb
# frontend directory
ADMIN_DIR = web
# auto-discover cmd/ subdirs, excluding the main app directory
COMMANDS  := $(filter-out orchard,$(notdir $(patsubst %/.,%,$(wildcard cmd/*/.))))

# ── Build flags ───────────────────────────────────────────

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
RELEASE    = -ldflags "-s -w -X main.version=$(VERSION)"
GOBUILD    = go build $(RELEASE)

# ── Targets ───────────────────────────────────────────────

.PHONY: one noweb all front build run dev clean test lint tidy

## one: local single-platform build (CGO disabled, current OS/ARCH)
one: front
	@echo "Build $(APP) + $(APP_LITE) (local) ..."
	mkdir -p bin
	CGO_ENABLED=0 $(GOBUILD) -o bin/$(APP) $(APP_DIR)
	CGO_ENABLED=0 $(GOBUILD) -tags $(NOWEB_TAG) -o bin/$(APP_LITE) $(APP_DIR)
	@for cmd in $(COMMANDS); do \
		CGO_ENABLED=0 $(GOBUILD) -o bin/$$cmd ./cmd/$$cmd; \
	done

## noweb: build only the web-less gorch (skips the frontend build)
noweb:
	@echo "Build $(APP_LITE) (local, -tags $(NOWEB_TAG)) ..."
	mkdir -p bin
	CGO_ENABLED=0 $(GOBUILD) -tags $(NOWEB_TAG) -o bin/$(APP_LITE) $(APP_DIR)

## all: full pipeline — clean, then local build, then cross-compile
all: clean one build

## build: cross-compile gorch, gorch-noweb, and all COMMANDS
build: front
	@echo "Cross-compiling ..."
	mkdir -p bin
	@for target in $(APP) $(APP_LITE); do \
		tags=""; \
		if [ "$$target" = "$(APP_LITE)" ]; then tags="-tags $(NOWEB_TAG)"; fi; \
		CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) $$tags -o bin/$$target-$(VERSION).darwin-arm64 $(APP_DIR) && \
		CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GOBUILD) $$tags -o bin/$$target-$(VERSION).darwin-amd64 $(APP_DIR) && \
		CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 $(GOBUILD) $$tags -o bin/$$target-$(VERSION).linux-arm64  $(APP_DIR) && \
		CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 $(GOBUILD) $$tags -o bin/$$target-$(VERSION).linux-amd64  $(APP_DIR); \
	done
	@for cmd in $(COMMANDS); do \
		CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 $(GOBUILD) -o bin/$$cmd-$(VERSION).darwin-arm64 ./cmd/$$cmd && \
		CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 $(GOBUILD) -o bin/$$cmd-$(VERSION).darwin-amd64 ./cmd/$$cmd && \
		CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 $(GOBUILD) -o bin/$$cmd-$(VERSION).linux-arm64  ./cmd/$$cmd && \
		CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 $(GOBUILD) -o bin/$$cmd-$(VERSION).linux-amd64  ./cmd/$$cmd && \
		CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) -o bin/$$cmd-$(VERSION).exe          ./cmd/$$cmd; \
	done
	@echo "✅ Build success."

## front: build frontend assets (tsc type-check + vite build)
front:
	@echo "Build frontend ..."
	cd $(ADMIN_DIR) && npm run build

## run: build frontend then run gorch with go run
run: front
	go run $(APP_DIR)

## dev: start hot-reload backend (air) + frontend dev server (vite)
dev:
	@which air > /dev/null 2>&1 || go install github.com/air-verse/air@latest
	@if [ ! -d "$(ADMIN_DIR)/node_modules" ]; then cd $(ADMIN_DIR) && npm install; fi
	(cd $(ADMIN_DIR) && npm run dev &)
	air

## clean: remove build artifacts and frontend dist
clean:
	rm -rf bin/* tmp/* $(ADMIN_DIR)/dist
	@echo "✅ Clean complete."

## test: run Go tests (both build variants must compile)
test:
	go test ./... -v
	go vet -tags $(NOWEB_TAG) ./...

## lint: run golangci-lint on entire project
lint:
	golangci-lint run ./...

## tidy: clean up go.mod dependencies
tidy:
	go mod tidy
