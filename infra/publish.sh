#!/usr/bin/env bash
#
# Publish the Haron infrastructure images to Docker Hub.
#
# Every service is pushed to a single repository as per-service tags:
#
#   <repo>:<service>              moving tag, updated on every publish
#   <repo>:<service>-<version>    immutable tag, safe to pin
#
# Run `docker login` first: this script never handles credentials.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO="${IMAGE_REPO:-hanzlearn/haron}"
ALL_SERVICES=(manager worker orchestrator forwarder webui mock-ctfd)

DRY_RUN=0
PUSH=1

usage() {
  cat <<'EOF'
Usage: publish.sh [options]

Build and push the Haron infrastructure images.

Options:
  --no-push    Build the images without pushing them.
  --dry-run    Print the docker commands instead of running them.
  -h, --help   Show this help.

Environment:
  IMAGE_REPO   Repository to publish to (default: hanzlearn/haron).
  SERVICES     Space-separated services to publish, in order.
               (default: manager worker orchestrator forwarder webui mock-ctfd)
  VERSION      Version used for the immutable tag. Defaults to
               `git describe --tags --always --dirty`.
  PLATFORM     Value for `docker build --platform` (e.g. linux/amd64).
  BUILD_ARGS   Extra arguments for `docker build`, split on whitespace
               (e.g. BUILD_ARGS="--build-arg FOO=bar").
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

warn() {
  printf 'warning: %s\n' "$*" >&2
}

run() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
  if ((DRY_RUN == 0)); then
    "$@"
  fi
}

while (($#)); do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --dry-run)
      DRY_RUN=1
      ;;
    --no-push)
      PUSH=0
      ;;
    *)
      printf 'error: unknown option: %s\n\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [[ -n "${SERVICES:-}" ]]; then
  read -r -a services <<<"$SERVICES"
else
  services=("${ALL_SERVICES[@]}")
fi
[[ ${#services[@]} -gt 0 ]] || die "no services selected"

if [[ -n "${VERSION:-}" ]]; then
  version="$VERSION"
elif git -C "$SCRIPT_DIR" rev-parse --git-dir >/dev/null 2>&1; then
  version="$(git -C "$SCRIPT_DIR" describe --tags --always --dirty 2>/dev/null || true)"
  [[ -n "$version" ]] || version="dev"
else
  version="dev"
fi

# Docker tags allow [A-Za-z0-9_][A-Za-z0-9_.-]{0,127}.
if [[ ! "$version" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]; then
  die "version '$version' is not a valid Docker tag"
fi
if [[ "$version" == *-dirty ]]; then
  warn "working tree is dirty; publishing '$version' is not reproducible"
fi

# Repository names must be lowercase.
if [[ "$REPO" != "$(printf '%s' "$REPO" | tr '[:upper:]' '[:lower:]')" ]]; then
  die "IMAGE_REPO '$REPO' must be lowercase"
fi

build_args=()
if [[ -n "${BUILD_ARGS:-}" ]]; then
  read -r -a build_args <<<"$BUILD_ARGS"
fi

if ((DRY_RUN == 0)); then
  docker info >/dev/null 2>&1 ||
    die "cannot reach the Docker daemon; is Docker running?"
fi

printf 'publishing %d service(s) to %s (version: %s)\n\n' \
  "${#services[@]}" "$REPO" "$version"

for service in "${services[@]}"; do
  context="$SCRIPT_DIR/$service"
  [[ -f "$context/Dockerfile" ]] ||
    die "no Dockerfile for service '$service' (looked in $context)"
  [[ "$service" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]*$ ]] ||
    die "service name '$service' is not a valid Docker tag"

  moving="$REPO:$service"
  pinned="$REPO:$service-$version"

  printf '==> building %s\n' "$service"
  cmd=(docker build)
  if [[ -n "${PLATFORM:-}" ]]; then
    cmd+=(--platform "$PLATFORM")
  fi
  if [[ ${#build_args[@]} -gt 0 ]]; then
    cmd+=("${build_args[@]}")
  fi
  cmd+=(-t "$pinned" -t "$moving" "$context")
  run "${cmd[@]}"
done

if ((PUSH)); then
  for service in "${services[@]}"; do
    # Push the immutable tag first so the moving tag never points at
    # something that failed to publish.
    for tag in "$REPO:$service-$version" "$REPO:$service"; do
      printf '==> pushing %s\n' "$tag"
      if ! run docker push "$tag"; then
        die "failed to push $tag; run 'docker login' and try again"
      fi
    done
  done
else
  printf '\nskipped push (--no-push)\n'
fi

printf '\ndone\n'
