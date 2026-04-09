VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.version=$(VERSION)
BIN     := ./bin/axon

.PHONY: build install test lint clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/axon

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/axon

test:
	go test -count=1 ./...

lint:
	golangci-lint run

clean:
	rm -f $(BIN)
