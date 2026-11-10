# Deployment tools

Applications invoke this bundle through their pinned `go tool foundry deploy`.
The [deployment guide](../../docs/guides/deployment.md) owns operator commands,
confirmation rules, host prerequisites, and recovery procedures.

| Path | Responsibility |
| --- | --- |
| `local/cli.py` | Local release selection, private input, plan review, confirmation, and SSH requests |
| `server/manage.py` | Request validation and registered-instance dispatch |
| `lib/release.py` | Release lifecycle and configuration |
| `lib/state_cleanup.py` | Temporary generation-5 state and private-record cleanup |
| `lib/upgrades.py` | Immutable bundle installation and protocol negotiation |
| `lib/backup.py` | PostgreSQL and encrypted key/config backups |
| `protocol.json` | Wire protocol and tool generation |

Generation 6 (v0.16.0) removes finished queue migration actions, startup admission,
containment, and Redis snapshot capture and restore. Ordinary deploys no longer
inspect job layouts. There are no aliases for removed actions. Generation-6
clients require generation-5 hosts to upgrade before ordinary mutations; the
minimum client generation remains 1, preserving generation-3 registrations.

Use `tools-plan`, confirmed `tools-apply`, then an ordinary deploy. Ready instances
receive new unit templates without startup checks, followed by a daemon reload.
This works with the generation-5 installer as well. Four inert files remain for
its bundle validator, including a successful no-op at the old startup-check path.

New-code mutating state loads and saves remove finished operation state, the old
gate, and only the instance's private migration records. They reset only that
instance's exact failed executor names. Backup rebinding during tool application
can trigger cleanup; an ordinary deploy also triggers it. Reports stay read-only.
External cleanup completes before atomic state publication, so interrupted cleanup
retries from persisted state. Off-host encrypted objects remain untouched. Unfinished
operations must be resolved on generation 5 before upgrading. Step 2 deletes the
four bridge files and this temporary shim after all hosts have upgraded and cleaned up.

Run the local tests and documentation checks from the worktree root:

```sh
python3 -B -m unittest discover -s tools/deploy/tests
bash -n tools/deploy/tests/run_acceptance.sh
make docs-check
```

The default suite uses temporary instance state and process/service fakes.
Real-systemd contracts are opt-in. The lead runs container acceptance separately.
The [acceptance fixture](../../tests/fixtures/deploy-acceptance/) supplies the archive
for every phase. It keeps its published framework pin. On a native arm64 Docker host:

```sh
framework="$PWD"
old_tools="$(go env GOMODCACHE)/github.com/weiloon1234/!foundry-!go@v0.15.3/tools/deploy"
build_info=$(mktemp -d)
python3 tools/deploy/tests/build_acceptance.py --architecture arm64 \
  --archive-file "$build_info/archive.txt"
archive=$(cat "$build_info/archive.txt")
tools/deploy/tests/run_acceptance.sh --ubuntu 24.04 --platform linux/arm64 \
  --archive "$archive" --framework-root "$framework" --foreign \
  --upgrade --tools-root "$old_tools"
tools/deploy/tests/run_acceptance.sh --ubuntu 26.04 --platform linux/arm64 \
  --archive "$archive" --framework-root "$framework" --foreign \
  --upgrade --tools-root "$old_tools"
```

The historical tools must already be present in the Go module cache. `--upgrade`
prepares the published v0.15.3 host with a completed queue migration, two admitted
versions, private records, and a real failed superseded executor. It upgrades through
normal tool planning/application, deploys, and proves cleanup, loaded unit changes,
role health, release retention, and clean reporting. It preserves the registered
client generation of 3. The remaining host, role, backup, edge, and replacement-host
recovery phases then run normally. Omitting `--upgrade` runs those phases directly.

Phase diagnostics identify artisan calls visible in an exception traceback.
Only a failed `config:check` is re-invoked (read-only, ten-second limit); its stderr
sample is limited to 4 KiB/eight lines with only fixed diagnostic phrases exposed.
No failed migration, seed, restore, or other mutation is replayed.

See [container acceptance](../../docs/guides/deployment.md#container-acceptance)
for retained phase logs and the remaining external-service acceptance requirements.
