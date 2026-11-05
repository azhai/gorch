# gorch — Makefile
#
# Odin project (odin >= dev-2026-10). Run `make deps` once after cloning: the
# state cache comes from the oboe submodule in vendor/oboe.
#
#   make one     local build, with the source locations a debugger wants
#   make run     dev loop: build locally, then run it
#   make all     release matrix (4 targets) + bin/SHA256SUMS
#   make test    every package that owns a *_test.odin file
#   make clean   drop bin/ and tmp/

# ── Project config ───────────────────────────────────────

APP_PKG  = src/cmd/gorch
APP      = gorch
BIN      = bin
ODIN     = odin
MIN_ODIN = dev-2026-10

# oboe (git submodule) supplies the runtime state cache: an in-memory key/value
# store with an atomic, compressed snapshot on disk, used instead of SQLite or
# pid files. `make deps` fetches it.
OBOE_DIR ?= vendor/oboe

# packages that own at least one *_test.odin file
TEST_PKGS := $(sort $(patsubst %/,%,$(dir $(wildcard src/lib/*/*_test.odin))))

# Release matrix. macOS Big Sur and later, which is also the first release with
# Apple Silicon; Linux binaries are static musl, so they carry no glibc
# dependency and run on any kernel of the last few years.
TARGETS   ?= darwin_arm64 darwin_amd64 linux_arm64 linux_amd64
MACOS_MIN ?= 11.0.0

# Odin cannot link a foreign target on this host, so linux targets are compiled
# to an object and linked by a cross toolchain - zig cc with static musl, which
# needs nothing installed on the target machine. Override for CI:
#   make all TARGETS="linux_amd64" CROSS_CC="x86_64-linux-musl-gcc"
ZIG      ?= zig
CROSS_CC ?= $(ZIG) cc

# vendor:zlib only declares zlib's C entry points - the implementation comes
# from the system. A static musl target has no such library, and the state
# snapshot compressed by oboe goes through it, so the Linux link needs these.
ZLIB_DIR  = third_party/zlib
ZLIB_SRCS = adler32.c compress.c crc32.c deflate.c infback.c inffast.c \
            inflate.c inftrees.c trees.c uncompr.c zutil.c

# ── Build flags ──────────────────────────────────────────

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# -o:size trades a little speed for a lot of text: it is the difference between
# a 1.3 MB binary and a 600 KB one, and this program waits on processes rather
# than on arithmetic.
#
# DEFAULT_TEMP_ALLOCATOR_BACKING_SIZE is Odin's per-thread temp arena block, 4
# MiB by default. Every thread that formats a string once takes a whole block,
# and the arena never gives it back; 64 KiB is plenty for the paths that use it
# and keeps a thread's floor at a few pages instead of four megabytes.
ODIN_DEPS  = -collection:oboe=$(OBOE_DIR)
ODIN_FLAGS = -o:size $(ODIN_DEPS) -define:DEFAULT_TEMP_ALLOCATOR_BACKING_SIZE=65536

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

# shasum ships with macOS and takes "-a 256"; sha256sum ships with Linux.
ifneq ($(shell command -v shasum 2>/dev/null),)
SHASUM = shasum -a 256
else
SHASUM = sha256sum
endif

# ── Targets ──────────────────────────────────────────────

.PHONY: all one run test clean deps assets check-deps odin-version

## deps: fetch the oboe submodule (once, before the first build)
deps:
	git submodule update --init --recursive
	@echo "✅ deps ready"

## check-deps: fail fast with a useful message when the submodule is missing
check-deps:
	@test -f $(OBOE_DIR)/cache/cache.odin || { \
		echo "vendor/oboe is missing; run 'make deps' first" >&2; exit 1; \
	}

## odin-version: refuse to build with an older toolchain
odin-version:
	@ver=$$($(ODIN) version 2>/dev/null | head -1 | awk '{print $$NF}' | cut -d: -f1); \
	if [ -z "$$ver" ]; then echo "odin $(MIN_ODIN)+ is required but $(ODIN) is not in PATH" >&2; exit 1; fi; \
	if [ "$$(printf '%s\n%s\n' '$(MIN_ODIN)' "$$ver" | sort -V | head -1)" != "$(MIN_ODIN)" ]; then \
		echo "gorch needs odin >= $(MIN_ODIN), found $$ver" >&2; exit 1; \
	fi; \
	echo "odin $$ver ok"

## assets: deflate web/ into the blob the binary embeds
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

## one: local build, with the source locations a developer wants
one: odin-version check-deps assets
	@echo "Build $(APP) (local, $(VERSION)) ..."
	@mkdir -p $(BIN)
	@$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) -out:$(BIN)/$(APP)
	@printf "  %s: %s bytes\n" $(BIN)/$(APP) "$$(wc -c < $(BIN)/$(APP) | tr -d ' ')"

## run: dev loop - build locally, then run it
#
#   make run                           # ./gorch.toml in the cwd
#   make run ARGS="-c /tmp/test.toml"  # or any other flags
run: one
	@$(BIN)/$(APP) $(ARGS)

## all: local build + the four-target release matrix + bin/SHA256SUMS
all: odin-version check-deps assets
	@rm -rf $(BIN)/* tmp/*
	@mkdir -p $(BIN)
	@echo "Build $(APP) $(VERSION) ..."
	@$(ODIN) build $(APP_PKG) $(ODIN_FLAGS) -out:$(BIN)/$(APP)
	@printf "  %-46s %8s bytes\n" $(BIN)/$(APP) "$$(wc -c < $(BIN)/$(APP) | tr -d ' ')"
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
	@cd $(BIN) && $(SHASUM) $(APP)-$(VERSION)-* > SHA256SUMS
	@echo
	@cat $(BIN)/SHA256SUMS

## test: run the tests of every package that has them
test: check-deps
	@for pkg in $(TEST_PKGS); do \
		echo "--- $$pkg" && $(ODIN) test $$pkg $(ODIN_DEPS) || exit 1; \
	done

## clean: remove build artifacts
clean:
	rm -rf $(BIN)/* tmp/*
	@echo "✅ Clean complete."
