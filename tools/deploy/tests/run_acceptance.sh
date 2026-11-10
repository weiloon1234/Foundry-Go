#!/usr/bin/env bash
# CI/lead integration runner. Nothing here is run by Python/shell syntax checks.
set -euo pipefail
umask 077

usage() {
  cat <<'EOF'
Usage: run_acceptance.sh --ubuntu 24.04|26.04 --platform linux/arm64|linux/amd64 \
  --archive /absolute/app.tar.gz --framework-root /absolute/Foundry-Go --foreign|--fresh [--upgrade --tools-root /absolute/module-cache/previous-release/tools/deploy]

Run each OS/architecture/mode separately with a matching Linux app archive.
The archive must declare HTTP + scheduler (worker optional), private R2 disks,
coordination and portal-cache Redis children, and a <=60s successful schedule.
The default schedule JSON msg is "acceptance schedule completed"; set
ACCEPTANCE_SCHEDULE_MESSAGE to the archive's actual completion message.
amd64 is emulated on arm64 Docker hosts and can be substantially slower.
DOCKER_CONFIG and DOCKER_HOST are inherited. No host ports are published.
Public reports must show live roles/readiness; Linux may withhold cross-UID
executable identity, which must be reported as unverified. Root reports must
prove every running role matches its selected release.
--upgrade uses the same deploy-acceptance archive for the previous published tools
and the candidate. Supply the previous release's Go module-cache bundle through
--tools-root. It bootstraps and deploys with that bundle, then upgrades through
tools-plan/tools-apply and proves the candidate deployment and installed units.
Optional: ACCEPTANCE_LOG_DIR, ACCEPTANCE_SCHEDULE_MESSAGE (see acceptance_roles.py),
ACCEPTANCE_SECRET_CONFIG_KEYS (comma-separated keys the first deploy needs, such as
seed passwords; each receives a random bootstrap value).
EOF
}

ubuntu= platform= archive= framework= mode= upgrade=0 old_tools=
while (($#)); do
  case "$1" in
    --ubuntu|--platform|--archive|--framework-root|--tools-root)
      (($# >= 2)) || { usage >&2; exit 2; }
      case "$1" in
        --ubuntu) ubuntu=$2 ;; --platform) platform=$2 ;;
        --archive) archive=$2 ;; --framework-root) framework=$2 ;; --tools-root) old_tools=$2 ;;
      esac
      shift 2 ;;
    --foreign|--fresh)
      [[ -z "$mode" ]] || { echo 'Select one host mode' >&2; exit 2; }
      mode=${1#--}; shift ;;
    --upgrade) upgrade=1; shift ;;
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
if ((upgrade)); then
  [[ -f "$tests/acceptance_upgrade.py" ]] || { echo 'Missing upgrade acceptance driver' >&2; exit 2; }
fi
for required in host.Dockerfile acceptance.Dockerfile acceptance_host.py acceptance_nginx.py acceptance_foreign.py acceptance_roles.py acceptance_backup.py acceptance_recover.py acceptance_edge.py acceptance_support.py acceptance_http.py acceptance_diagnostics.py acceptance_phase.py; do
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
rehearsal=foundry-rehearsal-$token
takeover=foundry-takeover-$token
s3=foundry-s3-$token
volume=foundry-s3-data-$token
image=foundry-acceptance-host:$token
base_image=foundry-acceptance-base:$token
s3_image=versity/versitygw:v1.10.0@sha256:89cad13cc981986ee284cbafea4613f9533ce6fd5f37a2c96c4c4fb8adb3a316
env_file=$logs/.private-env
certs=$logs/.private-tls
mkdir -m 700 "$certs"
baseline_done=0
foreign_verified=0
network_created=0
volume_created=0
host_created=0
recovery_hosts=()
s3_created=0
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
  # Debugging only: leave a failed environment running for phase reruns.
  if ((result != 0)) && [[ "${FOUNDRY_ACCEPTANCE_KEEP:-}" == 1 ]]; then
    echo "Kept for debugging: host=$host s3=$s3 network=$network volume=$volume env_file=$env_file certs=$certs recovery_hosts=${recovery_hosts[*]-}"
    echo "Acceptance exit=$result; logs: $logs"
    exit "$result"
  fi
  for recovery_host in ${recovery_hosts[@]+"${recovery_hosts[@]}"}; do docker rm -f -v "$recovery_host" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; done
  if ((host_created)); then docker rm -f -v "$host" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((s3_created)); then docker rm -f -v "$s3" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((volume_created)); then docker volume rm "$volume" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((network_created)); then docker network rm "$network" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((image_created)); then docker image rm "$image" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  if ((base_created)); then docker image rm "$base_image" >>"$logs/cleanup.log" 2>&1 || cleanup_failed=1; fi
  rm -f "$env_file"
  rm -rf "$logs/.private-recovery"
  rm -rf "$certs"  # Task-owned private CA and S3 stand-in key material only.
  ((cleanup_failed == 0)) || result=1
  echo "Acceptance exit=$result; logs: $logs"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

