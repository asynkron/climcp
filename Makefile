BINARY    := climcp
PREFIX    ?= /usr/local
BINDIR    ?= $(PREFIX)/bin
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)

.PHONY: all build install uninstall test vet fmt clean tidy

all: build

## build: compile the climcp binary into ./bin
build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

## install: build and copy the binary to $(BINDIR)
install: build
	install -d $(BINDIR)
	install -m 0755 bin/$(BINARY) $(BINDIR)/$(BINARY)

## uninstall: remove the installed binary
uninstall:
	rm -f $(BINDIR)/$(BINARY)

## test: run the test suite
test:
	go test ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: format the source
fmt:
	gofmt -w .

## tidy: tidy go.mod
tidy:
	go mod tidy

## clean: remove build artifacts
clean:
	rm -rf bin
