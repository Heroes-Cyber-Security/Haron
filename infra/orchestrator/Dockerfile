FROM debian:12-slim

RUN apt update
RUN apt upgrade -y
RUN apt install curl git build-essential -y

RUN useradd -ms /bin/bash ctf
USER ctf
WORKDIR /home/ctf

RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | bash -s -- -y
RUN /home/ctf/.cargo/bin/rustup update 1.90.0
RUN /home/ctf/.cargo/bin/rustup toolchain install 1.90.0

# copy manifests first to cache dependency downloads
COPY Cargo.toml Cargo.lock /home/ctf/
RUN /home/ctf/.cargo/bin/cargo fetch --locked || /home/ctf/.cargo/bin/cargo fetch

# copy rest and build release
COPY .env /home/ctf/
COPY src/ /home/ctf/src/
RUN /home/ctf/.cargo/bin/cargo build --release

ENTRYPOINT [ "/home/ctf/target/release/orchestrator" ]