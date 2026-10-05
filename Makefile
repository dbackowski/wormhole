deps:
	go mod tidy
	go mod download

build: deps
	go build -o bin/server/wormhole-server cmd/server/main.go
	go build -o bin/client/wormhole cmd/client/main.go

test:
	go test -race ./...

clean:
	rm -rf bin
