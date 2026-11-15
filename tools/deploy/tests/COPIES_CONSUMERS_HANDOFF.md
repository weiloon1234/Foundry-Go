# Release 2a consumer integration

The release 2a integration wires the consumer contracts into lifecycle, operations,
backup and recovery. The container scenario calls both required lane callbacks.
The sections below record the shared interfaces and historical lane verification.

## A1 integration points

1. `runtime_config.unit_secret_paths(host, entry, topology, paths=private_paths,
   profile=profile)` returns `dict[UnitIdentity, dict[str, str]]`. The inner map
   remains `_FILE` variable to private snapshot path. Pass `secrets[identity]` to
   configuration digests, argument resolution, `render_unit`, and template
   preparation. Carry it through the launch record using A1's identity encoding.
   Builtins have no own `secret_files` map. Candidate configuration revision
   staging passes `profile` to the secret owner, so newly added roles and ordinals
   are checked against candidate placement rather than the current entry.
   The former role-keyed `service_secret_paths` projection is removed. All
   lifecycle callers use the identity-keyed owner and serialize launch maps with
   `UnitIdentity.record_key`.
2. `topology.CopiesBinding` is a separate binding type, not an `EndpointBinding`.
   Skip it in the existing scalar binding resolver. Call
   `runtime_config.resolve_copies_bindings(host, entry, topology, profile, role,
   seed_resolution, paths=private_paths)` and put the returned tuple in
   `RoleBindings(..., copies=...)`. The resolver uses `expand_placement`, TCP
   endpoint ordinals, and Unix endpoint ordinals. It filters removed identities
   even if an old endpoint remains reserved. Empty declared lists are valid.
3. Resolve every `PublicListenerReference` through
   `edge.resolve_public_listener_binding(topology, profile, binding_name, reference)`.
   Do not keep constructing a scalar record from `public.ports[0]` in lifecycle.
   The helper preserves the old singleton record and adds `ports` for count > 1.
4. Copy secrets use ordinary service-secret storage and permissions: root owner,
   instance group, mode 0640, and protected directory ancestry. Staging creates
   `copy-secrets/` as needed. There are no copy-secret sandbox hooks to integrate.
   Each copy gets a distinct value, but every role of the instance can read it
   because roles share one account. Rendered templates share that boundary too.
5. Runtime preparation must validate all delivered copy paths before dispatch,
   including builtin consumers. The owning helper remains
   `runtime_config._read_value_file(host, entry, path)`; never log its result.
   Copy files live under the existing instance configuration directory in
   `copy-secrets/`, with root ownership, instance group and mode 0640. Keep
   unchanged consumer units only when their frozen copy lists and secret paths
   also match. Growth, shrink and secret rotation must update affected enabled
   consumers without starting deliberately stopped units.
6. Extend `unix_sockets.uses_sockets` to recognize `CopiesBinding` with scheme
   `unix`, so those consumers inherit the existing socket-directory unit grant.
   Topology parsing already requires Unix copy consumers to list their provider
   in `requires`. TCP copy lists follow existing endpoint behavior: declarations
   use `requires` when startup must await the provider. Copy edges participate
   in cycle validation even without an explicit prerequisite.
7. Extend the generation-18 predicate in `upgrades.feature_generation` to include
   any `CopiesBinding` or `SecretDeclaration.scope == "copy"`, even at count one.
   `Topology.secrets` values are now frozen `SecretDeclaration(prompt, scope)`
   records; default scope is internally `role`. Existing shared secret names and
   keys are unchanged. Public port list length is checked by
   `topology.public_listener_policies`, shared by profile parsing and edge policy.
8. `write_binding_descriptor` preserves `<role>.json` for ordinal 1 and writes
   `<role>@<ordinal>.json` for later ordinals. Use its returned path in per-unit
   dispatch records. Console selection stays HTTP copy 1. Its derived descriptor
   retains copy endpoints but removes all copy `secret_file` fields, as well as
   `shared_directories`.
