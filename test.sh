#!/bin/sh
# test.sh - run all Go unit tests for the vugu repository.
#
# Mirrors `mage test`: runs `go test` on every package except the
# wasm-test-suite and legacy-wasm-test-suite packages, which require
# Docker (nginx + chromedp containers). Use `mage testWasm` /
# `mage testLegacyWasm` for those.
set -e

cd "$(dirname "$0")"

# shellcheck disable=SC2046
go test $(go list ./... | grep -v wasm-test-suite)
