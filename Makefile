.PHONY: build test vet fmt clean check all

BINARY := nimblegate
PKG := ./cmd/nimblegate
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
LDFLAGS := -X nimblegate/internal/version.Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -l -s .
	@test -z "$$(gofmt -l -s .)" || (echo "gofmt issues found"; exit 1)

clean:
	rm -f $(BINARY)

check: vet fmt test
	@echo "All checks passed."

all: check build
