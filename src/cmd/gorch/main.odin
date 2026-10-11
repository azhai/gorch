package main

import p "../../lib/paths"
import l "../../lib/log"

main :: proc() {
	l.init()
	l.info("startup", "runtime_dir", p.RuntimeDir)
	assert(p.ensure_runtime_dir() == nil)
	l.info("logs at", "dir", p.DefaultLogDir)
}
