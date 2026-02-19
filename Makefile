.PHONY: build install test clean

build:
	go build -ldflags "-s -w" -o bin/differ .

install:
	go install .

test:
	go test ./...

clean:
	rm -rf bin/
