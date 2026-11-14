# B24 release 1: lane C recovery handoff

Backup and recovery use the existing typed deployment records. This lane does not
change the dispatcher or lane A files. Native tests cover archived configuration,
coverage, plan binding and orchestration with simulated lifecycle callbacks;
they do not establish real-host recovery or network isolation.

## Dispatcher integration

In `upgrades._check_feature_request`, add exactly these actions to `supported`:
`backup-configure`, `backup-run`, `restore-drill`, `recover-plan`, `recover-apply`.
Both the requested-profile/topology check and registered-instance check use that
set. Keep generation checks, rehearsal write restrictions, confirmation and plan
token checks. Recovery must retain `recover._feature_owner`'s refusal until all
three recovery callbacks and the `start_units`/`stop_units` seams below exist. Backup credential rotations are separate
actions and are not included in this lift.

Add `recovery_public_binding` to `manage.py`'s `recovery_fields` and the arguments
forwarded to both `recover.plan` and `recover.apply`. The client must send the same
reviewed object on both calls, retain the operation UUID on resume, and use the
successful takeover's `profile_update` object to persist the new host identity
and accepted addresses. Keep the source profile and descriptor unchanged in the
archive and operation intent. Rehearsal rejects a public binding and never updates
the local profile. Do not derive addresses from SSH or the source host.

## Lane A call order

`recover.plan` validates the archive and replacement binding, computes
`journal["intent"]["recovery_roles"]`, and requires all three callbacks before
target provisioning. `recover.apply` checks `old_server_gone is True` for takeover
before acquiring the host lock or performing effects. It rechecks the plan token,
persists `old_server_stopped_confirmed_at`, verifies the repository, restores
configuration and PostgreSQL, and prepares Redis before these calls:

1. `feature_lifecycle.prepare_recovery(host, registry, entry, journal, snapshot,
   archive, checksum, tools_dir)` runs in the `application` phase. Its return value
   is ignored. Validate the release checksum, exact archived declaration and
   `entry["recovered_from"]["release_commit"]` before selecting executables. Use
   the source descriptor/profile for archive validation and the separate effective
   target profile for host/address resolution. Install the selected packages and
   artifacts through their existing owners with package daemons suppressed.
   Prepare the unit graph and closed ingress, then call
   `stop_units(host, inventory)` to stop and prove exit of every workload.
   Restore systemd enablement to exactly `recovery_enabled_roles`, and carry
   `recovery_maintenance_holds` into the target admission selection. Neither
   enablement nor holds may default to every placed role. Never migrate or seed
   restored data. Persist resumable preparation evidence under this operation.
2. `feature_lifecycle.activate_recovery(host, registry, entry, journal)` runs in
   the `release` phase, after `application` is recorded complete. Prove the restored
   schema matches the selected release, restore current credential/Redis bindings,
   and call `start_units(host, inventory, journal["intent"]["recovery_roles"], deadline)`
   through existing admission and cleanup owners. This seam checks live systemd
   enablement and does not enable units. Use one bounded deadline. A timeout never
   proves callback exit. Return
   `{"version": <archived deployment release>}`; a different version refuses.
   Takeover must prove private health before public admission. Rehearsal starts
   HTTP and only its opted-in custom prerequisite closure, with private listeners,
   restricted external access and every other role stopped. It must never archive
   WAL, upload backups, enable backup timers or admit duplicate background work.
3. `feature_lifecycle.recovery_ready(host, entry, journal)` must return literal
   `True` only after fresh checks of the exact release, configuration, resolution,
   process identities and private health, plus takeover admission or rehearsal
   isolation. `recover._ready` calls it before the takeover backup, before timers,
   before final success, and on completed-operation replay. False, exceptions or
   incomplete observations block completion. A recovered empty enabled set must
   remain stopped with ingress closed; do not require or start HTTP to manufacture
   health. A historical receipt is insufficient.

The caller retains the host lock. Lane A must serialize any instance-owned work
using the existing host-then-instance lock order. Each callback can be re-entered
after interruption before its phase completion was saved. Reconcile its durable
intent and observed effects; never blindly repeat uncertain work. Lane A owns
failed-start cleanup, withdrawal, process-exit proof and retained obligations.

## Evidence and remaining refusals

Backup reads `services.enabled_units(host, inventory.units)` at archive time,
replaces only the archived selection's enabled-role observation, and preserves its
holds. The registry's cached enablement is never the backup authority. Takeover
keeps that enabled set, suppresses held roles and their dependents, and starts only
the remaining declared roles. Both enabled roles and effective holds are token-bound.

`snapshot` contains `deployment`, `resolution`, `selection`, `declaration`,
`secret_paths` and `service_secret_files`. `secret_paths` maps role to declared
`_FILE` name to immutable path. Backup calls
`runtime_config.service_secret_snapshot_paths(host, entry)` to inventory current
secret values, including retained names. `service_secret_files` records those
value paths; the index stays private. The configuration owner validates values
as root-owned 0640 files for the isolated instance group; `service-secrets.json`
stays root-owned 0600. Missing owner seams refuse instead of guessing paths.

Recovery restores configuration privately, then verifies the isolated group with
`runtime_config._instance_group` and publishes only `app.env`, its `_FILE` values
and current service-secret values with their required 0640 access. It uses
`_read_value_file` and `service_secret_snapshot_paths` to validate restored access
before storage selection and `validate_runtime_environment`. Private intake/index
files and unreferenced historical snapshots stay 0600. Preparation therefore
receives already-readable runtime configuration; it does not repair these readers
after they have run. Lane A owns permissions for regenerated runtime environments.
The original snapshot is retained privately beside the recovery journal as
`source-deployment.json`; it is evidence, not permission to replay old admission.

The effective profile and `RecoveryPublicBinding` are bound to the operation token
and retained separately from the source. `entry["profile"]` carries the effective
profile for subsequent actions; `entry["recovery_public_binding"]` retains the
accepted override. DNS instructions are emitted only after confirmed stop and
private takeover health. `external_pending`/`external_observed`, host readiness
and traffic cutover remain separate; recursive answers never prove provider
ownership or global propagation. No application DNS write is authorized.

`StatePolicy.recovery_owner` is an optional public label of at most 256 UTF-8 bytes,
accepted only for `external` state. An omitted owner preserves old declarations
and their `service_state_external_owner_required` incomplete-coverage finding.
A named owner delegates external recovery; it does not claim those bytes were
backed up or restored by Foundry. Local-unprotected state still blocks complete
coverage and takeover until an explicit restoration-evidence contract exists.

`ExecutableRole.recovery_rehearsal_safe` is a strict boolean, default false.
Opt-in only admits a custom HTTP prerequisite to the planned role set; lane A must
still enforce isolation. Built-in worker/scheduler dependencies always refuse,
and safe custom roles outside HTTP's prerequisite closure remain stopped. Unknown
declaration fields remain errors; omitted defaults do not rewrite source digests.
