FROM debian:12-slim

RUN apt update
RUN apt upgrade -y
RUN apt install git curl wget -y
RUN apt install -y make build-essential libssl-dev zlib1g-dev \
	libbz2-dev libreadline-dev libsqlite3-dev curl git \
	libncursesw5-dev xz-utils tk-dev libxml2-dev libxmlsec1-dev libffi-dev liblzma-dev

RUN useradd -ms /bin/bash ctf
USER ctf
WORKDIR /home/ctf

RUN curl -fsSL https://pyenv.run | bash
RUN /home/ctf/.pyenv/bin/pyenv install 3.14.0
RUN /home/ctf/.pyenv/bin/pyenv global 3.14.0

RUN curl -L https://foundry.paradigm.xyz | bash
RUN /home/ctf/.foundry/bin/foundryup

COPY src/requirements.txt .
RUN /home/ctf/.pyenv/shims/python -m pip install -r requirements.txt

COPY src/ .

ENTRYPOINT [ "/home/ctf/.pyenv/shims/python", "app.py" ]