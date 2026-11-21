# syntax=docker/dockerfile:1

# Build stage. The toolchain, cargo caches and the target directory stay here so
# the runtime image only carries the compiled binary.
FROM rust:1.100-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential \
        cmake \
        pkg-config \
        git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

COPY Cargo.toml Cargo.lock ./
COPY src ./src

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    --mount=type=cache,target=/build/target \
    cargo build --release --locked \
    && cp target/release/orchestrator /usr/local/bin/orchestrator

FROM debian:12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -ms /bin/bash ctf

COPY --from=builder /usr/local/bin/orchestrator /home/ctf/orchestrator

WORKDIR /home/ctf
USER ctf

ENTRYPOINT [ "/home/ctf/orchestrator" ]
