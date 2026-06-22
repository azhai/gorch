# gorch Makefile
#
# Targets:
#   make one     — local build (APP + all COMMANDS)
#   make build   — cross-compile for darwin/linux/windows (arm64/amd64)
#   make all     — clean → one → build (full pipeline)
#   make front   — build frontend only
#   make run     — build frontend + go run
#   make dev     — hot-reload dev server (air + vite dev)
#   make clean   — remove build artifacts
#   make test    — run Go tests
#   make lint    — run golangci-lint
#   make tidy    — run go mod tidy
#
# Test:
#   ./test_makefile.sh

# ── Project config ────────────────────────────────────────

# main binary name (built from ./)
APP       = gorch
# frontend directory
ADMIN_DIR = webui
# auto-discover cmd/ subdirs
COMMANDS  := $(notdir $(patsubst %/.,%,$(wildcard cmd/*/.)))

# ── Build flags ───────────────────────────────────────────

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
RELEASE    = -ldflags "-s -w -X main.version=$(VERSION)"
GOBUILD    = go build $(RELEASE)

# ── Targets ───────────────────────────────────────────────

.PHONY: one all front build run dev clean test lint tidy

## one: local single-platform build (CGO disabled, current OS/ARCH)
one: front
	@echo "Build $(APP) (local) ..."
	mkdir -p bin
	CGO_ENABLED=0 $(GOBUILD) -o bin/$(APP) ./
	@for cmd in $(COMMANDS); do \
		CGO_ENABLED=0 $(GOBUILD) -o bin/$$cmd ./cmd/$$cmd; \
	done

## all: full pipeline — clean, then local build, then cross-compile
all: clean one build

## build: cross-compile APP + all COMMANDS for 5 platforms
build: front
	@echo "Cross-compiling ..."
	mkdir -p bin
	@for target in $(APP) $(COMMANDS); do \
		if [ "$$target" = "$(APP)" ]; then src=.; else src=./cmd/$$target; fi; \
		CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) -o bin/$$target-$(VERSION).darwin-arm64 $$src && \
		CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GOBUILD) -o bin/$$target-$(VERSION).darwin-amd64 $$src && \
		CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 $(GOBUILD) -o bin/$$target-$(VERSION).linux-arm64  $$src && \
		CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 $(GOBUILD) -o bin/$$target-$(VERSION).linux-amd64  $$src; \
	done
	@echo "✅ Build success."

## front: build frontend assets (tsc type-check + vite build)
front:
	@echo "Build frontend ..."
	cd $(ADMIN_DIR) && npm run build

## run: build frontend then run main with go run
run: front
	go run ./

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

## test: run Go tests with verbose output (52 tests across 5 packages)
test:
	go test ./... -v

## lint: run golangci-lint on entire project
lint:
	golangci-lint run ./...

## tidy: clean up go.mod dependencies
tidy:
	go mod tidy
