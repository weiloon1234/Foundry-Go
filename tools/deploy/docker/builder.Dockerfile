# The release builder make deploy runs (/foundry-deploy/local/build.sh), on this
# machine's own architecture: Ubuntu 24.04, the oldest supported server, so the
# cgo binary runs on every supported release; Go and Node at the versions go.mod
# and web/package.json select; a C cross compiler for the other architecture
# (amd64 servers from an arm64 Mac and the reverse), without emulation.
ARG GO_VERSION
ARG NODE_MAJOR
FROM golang:${GO_VERSION} AS go
FROM node:${NODE_MAJOR}-bookworm-slim AS node

FROM ubuntu:24.04
# The local CLI labels, locks and bounds builders per project identity.
LABEL foundry.builder=1
RUN case "$(dpkg --print-architecture)" in \
		arm64) cross="gcc-x86-64-linux-gnu libc6-dev-amd64-cross" ;; \
		amd64) cross="gcc-aarch64-linux-gnu libc6-dev-arm64-cross" ;; \
		*) echo "unsupported builder architecture" >&2; exit 1 ;; \
	esac \
	&& apt-get update \
	&& DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
		build-essential ca-certificates python3 $cross \
	&& rm -rf /var/lib/apt/lists/*
COPY --from=go /usr/local/go /usr/local/go
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
	&& ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx
ENV PATH=/usr/local/go/bin:$PATH \
	GOTOOLCHAIN=local \
	GOFLAGS=-mod=readonly \
	GOMODCACHE=/cache/go/mod \
	GOCACHE=/cache/go/build \
	npm_config_cache=/cache/npm \
	CGO_ENABLED=1
