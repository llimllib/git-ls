#!/usr/bin/env bash
#
# Tag and push a release. GoReleaser runs in GitHub Actions.
# Make sure you update VERSION in main.go first.
#
# https://goreleaser.com/quick-start/

version=$(grep 'VERSION =' main.go | grep -Eo "[[:digit:]]+\.[[:digit:]]+\.[[:digit:]]+")
git tag -a "v$version" -m "v$version"
git push origin "v$version"
