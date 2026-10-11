#!/bin/sh
# odin check a package, hiding the expected "entry point main" + stack-size warnings
odin check "$@" 2>&1 | grep -vE "entry point procedure 'main'|may cause a stack overflow|buf: \[|Uncomment|^\s*\|| \^ |Did you mean" 
