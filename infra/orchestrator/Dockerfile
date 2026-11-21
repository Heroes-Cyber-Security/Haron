FROM debian:12-slim

RUN apt update
RUN apt upgrade -y
RUN apt install curl git build-essential -y

RUN useradd -ms /bin/bash ctf
USER ctf
WORKDIR /home/ctf

RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | bash -s -- -y --default-toolchain 1.100.0 --profile minimal
RUN /home/ctf/.cargo/bin/rustup component add rust-std

# copy manifests first to cache dependency downloads
COPY Cargo.toml Cargo.lock /home/ctf/

# copy rest and build release
COPY src/ /home/ctf/src/
RUN /home/ctf/.cargo/bin/cargo build --release

ENTRYPOINT [ "/home/ctf/target/release/orchestrator" ]