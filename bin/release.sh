#!/usr/bin/env bash
#
# Use goreleaser to create a release. make sure you update VERSION in main.go
# first
#
# https://goreleaser.com/quick-start/

version=$(grep 'VERSION =' main.go | grep -Eo "[[:digit:]]+\.[[:digit:]]+\.[[:digit:]]+")
git tag -a "v$version" -m "v$version"
git push origin "v$version"
goreleaser release --clean
