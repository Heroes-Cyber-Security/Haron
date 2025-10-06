FROM debian:12-slim

RUN apt update
RUN apt upgrade -y
RUN apt install git curl wget -y

RUN useradd -ms /bin/bash ctf
USER ctf
WORKDIR /home/ctf

RUN wget https://go.dev/dl/go1.25.1.linux-amd64.tar.gz -O go.tar.gz
RUN tar xzf go.tar.gz

# Compile sources
COPY src /home/ctf/
RUN /home/ctf/go/bin/go build

ENTRYPOINT [ "/home/ctf/forwarder" ]