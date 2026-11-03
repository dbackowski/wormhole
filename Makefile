.PHONY: build test clean install uninstall

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

build:
	go build -o bin/server/wormhole-server ./cmd/server
	go build -o bin/client/wormhole ./cmd/client

test:
	go test -race ./...

clean:
	rm -rf bin

# Rebuilds only when client sources change, so `sudo make install` after `make build` never runs go as root.
bin/client/wormhole: $(shell find cmd/client client common -type f) go.mod go.sum
	go build -o bin/client/wormhole ./cmd/client

install: bin/client/wormhole
	install -d $(DESTDIR)$(BINDIR)
	install -m 755 bin/client/wormhole $(DESTDIR)$(BINDIR)/wormhole

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/wormhole
