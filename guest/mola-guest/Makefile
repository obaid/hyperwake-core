# Mola guest daemon.
#
# The daemon ships as a static-ish binary inside every machine image, so it is
# cross-compiled for the architectures the image is built for.

VERSION ?= 0.1.0
LDFLAGS := -s -w -X github.com/obaid/mola-core/guest/mola-guest/internal/config.Version=$(VERSION)

.PHONY: all test vet fmt build clean

all: fmt vet test build

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./... -race

# Every Go source file is a prerequisite. Without this, make considers an
# existing binary up to date and silently ships a stale daemon.
SOURCES := $(shell find . -name '*.go' -not -name '*_test.go') go.mod

build: bin/mola-guest-linux-arm64 bin/mola-guest-linux-amd64

bin/mola-guest-linux-arm64: $(SOURCES)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/mola-guest

bin/mola-guest-linux-amd64: $(SOURCES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/mola-guest

clean:
	rm -rf bin