9. For template preparation, `feature_runtime.run("prepare", pin, launch)` must select
   `launch["secret_paths"][spec.identity.record_key]` and
   pass that inner `_FILE` map to `runtime_config.prepare_service`. Pass the
   selected instance entry unchanged. The renderer checks the entry's instance
   UUID against `invocation.unit.instance_id` before reading the selected file.
   The foundation-helper test proves distinct recreate-template values for
   identities 1 and 2.

Do not introduce a second inventory for these consumers. Declaration role policy
and `UnitIdentity` placement remain the shared owners from `COPIES_HANDOFF.md`.
The scalar endpoint kind is unchanged; a list declaration uses `copies` explicitly.

## A2 backup and recovery

`service_secret_snapshot_paths` still returns the private index followed by every
current or retained snapshot file. Copy snapshot index keys are
`<record_key>.<secret>` within the instance-owned index. The configuration
namespace is `FOUNDRY_SERVICE_SECRET__COPY__<ROLE>__<ORDINAL>__<SECRET>`.
`runtime_config.service_secret_keys` owns derivation; callers must not recreate it.

Launch and backup `secret_paths` maps use `UnitIdentity.record_key`.
`backup.parse_feature_snapshot` checks exact custom-copy coverage and uses
`MAX_SERVICE_SECRET_PATHS` plus one for the index. Backup and recovery share a
bounded archive entry allowance derived from that owner. Recovery preserves the
nested `copy-secrets/` tree, protected ancestry and exact file modes.
Retain removed ordinal values on shrink and ordinary rollback. A count change is
not permission to erase or rotate a secret. Public reports may explain retention
but must not include private snapshot contents or secret-derived hashes.

## Decided wire and Go shapes

- `bindings.copies` is omitted when empty. A nonempty collection contains
  `{"name": "workers", "copies": [...]}`. Each item has `ordinal`, the existing
  TCP or Unix `endpoint`, `scheme`, and optional `secret_file`. Ordinals are dense,
  unique and ascending. A named binding may contain an empty list.
- `Bindings.Copies(name)` returns an owned `[]Copy`, where `Copy` contains
  `Ordinal int`, `Endpoint Endpoint`, and `SecretFile string`. Missing or ungranted
  names return `fault.Missing`. An omitted secret path becomes an empty string. Explicit null is rejected.
- `PublicListener.Ports` is an owned `[]PublicPort`, with `Ordinal int` and
  `Port uint16`. `Port` remains the first copy's port. Multi-copy wire adds
  `ports: [{"ordinal": 1, "port": ...}, ...]`; singleton wire is unchanged.
  Profile `ports` order selects ordinal, without load balancing. Explicit null or
  malformed `ports` is rejected; only omission selects the singleton default.
- Console descriptors omit copy secret paths and retain the HTTP copy list and
  all other existing HTTP descriptor rules. The omission limits descriptor
  contents, not filesystem access under the shared instance account.

## Container callback

`acceptance_copies_consumers.check` operates on an already active
fixture. Its docstring lists every input. Call it after grow/shrink/regrow with
A1's current identity-keyed inventory. Pass its private returned snapshot as
`previous_paths` to prove reuse without rotation. It checks ordinal lists,
producer `_FILE` delivery, optional recreated config content, file ownership and
modes, consumer secret-file readability and non-writability, endpoint connectivity, optional
public routes/firewall exposure, and console path omission.

The compiled fixture command is `feature:copies <binding> <count>
<present|omitted> [public-binding]`. The callback runs it as the instance account
inside the actual consumer mount namespace. It never passes private values on
argv or prints them. The callback does not start containers or perform lifecycle
transitions. `acceptance_feature.py` calls this helper through growth, shrink and rollback.

## Verification status

Tests are supplied in `test_copies_consumers.py`, `test_service_secrets.py`,
`deployment/copies_test.go`, and `deployment/bindings_test.go`. The fixture command
is in `internal/app/feature_copies.go` and registered in `commands.go`.

Fix round 1 reproduced all 13 reported errors. The twelve maintenance failures
were a production return-type regression: `service_secret_paths` returned identity
keys before its lifecycle callers migrated from role keys. The singleton contract
is restored without changing `test_feature_maintenance` or `test_feature_wiring`.
The original singleton secret-delivery assertion is also restored. Identity-keyed
resolution has one owner, `unit_secret_paths`; the singleton projection refuses
numbered inputs.

