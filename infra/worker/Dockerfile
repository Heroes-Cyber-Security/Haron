# syntax=docker/dockerfile:1

# Foundry publishes forge as a single binary. Copying it out of the upstream
# image pins the version to the foundry revision the orchestrator links against
# (1.9.0) and skips the anvil, cast and chisel binaries foundryup also fetches.
FROM ghcr.io/foundry-rs/foundry:v1.9.0 AS foundry

# forge ships with symbols. Stripping it saves about 18 MB in the final image.
FROM debian:12-slim AS forge

RUN apt-get update && apt-get install -y --no-install-recommends binutils \
    && rm -rf /var/lib/apt/lists/*

COPY --from=foundry /usr/local/bin/forge /forge
RUN strip --strip-all /forge -o /forge-stripped \
    && chmod 0755 /forge-stripped

FROM python:3.14-slim-bookworm

ENV PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN useradd -ms /bin/bash ctf

# Challenges resolve forge at this exact path, and at runtime they build their
# own venv from the interpreter above, so no compiler is required here.
COPY --from=forge --chown=ctf:ctf /forge-stripped /home/ctf/.foundry/bin/forge

WORKDIR /home/ctf

COPY src/requirements.txt /home/ctf/requirements.txt
RUN pip install -r /home/ctf/requirements.txt

COPY src/ /home/ctf/

USER ctf

ENTRYPOINT [ "python", "app.py" ]
