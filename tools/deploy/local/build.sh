#!/usr/bin/env bash
# Runs only inside the local builder. Input is public metadata, never app secrets.
set -euo pipefail
: "${VERSION:?}" "${ARCHITECTURE:?}"
case "$ARCHITECTURE" in
amd64) CROSS_CC=x86_64-linux-gnu-gcc ;;
arm64) CROSS_CC=aarch64-linux-gnu-gcc ;;
*) printf 'Unsupported architecture\n' >&2; exit 1 ;;
esac
work=/work
stage=/stage
mkdir -p "$work" "$stage"
# Never copy operator settings, runtime files, environment files or key material.
tar -C /src --exclude=.git --exclude='.env*' --exclude=.deploy --exclude=dist --exclude=bin \
	--exclude=.aws --exclude=.ssh --exclude=.netrc --exclude=.npmrc --exclude=.codex \
	--exclude=./.claude/settings.local.json --exclude=./CLAUDE.local.md \
	--exclude=storage --exclude=./public/build --exclude=./config/app.toml --exclude='./config/*.local.toml' \
	--exclude=node_modules --exclude='*.pem' --exclude='*.key' --exclude='*.p12' --exclude='*.pfx' \
	--exclude=go.work --exclude=go.work.sum -cf - . | tar -C "$work" -xf -
python3 /foundry-deploy/local/build_metadata.py public-env /request.json "$work/.env"
for frontend in client web; do
	if [ -f "$work/$frontend/package.json" ]; then
		printf 'Building %s\n' "$frontend" >&2
		npm ci --prefix "$work/$frontend" --ignore-scripts --no-audit --no-fund --loglevel=error
		FOUNDRY_BUILD_RELEASE="$VERSION" npm --prefix "$work/$frontend" run build --silent
	fi
done
printf 'Building artisan for linux/%s\n' "$ARCHITECTURE" >&2
module=$(awk '$1 == "module" {print $2; exit}' "$work/go.mod")
[ -n "$module" ] || { printf 'go.mod has no module declaration\n' >&2; exit 1; }
# Bound compiler concurrency in the local builder.
(cd "$work" && GOWORK=off GOOS=linux GOARCH="$ARCHITECTURE" CC="$CROSS_CC" go build -p 1 -trimpath \
	-ldflags "-X $module/internal/config.BuildRelease=$VERSION" -o "$stage/artisan" ./cmd/artisan)
mkdir -p "$stage/config" "$stage/private-source-maps"
cp "$work/config/deploy.toml" "$stage/config/"
if [ -d "$work/public" ]; then cp -a "$work/public" "$stage/public"; fi
if [ -d "$work/storage/private-source-maps/$VERSION" ]; then
	cp -a "$work/storage/private-source-maps/$VERSION" "$stage/private-source-maps/"
fi
python3 /foundry-deploy/local/build_metadata.py package /request.json "$stage" /out
