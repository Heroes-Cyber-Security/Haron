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

Builds are slow: `worker` compiles Python and installs Foundry, and
`orchestrator` compiles Rust from source.

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
deployment, point `manager`'s `CTFD_BASE` at your CTFd instance rather than
the mock.

For local development, use `./debug.sh` with the default
`docker-compose.yml`.
