SRC := $(shell git ls-files '*.go')

.PHONY: all
all: git-ls

git-ls: $(SRC)
	go build

# depends on golangci-lint:
# https://golangci-lint.run/welcome/install/#local-installation
lint:
	golangci-lint run
