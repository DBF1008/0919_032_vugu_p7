#!/bin/sh
#
# test.sh - manually run the Go unit test suites for the repository.
#
# Usage:
#   ./test.sh                 # run the common test packages (see below)
#   ./test.sh ./devutil       # run tests for one or more specific packages
#   ./test.sh -v ./devutil    # extra flags are passed straight through to go test
#
# The script keeps the Go build cache out of the system directory so it
# also works in restricted sandbox environments.

set -e

cd "$(dirname "$0")"

export GOCACHE="${GOCACHE:-/tmp/gobuild_vugu}"
mkdir -p "$GOCACHE"

if [ "$#" -eq 0 ]; then
	# Default set of packages with ordinary unit tests. Packages that
	# require a browser/docker (wasm-test-suite) are intentionally
	# excluded; run them explicitly when the environment supports them.
	set -- . ./devutil ./distutil ./domrender ./gen ./js ./staticrender ./simplehttp ./vgform
fi

echo ">> go test $*"
go test "$@"

echo ">> all requested unit tests passed"
