# gorch Makefile
#
# Odin project (requires odin >= dev-2026-10):
#   src/cmd/gorch — supervisor/CLI entry point (package main)
#   src/lib/*     — support packages; the ones with *_test.odin run under `odin test`
#   web/          — Alpine SPA (Material 3 Monet theme), served straight from disk
#
# The state cache comes from the oboe submodule (vendor/oboe), wired in through
# -collection:oboe; run `make deps` once after cloning.
#
# The frontend has no build step — Alpine is vendored in web/vendor/ — and no
# target below touches it. web/ is baked into the binary at compile time
# (src/lib/web/embed.odin), so `make one` is enough to update a deployed copy;
# a web/ directory next to the binary or the config wins over the embedded one.

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

# oboe (git submodule) supplies the runtime state cache: an in-memory key/value
# store with an atomic, compressed snapshot on disk. gorch uses it instead of
# SQLite or pid files. `make deps` fetches it.
OBOE_DIR ?= vendor/oboe

# packages that own at least one *_test.odin file
TEST_PKGS := $(sort $(patsubst %/,%,$(dir $(wildcard src/lib/*/*_test.odin))))

# Linux/Windows cannot be linked by Odin itself on this host ("Linking for cross
# compilation for this platform is not yet supported"), so foreign targets are
# compiled to objects and linked by a cross toolchain instead — zig cc with static
# musl, which needs nothing installed on the target machine. Override for CI:
#   make build TARGETS="linux_amd64" CROSS_CC="x86_64-linux-musl-gcc"
TARGETS ?= darwin_arm64 darwin_amd64
# the cross linker used for non-darwin targets
ZIG      ?= zig
CROSS_CC ?= $(ZIG) cc

# ── Build flags ───────────────────────────────────────────

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
# release flags: light optimisation and no -debug (Odin's defaults, stated explicitly)
# the collection makes `import "oboe:cache"` resolve inside the submodule
ODIN_FLAGS  = -o:minimal -collection:oboe=$(OBOE_DIR)

# ── Targets ───────────────────────────────────────────────

.PHONY: all odin-version deps one build linux run test check clean

## deps: fetch the oboe submodule (needed before the first build)
deps:
	git submodule update --init --recursive
	@echo "✅ deps ready"

## check-deps: fail fast with a useful message when the submodule is missing
check-deps:
	@test -f $(OBOE_DIR)/cache/cache.odin || { \
		echo "vendor/oboe is missing; run 'make deps' first" >&2; exit 1; \
	}

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
one: odin-version check-deps
	@echo "Build $(APP) (local) ..."
	mkdir -p $(BIN)
	$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) -out:$(BIN)/$(APP)

## build: compile every release TARGET
build: odin-version check-deps
	@echo "Compiling $(TARGETS) ..."
	mkdir -p $(BIN)
	@for target in $(TARGETS); do \
		os=$${target%_*}; arch=$${target#*_}; \
		out=$(BIN)/$(APP)-$(VERSION).$$os-$$arch; \
		if [ "$$os" = darwin ]; then \
			$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) -target:$$target -out:$$out || exit 1; \
			continue; \
		fi; \
		case $$arch in \
			amd64) triple=x86_64-linux-musl ;; \
			arm64) triple=aarch64-linux-musl ;; \
			*) echo "no musl triple for $$os/$$arch" >&2; exit 1 ;; \
		esac; \
		objs=$$(mktemp -d); \
		$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) -target:$$target -build-mode:object -out:$$objs/app.o || exit 1; \
		$(CROSS_CC) -target $$triple -static -Wl,--strip-all $$objs/*.o -o $$out -lpthread -ldl -lm || exit 1; \
		rm -rf $$objs; \
		echo "  $$out ($$triple, static)"; \
	done
	@echo "✅ Build success."

## linux: static musl release for both Linux architectures
linux: odin-version
	@$(MAKE) --no-print-directory build TARGETS="linux_amd64 linux_arm64"

## run: build and run the supervisor
run: odin-version
	$(ODIN) run $(APP_PKG)

## test: run the tests of every package that has them
test: check-deps
	@for pkg in $(TEST_PKGS); do \
		echo "--- $$pkg" && $(ODIN) test $$pkg -collection:oboe=$(OBOE_DIR) || exit 1; \
	done

## check: type check every package; `odin check` errors other than the missing
#  entry point (library packages have no main) fail the target
check: check-deps
	@fail=0; for pkg in $(sort $(dir $(wildcard src/cmd/*/*.odin src/lib/*/*.odin))); do \
		errs=$$($(ODIN) check $$pkg -collection:oboe=$(OBOE_DIR) 2>&1 | grep 'Error:' | grep -v "Undefined entry point procedure 'main'"); \
		if [ -n "$$errs" ]; then echo "--- $$pkg"; echo "$$errs"; fail=1; fi; \
	done; \
	if [ $$fail -eq 1 ]; then echo "❌ check failed" >&2; exit 1; fi; \
	echo "✅ check clean."

## clean: remove build artifacts
clean:
	rm -rf $(BIN)/* tmp/*
	@echo "✅ Clean complete."
