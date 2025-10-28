deps:
	go mod tidy
	go mod download

build: deps
	go build -o cmd/server/wormhole server/main.go
	go build -o cmd/client/wormhole client/main.go

clean:
	rm -rf cmd
