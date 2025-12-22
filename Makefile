deps:
	go mod tidy
	go mod download

build: deps
	go build -o bin/server/wormhole cmd/server/main.go
	go build -o bin/client/wormhole cmd/client/main.go

clean:
	rm -rf bin