old_tools_mount=()
if ((upgrade)); then
  [[ -n "$old_tools" && -f "$old_tools/protocol.json" ]] || { echo '--upgrade requires --tools-root from the previous published release' >&2; exit 2; }
  old_tools=$(cd "$old_tools" && pwd -P)
  [[ "$old_tools" =~ /github.com/weiloon1234/!foundry-!go@v[0-9]+\.[0-9]+\.[0-9]+/tools/deploy$ ]] || { echo 'Expected published release module-cache tools' >&2; exit 2; }
  old_tools_mount=(--mount "type=bind,source=$old_tools,target=/verification/old-deploy,readonly")
fi

access=acceptance-$token
secret=$(openssl rand -hex 32)
cat >"$env_file" <<EOF
FOUNDRY_DISPOSABLE_HOST=1
FOUNDRY_ACCEPTANCE_SYNTHETIC_CERT=1
FOUNDRY_NGINX_EDGE_ACCEPTANCE=1
PYTHONDONTWRITEBYTECODE=1
ACCEPTANCE_S3_ENDPOINT=https://s3:9000
ACCEPTANCE_S3_ACCESS_KEY=$access
ACCEPTANCE_S3_SECRET_KEY=$secret
ACCEPTANCE_APP_BUCKET=app-$token
ACCEPTANCE_BACKUP_BUCKET=backup-$token
ACCEPTANCE_SCHEDULE_MESSAGE=${ACCEPTANCE_SCHEDULE_MESSAGE:-acceptance schedule completed}
ACCEPTANCE_SECRET_CONFIG_KEYS=${ACCEPTANCE_SECRET_CONFIG_KEYS:-}
EOF
# A private disposable CA signs both the S3 stand-in endpoint and synthetic site
# lineages. pgBackRest/rclone/app clients use the normal host trust store.
# Explicit CA extensions: LibreSSL adds none by default, and strict X.509
# verifiers (Python 3.13+ on Ubuntu 26.04) require CA:TRUE and key identifiers.
cat >"$certs/ca.cnf" <<'CA_CONFIG'
[req]
distinguished_name = dn
x509_extensions = v3_ca
prompt = no
[dn]
CN = Foundry-acceptance-CA
[v3_ca]
basicConstraints = critical,CA:TRUE
keyUsage = critical,keyCertSign,cRLSign
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always
CA_CONFIG
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -sha256 -config "$certs/ca.cnf" \
  -keyout "$certs/ca.key" -out "$certs/ca.crt" >"$logs/tls-setup.log" 2>&1
openssl req -new -newkey rsa:2048 -nodes -subj /CN=s3 \
  -keyout "$certs/private.key" -out "$certs/s3.csr" >>"$logs/tls-setup.log" 2>&1
cat >"$certs/s3.ext" <<'EOF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:s3
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid,issuer
EOF
openssl x509 -req -in "$certs/s3.csr" -CA "$certs/ca.crt" -CAkey "$certs/ca.key" \
  -CAcreateserial -days 2 -sha256 -extfile "$certs/s3.ext" -out "$certs/public.crt" \
  >>"$logs/tls-setup.log" 2>&1
chmod 600 "$certs/ca.key" "$certs/private.key"
printf 'ubuntu=%s\nplatform=%s\nmode=%s\nupgrade=%s\ns3=%s\n' "$ubuntu" "$platform" "$mode" "$upgrade" "$s3_image" >"$logs/run.txt"
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
ROOT_ACCESS_KEY_ID=$access ROOT_SECRET_ACCESS_KEY=$secret \
docker create --platform "$platform" --user 0:0 --name "$s3" --network "$network" --network-alias s3 \
  -e ROOT_ACCESS_KEY_ID -e ROOT_SECRET_ACCESS_KEY --mount "type=volume,source=$volume,target=/data" \
  "$s3_image" --cert /certs/public.crt --key /certs/private.key --port :9000 \
  --region us-east-1 posix /data >"$logs/s3-create.log" 2>&1
s3_created=1
unset secret access
mkdir -m 700 "$certs/s3"
cp "$certs/public.crt" "$certs/private.key" "$certs/s3/"
docker cp "$certs/s3" "$s3:/certs" >"$logs/s3-tls-copy.log" 2>&1
docker start "$s3" >"$logs/s3-start.log" 2>&1
docker create --platform "$platform" --name "$host" --network "$network" \
  --privileged --cgroupns=private --memory=4g --cpus=2 \
  --tmpfs /run --tmpfs /run/lock --tmpfs /tmp \
  --mount "type=bind,source=$framework/tools/deploy,target=/verification/deploy,readonly" \
  --mount "type=bind,source=$framework,target=/verification/foundry-go,readonly" \
  ${old_tools_mount[@]+"${old_tools_mount[@]}"} \
  "$image" >"$logs/host-create.log" 2>&1
host_created=1
docker start "$host" >"$logs/host-start.log" 2>&1
# PID 1 needs a moment to create its private bus; systemctl fails until then.
for _ in $(seq 60); do
  docker exec "$host" test -S /run/systemd/private 2>/dev/null && break
  sleep 1
done
docker exec "$host" systemctl start multi-user.target >"$logs/systemd.log" 2>&1
docker exec "$host" mkdir -m 700 -p /var/lib/foundry-acceptance-f4a /etc/foundry/acceptance-ca
docker cp "$certs/ca.crt" "$host:/etc/foundry/acceptance-ca/ca.crt" >"$logs/host-tls-copy.log" 2>&1
docker cp "$certs/ca.key" "$host:/etc/foundry/acceptance-ca/ca.key" >>"$logs/host-tls-copy.log" 2>&1
docker exec "$host" chown -R root:root /etc/foundry/acceptance-ca
docker exec "$host" chmod 600 /etc/foundry/acceptance-ca/ca.key /etc/foundry/acceptance-ca/ca.crt
docker exec "$host" cp /etc/foundry/acceptance-ca/ca.crt /usr/local/share/ca-certificates/foundry-acceptance.crt
docker exec "$host" chmod 644 /usr/local/share/ca-certificates/foundry-acceptance.crt
docker exec "$host" update-ca-certificates >"$logs/host-trust.log" 2>&1
docker cp "$archive" "$host:/var/lib/foundry-acceptance-f4a/input.tar.gz" >"$logs/archive-copy.log" 2>&1
docker exec --env-file "$env_file" -i "$host" python3 - >"$logs/s3-ready.log" 2>&1 <<'PY'
import sys, time
sys.path.insert(0, "/verification/deploy/tests")
from acceptance_backup import rclone, s3_settings
settings = s3_settings()
deadline = time.monotonic() + 180
while time.monotonic() < deadline:
    try:
        rclone(settings, "lsd", "acceptance:", "--retries", "1",
               "--low-level-retries", "1", "--contimeout", "5s", "--timeout", "5s", timeout=5)
        break
    except RuntimeError:
        pass
    time.sleep(1)
else:
    raise SystemExit('S3 stand-in readiness deadline exceeded')
PY

phase_on() {
  local target=$1 name=$2 result
  shift 2
  echo "Running $name"
  if docker exec --env-file "$env_file" "$target" python3 -B \
    /verification/deploy/tests/acceptance_phase.py "$@" >"$logs/$name.log" 2>&1; then
    return 0
  else
    result=$?
    # Preserve the failed action's status even when diagnosis is unavailable.
    docker exec --env-file "$env_file" "$target" python3 -B \
      /verification/deploy/tests/acceptance_diagnostics.py >>"$logs/$name.log" 2>&1 || \
      echo 'ACCEPTANCE DIAGNOSTICS: collector unavailable' >>"$logs/$name.log"
    return "$result"
  fi
}
phase() {
  phase_on "$host" "$@"
}
# Refresh before foreign ownership/audit capture; production package planning
# deliberately never changes the repository view underneath its reviewed plan.
docker exec "$host" apt-get update -qq >"$logs/package-index.log" 2>&1
phase foreign-baseline /verification/deploy/tests/acceptance_foreign.py baseline --mode "$mode"
baseline_done=1
host_phase=()
if ((upgrade)); then
  phase upgrade-prepare /verification/deploy/tests/acceptance_host.py \
    --archive /var/lib/foundry-acceptance-f4a/input.tar.gz \
    --prepare-upgrade --tools-root /verification/old-deploy
  phase upgrade /verification/deploy/tests/acceptance_upgrade.py \
    --archive /var/lib/foundry-acceptance-f4a/input.tar.gz --tools-root /verification/old-deploy
  host_phase=(--after-upgrade)
fi
phase host /verification/deploy/tests/acceptance_host.py \
  --archive /var/lib/foundry-acceptance-f4a/input.tar.gz --redis-source /verification/foundry-go \
  ${host_phase[@]+"${host_phase[@]}"}
