# Variables
BINARY  := bin/vek
VERSION := $(shell git describe --tags --always)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build run test check clean

build:
	mkdir -p bin
	go build $(LDFLAGS) -o $(BINARY) ./cmd/vek

run: build
	./$(BINARY) serve

test:
	go test ./...

check:
	go vet ./...
	@files="$$(gofmt -l .)"; if [ -n "$$files" ]; then echo "unformatted:" $$files; exit 1; fi

clean:
	rm -rf bin
