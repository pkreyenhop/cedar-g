# Makefile for cedar-g — Cedar/Mesa TiogaViewer (Go) and the archive downloaders.

GO      ?= go
BIN_DIR := bin
CMDS    := tiogaviewer xeroxdl xeroxsrc
BINS    := $(addprefix $(BIN_DIR)/,$(CMDS))

# Args passed to `make run` / `make download*`, e.g. `make run ARGS=download`.
ARGS ?=

.DEFAULT_GOAL := build

## build: compile all binaries into ./bin
.PHONY: build
build: $(BINS)

# Pattern rule: bin/<name> is built from ./cmd/<name>.
$(BIN_DIR)/%: FORCE
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $@ ./cmd/$*

# Convenience aliases for individual binaries.
.PHONY: viewer downloader source-downloader
viewer: $(BIN_DIR)/tiogaviewer            ## build just the GUI viewer
downloader: $(BIN_DIR)/xeroxdl            ## build just the full downloader
source-downloader: $(BIN_DIR)/xeroxsrc   ## build just the source-only downloader

## test: run the full test suite
.PHONY: test
test:
	$(GO) test ./...

## vet: run go vet
.PHONY: vet
vet:
	$(GO) vet ./...

## fmt: format all Go sources
.PHONY: fmt
fmt:
	$(GO) fmt ./...

## tidy: sync go.mod / go.sum
.PHONY: tidy
tidy:
	$(GO) mod tidy

## check: fmt, vet and test in one go
.PHONY: check
check: fmt vet test

## run: build and run the viewer (make run ARGS=/path/to/tree)
.PHONY: run
run: $(BIN_DIR)/tiogaviewer
	./$(BIN_DIR)/tiogaviewer $(ARGS)

## download: mirror the whole archive into ./download (ARGS forwarded)
.PHONY: download
download: $(BIN_DIR)/xeroxdl
	./$(BIN_DIR)/xeroxdl $(ARGS)

## download-src: mirror only *.mesa/*.tioga sources into ./download-src
.PHONY: download-src
download-src: $(BIN_DIR)/xeroxsrc
	./$(BIN_DIR)/xeroxsrc $(ARGS)

## clean: remove built binaries (from ./bin and any stray copies in the root)
.PHONY: clean
clean:
	rm -rf $(BIN_DIR)
	rm -f $(CMDS)

## distclean: also remove downloaded mirrors
.PHONY: distclean
distclean: clean
	rm -rf download download-src mirror

## help: list available targets
.PHONY: help
help:
	@echo "Targets:"
	@grep -hE '^##' $(MAKEFILE_LIST) | sed 's/^## /  /'

# Force the pattern rule to rebuild (Go's own cache handles incremental builds).
.PHONY: FORCE
FORCE:
