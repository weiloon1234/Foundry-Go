#!/usr/bin/env bash
# Lead-only integration runner. Nothing here is run by Python/shell syntax checks.
set -euo pipefail
umask 077

usage() {
  cat <<'EOF'
Usage: run_acceptance.sh --ubuntu 24.04|26.04 --platform linux/arm64|linux/amd64 \
  --archive /absolute/app.tar.gz --framework-root /absolute/Foundry-Go --foreign|--fresh

Run each OS/architecture/mode separately with a matching Linux app archive.
amd64 is emulated on arm64 Docker hosts and can be substantially slower.
DOCKER_CONFIG and DOCKER_HOST are inherited. No host ports are published.
Optional: ACCEPTANCE_LOG_DIR, ACCEPTANCE_SCHEDULE_MESSAGE (see acceptance_roles.py).
EOF
}

ubuntu= platform= archive= framework= mode=
while (($#)); do
  case "$1" in
    --ubuntu|--platform|--archive|--framework-root)
      (($# >= 2)) || { usage >&2; exit 2; }
      case "$1" in
        --ubuntu) ubuntu=$2 ;; --platform) platform=$2 ;;
        --archive) archive=$2 ;; --framework-root) framework=$2 ;;
      esac
      shift 2 ;;
    --foreign|--fresh)
      [[ -z "$mode" ]] || { echo 'Select one host mode' >&2; exit 2; }
      mode=${1#--}; shift ;;
    --help|-h) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done
[[ "$ubuntu" == 24.04 || "$ubuntu" == 26.04 ]] || { usage >&2; exit 2; }
[[ "$platform" == linux/arm64 || "$platform" == linux/amd64 ]] || { usage >&2; exit 2; }
[[ -n "$mode" && -f "$archive" && -d "$framework" ]] || { usage >&2; exit 2; }
framework=$(cd "$framework" && pwd -P)
archive=$(cd "$(dirname "$archive")" && pwd -P)/$(basename "$archive")
tests="$framework/tools/deploy/tests"
# CONTRACT: F0 host.Dockerfile remains the systemd/inspection-only base image;
# acceptance_host accepts --archive/--redis-source and covers bootstrap/repeat/
# concurrent operations against dedicated Foundry services. acceptance_nginx
# preserves the nginx master and handles schema-2/optional worker topology.
for required in host.Dockerfile acceptance.Dockerfile acceptance_host.py acceptance_nginx.py acceptance_foreign.py acceptance_roles.py acceptance_backup.py; do
  [[ -f "$tests/$required" ]] || { echo "Missing integrated fixture: $required" >&2; exit 2; }
done
if [[ -n "${ACCEPTANCE_LOG_DIR:-}" ]]; then
  mkdir -m 700 "$ACCEPTANCE_LOG_DIR"  # Refuse overwriting a previous run.
  logs=$(cd "$ACCEPTANCE_LOG_DIR" && pwd -P)
else
  logs=$(mktemp -d "${TMPDIR:-/tmp}/foundry-acceptance.XXXXXXXX")
fi
token=$(openssl rand -hex 8)
network=foundry-acceptance-$token
host=foundry-host-$token
minio=foundry-minio-$token
volume=foundry-minio-data-$token
image=foundry-acceptance-host:$token
base_image=foundry-acceptance-base:$token
minio_image=minio/minio:RELEASE.2025-04-22T22-12-26Z
env_file=$logs/.private-env
baseline_done=0
foreign_verified=0
network_created=0
volume_created=0
host_created=0
minio_created=0
image_created=0
base_created=0

