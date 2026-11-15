#!/usr/bin/env bash
# extract.sh — export the open-balena-api model config for one release tag.
#
# Usage: schema/extract.sh <tag> <work-dir>
#
# Writes <work-dir>/config.json (the raw model config, never committed) and
# <work-dir>/provenance.json. Upstream code runs only inside a pinned container:
# dependency installation has network access with install scripts disabled; the
# export itself runs with no network, as a non-root user, with no capabilities,
# a read-only root filesystem and read-only source. It runs twice and the two
# outputs must be byte-identical.
#
# Requires docker, git, jq and go.
set -euo pipefail

UPSTREAM_REPO="https://github.com/balena-io/open-balena-api"
# node:26-bookworm-slim. Upstream's package.json sets the minimum Node version.
NODE_IMAGE="node@sha256:5df79989ed685e09f0a98019db4cebfb22fb9df1a53c0161664a011f7ff6b0f1"
RUN_TIMEOUT_SECONDS=300

TAG=${1:?usage: extract.sh <tag> <work-dir>}
WORK=${2:?usage: extract.sh <tag> <work-dir>}

if [[ ! $TAG =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "extract.sh: refusing tag '$TAG': expected vMAJOR.MINOR.PATCH" >&2
	exit 1
fi

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK_MARKER=".extract-work"
# Only ever clear a directory an earlier run created.
if [ -e "$WORK" ]; then
	if [ ! -f "$WORK/$WORK_MARKER" ]; then
		echo "extract.sh: $WORK exists and was not created by extract.sh; remove it yourself" >&2
		exit 1
	fi
	find "$WORK" -mindepth 1 -delete
fi
mkdir -p "$WORK"
WORK=$(cd "$WORK" && pwd)
touch "$WORK/$WORK_MARKER"
USER_ID="$(id -u):$(id -g)"

echo "== cloning $TAG"
git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$TAG" "$UPSTREAM_REPO" "$WORK/src"
COMMIT=$(git -C "$WORK/src" rev-parse HEAD)
mkdir "$WORK/app"
git -C "$WORK/src" archive HEAD | tar -x -C "$WORK/app"

echo "== installing dependencies (network, no install scripts)"
mkdir -p "$WORK/npm-cache"
docker run --rm --user "$USER_ID" --cap-drop ALL --security-opt no-new-privileges \
	-e HOME=/tmp -e npm_config_cache=/cache \
	-v "$WORK/app:/app" -v "$WORK/npm-cache:/cache" -w /app \
	"$NODE_IMAGE" npm ci --ignore-scripts --no-audit --no-fund --loglevel=error

echo "== building placeholder environment"
(cd "$REPO_ROOT" && go run ./cmd/schemagen placeholder-env "$WORK/src/docker-compose.test-custom.yml" "$WORK/placeholder.env")

run_export() {
	local out=$1
	mkdir -p "$out"
	chmod 777 "$out"
	timeout "$RUN_TIMEOUT_SECONDS" docker run --rm --network none --user "$USER_ID" --cap-drop ALL \
		--security-opt no-new-privileges --read-only --tmpfs /tmp:rw,exec,size=256m -e HOME=/tmp \
		--env-file "$WORK/placeholder.env" -e GENERATE_CONFIG=/out/config.json \
		-v "$WORK/app:/app:ro" -v "$out:/out" -w /app \
		"$NODE_IMAGE" node_modules/.bin/mocha >"$out.log" 2>&1 || {
		echo "extract.sh: export failed, log follows" >&2
		tail -40 "$out.log" >&2
		exit 1
	}
}

echo "== exporting model config (no network), twice"
run_export "$WORK/run1"
run_export "$WORK/run2"
if ! cmp -s "$WORK/run1/config.json" "$WORK/run2/config.json"; then
	echo "extract.sh: the two exports differ" >&2
	exit 1
fi
cp "$WORK/run1/config.json" "$WORK/config.json"

PINEJS=$(jq -r .version "$WORK/app/node_modules/@balena/pinejs/package.json")
NODE_VERSION=$(docker run --rm --network none "$NODE_IMAGE" node --version)
NPM_VERSION=$(docker run --rm --network none "$NODE_IMAGE" npm --version)
for v in "$PINEJS" "$NODE_VERSION" "$NPM_VERSION"; do
	if [[ ! $v =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
		echo "extract.sh: unexpected version string '$v'" >&2
		exit 1
	fi
done

jq -n --sort-keys \
	--arg upstream "$UPSTREAM_REPO" \
	--arg tag "$TAG" \
	--arg commit "$COMMIT" \
	--arg lock "$(sha256sum "$WORK/app/package-lock.json" | cut -d' ' -f1)" \
	--arg config "$(sha256sum "$WORK/config.json" | cut -d' ' -f1)" \
	--arg pinejs "$PINEJS" \
	--arg image "$NODE_IMAGE" \
	--arg node "$NODE_VERSION" \
	--arg npm "$NPM_VERSION" \
	'{upstream: $upstream, tag: $tag, commit: $commit, package_lock_sha256: $lock,
	  raw_config_sha256: $config, pinejs: $pinejs, node_image: $image, node: $node, npm: $npm}' \
	>"$WORK/provenance.json"

echo "== done: $WORK/config.json ($(wc -c <"$WORK/config.json") bytes)"