phase roles /verification/deploy/tests/acceptance_roles.py --archive /var/lib/foundry-acceptance-f4a/input.tar.gz
phase capacity /verification/deploy/tests/acceptance_capacity.py
phase backup /verification/deploy/tests/acceptance_backup.py
phase nginx /verification/deploy/tests/acceptance_nginx.py --rounds 1
phase edge /verification/deploy/tests/acceptance_edge.py

# Set before invocation: a failed verify must not be repeated after it has closed
# its Redis observation window.
foreign_verified=1
phase foreign-verify /verification/deploy/tests/acceptance_foreign.py verify
# A is the already bootstrapped source. B and C use the base image: pgBackRest
# must be installed by recover-apply, and neither has any Foundry resources.
phase recover-source /verification/deploy/tests/acceptance_recover.py source
mkdir -m 700 "$logs/.private-recovery"
docker cp "$host:/var/lib/foundry-acceptance-f4a/recover-export/." "$logs/.private-recovery/" >"$logs/recover-export.log" 2>&1
recovery_target() {
  recovery_host=$1
  recovery_mode=$2
  docker create --platform "$platform" --name "$recovery_host" --network "$network" \
    --privileged --cgroupns=private --memory=4g --cpus=2 \
    --tmpfs /run --tmpfs /run/lock --tmpfs /tmp \
    --mount "type=bind,source=$framework/tools/deploy,target=/verification/deploy,readonly" \
    "$base_image" >"$logs/$recovery_mode-create.log" 2>&1
  recovery_hosts+=("$recovery_host")
  docker start "$recovery_host" >"$logs/$recovery_mode-start.log" 2>&1
  for _ in $(seq 60); do
    docker exec "$recovery_host" test -S /run/systemd/private 2>/dev/null && break
    sleep 1
  done
  docker exec "$recovery_host" systemctl start multi-user.target >"$logs/$recovery_mode-systemd.log" 2>&1
  # Same image-only policy handling as acceptance.Dockerfile, before the target
  # is presented to production recovery. Production never replaces this file.
  docker exec "$recovery_host" sh -c 'if [ -e /usr/sbin/policy-rc.d ]; then mv /usr/sbin/policy-rc.d /usr/sbin/policy-rc.d.acceptance-image; fi'
  docker exec "$recovery_host" mkdir -m 700 -p /var/lib/foundry-acceptance-f4a/recover-export
  docker cp "$logs/.private-recovery/." "$recovery_host:/var/lib/foundry-acceptance-f4a/recover-export/" >"$logs/$recovery_mode-copy.log" 2>&1
  docker cp "$certs/ca.crt" "$recovery_host:/var/lib/foundry-acceptance-f4a/ca.crt" >>"$logs/$recovery_mode-copy.log" 2>&1
  docker cp "$certs/ca.key" "$recovery_host:/var/lib/foundry-acceptance-f4a/ca.key" >>"$logs/$recovery_mode-copy.log" 2>&1
  docker exec "$recovery_host" cp /var/lib/foundry-acceptance-f4a/ca.crt /usr/local/share/ca-certificates/acceptance-root.crt
  docker exec "$recovery_host" chmod 644 /usr/local/share/ca-certificates/acceptance-root.crt
  docker exec "$recovery_host" chmod 600 /var/lib/foundry-acceptance-f4a/ca.key
  docker exec "$recovery_host" update-ca-certificates >"$logs/$recovery_mode-trust.log" 2>&1
  docker exec "$recovery_host" apt-get update -qq >"$logs/$recovery_mode-packages.log" 2>&1
  # Real recovery runs from a root-only staged bundle that service accounts can't read.
  docker exec "$recovery_host" sh -c 'umask 077; mkdir /var/lib/foundry-acceptance-f4a/staged-tools; cp -R /verification/deploy/. /var/lib/foundry-acceptance-f4a/staged-tools/'
  phase_on "$recovery_host" "recover-$recovery_mode" /var/lib/foundry-acceptance-f4a/staged-tools/tests/acceptance_recover.py "$recovery_mode"
}
recovery_target "$rehearsal" rehearsal
phase recover-repository-unchanged /verification/deploy/tests/acceptance_recover.py unchanged
docker stop --time 150 "$host" >"$logs/recover-stop-source.log" 2>&1
[[ "$(docker inspect --format '{{.State.Running}}' "$host")" == false ]] || { echo 'Source A must be stopped before takeover' >&2; exit 1; }
recovery_target "$takeover" takeover
# These are decrypted task-owned artifacts; retain only safe acceptance evidence.
rm -rf "$logs/.private-recovery"
echo 'PASS all acceptance phases, including synthetic CA lineage/renewal/reload; real Cloudflare DNS/ACME and R2 remain separate gates.'
