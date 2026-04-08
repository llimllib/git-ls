SRC := $(shell git ls-files '*.go')
TRANSCRIPTS := $(wildcard transcripts/*)
STATIC := $(wildcard static/*)

.PHONY: all
all: git-ls

git-ls: $(SRC)
	go build

# depends on golangci-lint:
# https://golangci-lint.run/welcome/install/#local-installation
.PHONY: lint
lint:
	@if [ -n "$$(go fmt ./...)" ]; then \
		echo "Error: The following files were reformatted. Please commit the changes:"; \
		go fmt ./...; \
		exit 1; \
	fi
	golangci-lint run

.PHONY: publish
publish:
	make lint && go test && bin/release.sh

_site/index.html: README.md $(TRANSCRIPTS) $(STATIC)
	@./tools/build-site.sh

.PHONY: serve-site
serve-site: _site/index.html
	devd -ol ./_site/

