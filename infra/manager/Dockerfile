# syntax=docker/dockerfile:1

FROM golang:1.27-bookworm AS builder

WORKDIR /build

COPY src/go.mod src/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY src/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/manager .

# The binary is static, so the runtime only needs the CA bundle that distroless
# already ships. Challenges are read from the bind mount at /home/ctf/challenges.
# Keep the historic uid 1000 so bind mounts owned by the deploy user stay readable.
FROM gcr.io/distroless/static-debian12:nonroot

USER 1000:1000

WORKDIR /home/ctf

COPY --from=builder /out/manager /home/ctf/manager

VOLUME [ "/home/ctf/challenges/" ]

ENTRYPOINT [ "/home/ctf/manager" ]
