# zlib 1.3.1 (vendored)

`vendor:zlib` in the Odin distribution is a binding, not an implementation: it
declares zlib's C entry points and leaves them to `system:z`. macOS and glibc
Linux have that library; a static musl target does not, and the state snapshot
in `vendor/oboe` compresses through it, so a Linux release has nothing to link
against.

These are the upstream 1.3.1 sources, unmodified, cut down to the files
`compress2`, `compressBound` and `uncompress` need. `make build` compiles them
with the same cross compiler that links the rest of the binary.

Nothing here is patched. To update, unpack a newer zlib release over this
directory and keep the same file list; `LICENSE` is upstream's.
