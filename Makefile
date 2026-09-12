.PHONY: build release test vet run

build:
	mkdir -p bin
	go build -trimpath -o bin/feed-benchmark ./cmd/feed-benchmark

release:
	VERSION=$${VERSION:-dev} ./compile.sh

test:
	go test ./...

vet:
	go vet ./...

run:
	go run ./cmd/feed-benchmark
