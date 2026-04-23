ARG GO_VERSION=1
FROM golang:${GO_VERSION}-bookworm as builder

WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN cd cmd/server && go build -v -o /run-app .

FROM debian:bookworm

COPY --from=builder /run-app /usr/local/bin/
CMD run-app -auth-token="${AUTH_TOKEN}" -host="${HOST}"
