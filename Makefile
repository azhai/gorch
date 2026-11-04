# gorch Makefile
#
# Odin project (requires odin >= dev-2026-10):
#   src/cmd/gorch — supervisor/CLI entry point (package main)
#   src/lib/*     — support packages; the ones with *_test.odin run under `odin test`
#   tools/embedgen — turns web/ into the blob embedded in the binary
#   web/          — Alpine SPA (Material 3 Monet theme), served from disk if present
#
# The state cache comes from the oboe submodule (vendor/oboe), wired in through
# -collection:oboe; run `make deps` once after cloning.
#
# The frontend has no build step of its own - Alpine is vendored in web/vendor/
# - but it is compressed into the binary: `make assets` gzips web/ to about a
# quarter of its size and writes the table that indexes it. A web/ directory
# next to the binary or the config still wins over that copy.

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

# The release matrix: both operating systems, both architectures.
#
# Only the versions that still receive updates are supported. On macOS that
# means Big Sur and later, which is also the first release with Apple Silicon;
# on Linux the binaries are static musl, so they carry no glibc dependency and
# run on any kernel of the last few years.
TARGETS   ?= darwin_arm64 darwin_amd64 linux_arm64 linux_amd64
MACOS_MIN ?= 11.0.0

# Linux/Windows cannot be linked by Odin itself on this host ("Linking for cross
# compilation for this platform is not yet supported"), so foreign targets are
# compiled to objects and linked by a cross toolchain instead — zig cc with
# static musl, which needs nothing installed on the target machine. Override for
# CI: make build TARGETS="linux_amd64" CROSS_CC="x86_64-linux-musl-gcc"
ZIG      ?= zig
CROSS_CC ?= $(ZIG) cc

# vendor:zlib only declares zlib's C entry points - the implementation comes
# from the system. A static musl target has no such library, and the state
# snapshot compressed by oboe goes through it, so the Linux link needs these.
ZLIB_DIR  = third_party/zlib
ZLIB_SRCS = adler32.c compress.c crc32.c deflate.c infback.c inffast.c \
            inflate.c inftrees.c trees.c uncompr.c zutil.c

# ── Build flags ───────────────────────────────────────────

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# -o:size trades a little speed for a lot of text: it is the difference between
# a 1.3 MB binary and a 600 KB one, and this program waits on processes rather
# than on arithmetic.
ODIN_FLAGS = -o:size -collection:oboe=$(OBOE_DIR)

# Release builds drop the file and line table Odin keeps for panic messages:
# 50 KB for something only a developer with symbols can use. `make one` keeps
# those, so a local crash still says where it happened.
RELEASE_FLAGS = -source-code-locations:none

# the compressed frontend and the table that indexes it
WEB_DIR      = web
EMBED_BLOB   = tmp/assets.blob
ASSETS_BIN   = src/lib/web/assets.bin
ASSETS_TABLE = src/lib/web/assets_gen.odin
WEB_FILES   := $(shell find $(WEB_DIR) -type f ! -name '.*')

# ARM64 micro-architecture baseline for the `arm64` target.
#   v8.0 (default) — runs on every ARM64 chip; the Go runtime still dispatches
#                    to AES/SHA/CRC32/LSE at runtime when the CPU exposes them.
#   v9.0           — emits ARMv9 instructions (post-2021 cores only: Cortex-X2/
#                    A710+, Neoverse N2/V2, AWS Graviton4, Apple M3 …).
GOARM64   ?= v8.0
# `make release` additionally packs the Linux binaries with UPX (upx --brute),
# writing gorch-<ver>-linux-<arch>.upx next to each plain one.

# ── Targets ───────────────────────────────────────────────

.PHONY: all assets odin-version deps one build release upx-linux linux run test check clean

## deps: fetch the oboe submodule (needed before the first build)
deps:
	git submodule update --init --recursive
	@echo "✅ deps ready"

## check-deps: fail fast with a useful message when the submodule is missing
check-deps:
	@test -f $(OBOE_DIR)/cache/cache.odin || { \
		echo "vendor/oboe is missing; run 'make deps' first" >&2; exit 1; \
	}

## assets: compress web/ into the blob the binary embeds
#
# gzip writes a container (10-byte header, deflate stream, 8-byte trailer) and
# the runtime inflates raw deflate, so this strips the wrapper with dd. Keeping
# the compressor outside the build tool is deliberate: gzip is everywhere, and
# embedgen stays a plain file layout with no dependency beyond the stdlib.
assets: $(ASSETS_BIN)

$(ASSETS_BIN): $(WEB_FILES) tools/embedgen/main.odin
	@mkdir -p tmp
	@$(ODIN) run tools/embedgen -- -root $(WEB_DIR) -blob $(EMBED_BLOB) -table $(ASSETS_TABLE)
	@gzip -9 -nc $(EMBED_BLOB) > tmp/assets.gz
	@size=$$(wc -c < tmp/assets.gz | tr -d ' '); \
		dd if=tmp/assets.gz of=$(ASSETS_BIN) bs=1 skip=10 count=$$((size - 18)) 2>/dev/null
	@printf "  %s: %s bytes of raw deflate\n" $(ASSETS_BIN) "$$(wc -c < $(ASSETS_BIN) | tr -d ' ')"

