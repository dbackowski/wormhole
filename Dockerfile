# check=skip=JSONArgsRecommended
# JSON CMD would not expand ${AUTH_TOKEN}/${HOST}; we use shell form with
# `exec` instead so the binary is still PID 1 and receives signals.

ARG GO_VERSION=1.25
FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN cd cmd/server && go build -v -o /run-app .

FROM debian:bookworm

COPY --from=builder /run-app /usr/local/bin/

USER nobody

CMD exec run-app -auth-token="${AUTH_TOKEN}" -host="${HOST}"