The recreate-template failure was a fixture ownership error. Two independent
`profile()` calls generated different instance UUIDs for the entry and placed
units. The fixture now derives its entry from its placed profile and preserves a
consistent profile projection. The template ownership check remains unchanged.

Targeted verification passed all 53 tests with no skips:

```sh
cd tools/deploy/tests
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest test_feature_maintenance test_copies_consumers test_service_secrets test_config_templates
```

The sandboxed `make deploy-tools-check` ran 1,248 tests. All reported lane
failures were fixed; five existing Unix-socket tests failed at `socket.bind` with
`PermissionError: Operation not permitted`. No source or assertions were changed
for those environmental failures. Repeating the requested full check outside
the sandbox passed: **1,248 tests in 94.878 seconds, OK (skipped=15)**. No errors
or failures remain. The earlier sandboxed attempt took 102.672 seconds.

The suite's 15 existing skips are expected on this macOS workspace:

- 14 `test_systemd_contract.SystemdContractTests` require
  `FOUNDRY_REAL_SYSTEMD=1`, root and a real systemd host.
- `test_unix_socket_probe.LinuxSocketProbeTests.test_real_pinned_socket_credentials_and_symlink_swap_after_privilege_drop`
  requires Linux `O_PATH` and `SO_PEERPIDFD`.

No lane C test is skipped. These host-only checks remain with the lead's Linux
acceptance environment; this task does not authorize running containers.
The tech lead reported Go build, deployment vet/tests and fixture build passing
before fix round 1; that round changed no Go source and did not rerun Go checks.
No containers have run. Remaining integration work is limited to the explicit
A1/A2 hooks above, not failing or disabled lane C regression tests.

## Fix round 2

Removed `copy_secret_sandbox`, its tmpfs mask, `PrivateUsers=yes`, per-file
`BindReadOnlyPaths`/`ReadOnlyPaths`, and `prepare_copy_secret_directory`. There
were no production call sites in this branch. The sandbox-policy unit test and
container callback's sibling-read denial assertion were removed. A1 must not add
these removed hooks when integrating this lane.

Copy snapshots retain the existing service-secret owner, instance group, mode
0640, no-follow reads and backup enumeration. Staging still creates the protected
`copy-secrets/` subdirectory. Copies receive distinct values, not an isolation
boundary: every role shares the instance account and can read these secrets,
including values in rendered templates. Console path omission is unchanged and
does not restrict filesystem access. Guides, compatibility notes and the
changelog now state that boundary. The fixture callback uses `present|omitted`
for secret path presence, with ordinary file readability/non-writability checks.

Go's pointer/slice wire fields conflated omitted fields with explicit JSON null.
`secret_file` and public `ports` now retain raw field presence before strict
validation. Explicit null, wrong types and malformed collections return
`fault.Invalid`; omitted fields retain their compatibility defaults. Port entries
still reject unknown fields. Both new null regressions failed before the fix and
pass after it; existing omission and valid-list tests remain green.

Verification for this round:

- Targeted Python command above: **52 tests passed, no skips** (the removed
  isolation-policy test accounts for the decrease from 53).
- `make deploy-tools-check`: **1,247 tests in 98.697 seconds, OK (skipped=15)**.
  It ran outside the sandbox for the existing Unix-socket bind tests. The 15
  existing macOS platform skips have the Linux/systemd reasons listed above;
  no lane C test is skipped and no failures remain.
- All three touched Python modules compile, including the container callback.
- `go build ./...`: passed.
- `go vet ./deployment/...`: passed.
- `go test -count=1 . ./deployment/...`: passed.
- Fixture `GOWORK=off go build ./...`: passed.

Go checks used the existing cache outside the workspace sandbox after a sandboxed
regression run was denied access to a Go build-cache file. No containers ran and
no commit was made. A1's remaining identity-keyed delivery hooks are unchanged;
copy-secret sandbox integration is no longer required.
