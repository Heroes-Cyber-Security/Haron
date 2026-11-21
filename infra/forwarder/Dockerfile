# syntax=docker/dockerfile:1

FROM golang:1.27-bookworm AS builder

WORKDIR /build

COPY src/go.mod src/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY src/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/forwarder .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/forwarder /usr/local/bin/forwarder

ENTRYPOINT [ "/usr/local/bin/forwarder" ]
