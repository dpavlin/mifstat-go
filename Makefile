# mifstat Makefile

BINARY_NAME=mifstat
VERSION=0.1.0
BUILD_OPTS=-ldflags="-s -w"

.PHONY: all build clean release test

all: build

build:
	CGO_ENABLED=0 go build $(BUILD_OPTS) -o $(BINARY_NAME) .

clean:
	rm -f $(BINARY_NAME) mifstat-bin

test:
	go test ./...

release: build
	@echo "Built $(BINARY_NAME) version $(VERSION)"

# Cross-compilation examples
build-linux-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_OPTS) -o $(BINARY_NAME)-linux-amd64 .

build-linux-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(BUILD_OPTS) -o $(BINARY_NAME)-linux-arm64 .
