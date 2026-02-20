.PHONY: build install test clean release-dry-run

VERSION ?= dev
LDFLAGS := -s -w -X github.com/jansmrcka/differ/cmd.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/differ .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

clean:
	rm -rf bin/ dist/

release-dry-run:
	goreleaser release --snapshot --clean
