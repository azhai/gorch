# gorch Makefile
#
# Odin project (requires odin >= dev-2026-10):
#   src/cmd/gorch — supervisor/CLI entry point (package main)
#   src/lib/*     — support packages; the ones with *_test.odin run under `odin test`
#   web/          — Alpine SPA (Material 3 Monet theme), served straight from disk
#
# The frontend has no build step — Alpine is vendored in web/vendor/ — so no
# target below touches web/; the server embeds or serves that directory as-is.

# ── Project config ────────────────────────────────────────

# entry point package directory
APP_PKG = src/cmd/gorch
# binary name
APP     = gorch
# binary output directory
BIN     = bin
# Odin toolchain and the minimum version this project is written against
ODIN     = odin
MIN_ODIN = dev-2026-10

# packages that own at least one *_test.odin file
TEST_PKGS := $(sort $(patsubst %/,%,$(dir $(wildcard src/lib/*/*_test.odin))))

# Odin cannot cross-link Linux/Windows from macOS, so the release matrix defaults
# to this OS's architectures; build elsewhere or override:
#   make build TARGETS="linux_amd64 linux_arm64"
TARGETS ?= darwin_arm64 darwin_amd64

# ── Build flags ───────────────────────────────────────────

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
# release flags: light optimisation and no -debug (Odin's defaults, stated explicitly)
ODIN_FLAGS  = -o:minimal

# ── Targets ───────────────────────────────────────────────

.PHONY: all odin-version one build run test check clean

## all: full pipeline — clean, then local build, then the release matrix
all: clean one build

## odin-version: refuse to build with an older toolchain
odin-version:
	@ver=$$($(ODIN) version 2>/dev/null | head -1 | awk '{print $$NF}' | cut -d: -f1); \
	if [ -z "$$ver" ]; then echo "odin $(MIN_ODIN)+ is required but $(ODIN) is not in PATH" >&2; exit 1; fi; \
	if [ "$$(printf '%s\n%s\n' '$(MIN_ODIN)' "$$ver" | sort -V | head -1)" != "$(MIN_ODIN)" ]; then \
		echo "gorch needs odin >= $(MIN_ODIN), found $$ver" >&2; exit 1; \
	fi; \
	echo "odin $$ver ok"

## one: local single-platform build
one: odin-version
	@echo "Build $(APP) (local) ..."
	mkdir -p $(BIN)
	$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) -out:$(BIN)/$(APP)

## build: compile every release TARGET
build: odin-version
	@echo "Compiling $(TARGETS) ..."
	mkdir -p $(BIN)
	@for target in $(TARGETS); do \
		os=$${target%_*}; arch=$${target#*_}; \
		$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) -target:$$target -out:$(BIN)/$(APP)-$(VERSION).$$os-$$arch || exit 1; \
	done
	@echo "✅ Build success."

## run: build and run the supervisor
run: odin-version
	$(ODIN) run $(APP_PKG)

## test: run the tests of every package that has them
test:
	@for pkg in $(TEST_PKGS); do \
		echo "--- $$pkg" && $(ODIN) test $$pkg || exit 1; \
	done

## check: type check every package; `odin check` errors other than the missing
#  entry point (library packages have no main) fail the target
check:
	@fail=0; for pkg in $(sort $(dir $(wildcard src/cmd/*/*.odin src/lib/*/*.odin))); do \
		errs=$$($(ODIN) check $$pkg 2>&1 | grep 'Error:' | grep -v "Undefined entry point procedure 'main'"); \
		if [ -n "$$errs" ]; then echo "--- $$pkg"; echo "$$errs"; fail=1; fi; \
	done; \
	if [ $$fail -eq 1 ]; then echo "❌ check failed" >&2; exit 1; fi; \
	echo "✅ check clean."

## clean: remove build artifacts
clean:
	rm -rf $(BIN)/* tmp/*
	@echo "✅ Clean complete."
