ARG GO_VERSION=1.25
FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
# CGO_ENABLED=0 produces a fully static binary (pure-Go net/user resolvers, no
# libc), so it can run on a base image with no shared libraries at all.
RUN cd cmd/server && CGO_ENABLED=0 GOOS=linux go build -v -o /run-app .

# distroless static: no shell, no package manager. It runs as root because Fly
# mounts the volume holding the database (/data) owned by root.
# The server reads GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET and WORMHOLE_HOST from
# the environment (see server/config.go), so no shell-form CMD is needed.
FROM gcr.io/distroless/static-debian12

COPY --from=builder /run-app /usr/local/bin/run-app

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/run-app", "-db=/data/wormhole.db"]
