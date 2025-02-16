FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY / ./

RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -v -o /server .

FROM alpine

COPY --from=builder  /server /usr/bin/server

ENTRYPOINT [ "/usr/bin/server" ]
