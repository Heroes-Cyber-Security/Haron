# Haron infrastructure

Exposed ports:
- 40000: manager
- 40001: forwarder
- 57254: webui

## Publishing

The images are published to Docker Hub as per-service tags in a single
repository, `hanzlearn/haron`:

| Tag | Purpose |
| --- | --- |
| `hanzlearn/haron:<service>` | Moving tag, updated on every publish |
| `hanzlearn/haron:<service>-<version>` | Immutable tag, safe to pin |

`<service>` is one of `manager`, `worker`, `orchestrator`, `forwarder`,
`webui`, `mock-ctfd`.

Log in once, then run the publish script:

```bash
docker login
./publish.sh
```

The version defaults to `git describe --tags --always --dirty`. Useful
overrides:

```bash
./publish.sh --no-push                 # build only
./publish.sh --dry-run                 # print commands, run nothing
VERSION=0.1.2 ./publish.sh             # explicit version
SERVICES="manager webui" ./publish.sh  # publish a subset
PLATFORM=linux/amd64 ./publish.sh      # force a platform
```

Builds are slow: `orchestrator` compiles Rust from source against the pinned
foundry revision, and `worker` installs the Python dependencies. Every service
uses a multi-stage build, so the toolchains stay in the builder stage and the
runtime images only carry what the service needs.

## Running published images

`docker-compose.prod.yml` runs the published images instead of building
locally. It omits the dev-only `mock-ctfd` service.

```bash
# Moving tags
docker compose -f docker-compose.prod.yml up -d

# Pin a release
HARON_TAG_SUFFIX=-0.1.2 docker compose -f docker-compose.prod.yml up -d
```

Each service still reads its configuration from `<service>/.env`. For a real
deployment, point `manager`'s `CTF_BASE` at your CTF platform rather than the
mock.

## CTF platform integration

`manager` authenticates players against CTFd or rCTF. Select the platform with
`CTF_PLATFORM` (default `ctfd`) and point `CTF_BASE` at the instance:

```env
# CTFd
CTF_PLATFORM=ctfd
CTF_BASE=https://ctfd.example.com

# rCTF
CTF_PLATFORM=rctf
CTF_BASE=https://rctf.example.com
```

`CTFD_BASE` and `RCTF_BASE` are still accepted as platform-specific fallbacks
when `CTF_BASE` is unset. With rCTF, players paste their **auth token** (the
`Authorization: Bearer` token from login), not the team token.

For local development, use `./debug.sh` with the default `docker-compose.yml`.
