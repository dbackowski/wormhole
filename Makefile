.PHONY: build test clean

build:
	go build -o bin/server/wormhole-server ./cmd/server
	go build -o bin/client/wormhole ./cmd/client

test:
	go test -race ./...

clean:
	rm -rf bin