## all: full pipeline — clean, local build, then the release matrix
all: clean one build

## odin-version: refuse to build with an older toolchain
odin-version:
	@ver=$$($(ODIN) version 2>/dev/null | head -1 | awk '{print $$NF}' | cut -d: -f1); \
	if [ -z "$$ver" ]; then echo "odin $(MIN_ODIN)+ is required but $(ODIN) is not in PATH" >&2; exit 1; fi; \
	if [ "$$(printf '%s\n%s\n' '$(MIN_ODIN)' "$$ver" | sort -V | head -1)" != "$(MIN_ODIN)" ]; then \
		echo "gorch needs odin >= $(MIN_ODIN), found $$ver" >&2; exit 1; \
	fi; \
	echo "odin $$ver ok"

## one: local build, with the source locations a developer wants
one: odin-version check-deps assets
	@echo "Build $(APP) (local) ..."
	@mkdir -p $(BIN)
	$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) -out:$(BIN)/$(APP)
	@printf "  %s: %s bytes\n" $(BIN)/$(APP) "$$(wc -c < $(BIN)/$(APP) | tr -d ' ')"

## build: compile every release TARGET
build: odin-version check-deps assets
	@echo "Compiling $(TARGETS) as $(VERSION) ..."
	@mkdir -p $(BIN)
	@for target in $(TARGETS); do \
		os=$${target%_*}; arch=$${target#*_}; \
		out=$(BIN)/$(APP)-$(VERSION)-$$os-$$arch; \
		case $$os in \
		darwin) \
			$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) $(RELEASE_FLAGS) \
				-target:$$target -minimum-os-version:$(MACOS_MIN) -out:$$out || exit 1 ;; \
		linux) \
			case $$arch in \
				amd64) triple=x86_64-linux-musl ;; \
				arm64) triple=aarch64-linux-musl ;; \
				*) echo "no musl triple for $$os/$$arch" >&2; exit 1 ;; \
			esac; \
			objs=$$(mktemp -d); \
			$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) $(RELEASE_FLAGS) \
				-target:$$target -build-mode:object -out:$$objs/app.o || exit 1; \
			for src in $(ZLIB_SRCS); do \
				$(CROSS_CC) -target $$triple -Os -DHAVE_UNISTD_H -I$(ZLIB_DIR) \
					-c $(ZLIB_DIR)/$$src -o $$objs/z_$${src%.c}.o || exit 1; \
			done; \
			$(CROSS_CC) -target $$triple -static -Os -Wl,--gc-sections -Wl,--strip-all \
				$$objs/*.o -o $$out -lpthread -ldl -lm || exit 1; \
			rm -rf $$objs ;; \
		*) echo "no rule for $$os" >&2; exit 1 ;; \
		esac; \
		printf "  %-46s %8s bytes\n" $$out "$$(wc -c < $$out | tr -d ' ')"; \
	done
	@cd $(BIN) && if command -v shasum >/dev/null; then shasum -a 256 $(APP)-$(VERSION)-*; \
		else sha256sum $(APP)-$(VERSION)-*; fi > SHA256SUMS
	@echo "✅ Build success."

## upx-linux: additionally pack the Linux release binaries with UPX --brute
#
# UPX decompresses transparently at load time, so a .upx file is a drop-in
# replacement for the plain one - just smaller. The original is kept too.
upx-linux:
	@for f in $(BIN)/$(APP)-$(VERSION)-linux-*; do \
		[ -f "$$f" ] || continue; \
		if command -v upx >/dev/null 2>&1; then \
			echo "UPX $$f ..."; \
			upx --brute -o "$$f.upx" "$$f" || { echo "  upx failed on $$f, skipped" >&2; continue; }; \
			printf "  %-46s %8s bytes\n" "$$f.upx" "$$(wc -c < $$f.upx | tr -d ' ')"; \
		else \
			echo "upx not found; skipping compression of $$f" >&2; \
		fi; \
	done

## release: build the matrix, pack the Linux binaries, and print what it produced
release: build upx-linux
	@cd $(BIN) && if command -v shasum >/dev/null; then shasum -a 256 $(APP)-$(VERSION)-*; \
		else sha256sum $(APP)-$(VERSION)-*; fi > SHA256SUMS
	@echo
	@cat $(BIN)/SHA256SUMS

## linux: static musl release for both Linux architectures
linux: odin-version
	@$(MAKE) --no-print-directory build TARGETS="linux_amd64 linux_arm64"

## run: build and run the supervisor
run: odin-version assets
	$(ODIN) run $(APP_PKG) -collection:oboe=$(OBOE_DIR)

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