cleanup() {
  result=$?
  trap - EXIT INT TERM
  set +e
  # Preserve foreign evidence even when an earlier phase failed. This does not
  # turn the original failure into success or hide a failed preservation check.
  if ((host_created && baseline_done && !foreign_verified)); then
    docker exec --env-file "$env_file" "$host" python3 -B \
      /verification/deploy/tests/acceptance_foreign.py verify >"$logs/foreign-final-on-failure.log" 2>&1
    (( $? == 0 )) || result=1
  fi
  cleanup_failed=0
  if ((host_created)); then docker rm -f -v "$host" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((minio_created)); then docker rm -f -v "$minio" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((volume_created)); then docker volume rm "$volume" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((network_created)); then docker network rm "$network" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((image_created)); then docker image rm "$image" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((base_created)); then docker image rm "$base_image" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  rm -f "$env_file"
  ((cleanup_failed == 0)) || result=1
  echo "Acceptance exit=$result; logs: $logs"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

access=acceptance-$token
secret=$(openssl rand -hex 32)
cat >"$env_file" <<EOF
FOUNDRY_DISPOSABLE_HOST=1
PYTHONDONTWRITEBYTECODE=1
MINIO_ROOT_USER=$access
MINIO_ROOT_PASSWORD=$secret
ACCEPTANCE_S3_ENDPOINT=http://minio:9000
ACCEPTANCE_S3_ACCESS_KEY=$access
ACCEPTANCE_S3_SECRET_KEY=$secret
ACCEPTANCE_APP_BUCKET=app-$token
ACCEPTANCE_BACKUP_BUCKET=backup-$token
ACCEPTANCE_SCHEDULE_MESSAGE=${ACCEPTANCE_SCHEDULE_MESSAGE:-acceptance schedule completed}
EOF
unset secret access
printf 'ubuntu=%s\nplatform=%s\nmode=%s\nminio=%s\n' "$ubuntu" "$platform" "$mode" "$minio_image" >"$logs/run.txt"
echo "Acceptance logs: $logs"
if [[ "$platform" == linux/amd64 ]]; then
  echo 'linux/amd64 uses emulation on arm64 hosts; expect substantially slower startup and execution.'
fi
docker build --platform "$platform" -f "$tests/host.Dockerfile" \
  --build-arg "UBUNTU_VERSION=$ubuntu" -t "$base_image" "$tests" >"$logs/build-base.log" 2>&1
base_created=1
docker build --platform "$platform" -f "$tests/acceptance.Dockerfile" \
  --build-arg "BASE_IMAGE=$base_image" -t "$image" "$tests" >"$logs/build.log" 2>&1
image_created=1
docker network create "$network" >"$logs/network.log" 2>&1
network_created=1
docker volume create "$volume" >"$logs/volume.log" 2>&1
volume_created=1
# No -p/-P flags: S3 and all application ports stay on the dedicated network.
docker create --platform "$platform" --name "$minio" --network "$network" --network-alias minio \
  --env-file "$env_file" --mount "type=volume,source=$volume,target=/data" \
  "$minio_image" server /data --console-address :9001 >"$logs/minio-create.log" 2>&1
minio_created=1
docker start "$minio" >"$logs/minio-start.log" 2>&1
docker create --platform "$platform" --name "$host" --network "$network" \
  --privileged --cgroupns=private --memory=4g --cpus=2 \
  --tmpfs /run --tmpfs /run/lock --tmpfs /tmp \
  --mount "type=bind,source=$framework/tools/deploy,target=/verification/deploy,readonly" \
  --mount "type=bind,source=$framework,target=/verification/foundry-go,readonly" \
  "$image" >"$logs/host-create.log" 2>&1
host_created=1
docker start "$host" >"$logs/host-start.log" 2>&1
docker exec "$host" systemctl start multi-user.target >"$logs/systemd.log" 2>&1
docker exec "$host" mkdir -m 700 -p /var/lib/foundry-acceptance-f4a
docker cp "$archive" "$host:/var/lib/foundry-acceptance-f4a/input.tar.gz" >"$logs/archive-copy.log" 2>&1
docker exec --env-file "$env_file" -i "$host" python3 - >"$logs/minio-ready.log" 2>&1 <<'PY'
import os, time, urllib.request
deadline = time.monotonic() + 180
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen(os.environ['ACCEPTANCE_S3_ENDPOINT'] + '/minio/health/ready', timeout=5) as response:
            if response.status == 200:
                break
    except OSError:
        pass
    time.sleep(1)
else:
    raise SystemExit('MinIO readiness deadline exceeded')
PY

phase() {
  name=$1; shift
  echo "Running $name"
  docker exec --env-file "$env_file" "$host" python3 -B "$@" >"$logs/$name.log" 2>&1
}
phase foreign-baseline /verification/deploy/tests/acceptance_foreign.py baseline --mode "$mode"
baseline_done=1
phase host /verification/deploy/tests/acceptance_host.py \
  --archive /var/lib/foundry-acceptance-f4a/input.tar.gz --redis-source /verification/foundry-go
phase roles /verification/deploy/tests/acceptance_roles.py --archive /var/lib/foundry-acceptance-f4a/input.tar.gz
phase backup /verification/deploy/tests/acceptance_backup.py
phase nginx /verification/deploy/tests/acceptance_nginx.py --rounds 1
# Set before invocation: a failed verify must not be repeated after it has closed
# its Redis observation window.
foreign_verified=1
phase foreign-verify /verification/deploy/tests/acceptance_foreign.py verify
echo 'PASS all acceptance phases; external Cloudflare DNS/ACME and real R2 remain separate gates.'
