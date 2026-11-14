"""Exact-release replacement recovery under the existing admission/cleanup owners."""
from dataclasses import replace
from pathlib import Path
import time
import uuid

import admission
import feature_lifecycle as lifecycle
import feature_maintenance
import feature_network
import operation_lock
import operations
import packages
import recovery_rehearsal
import release
import runtime_config
import services
from contracts import (AdmissionDisposition, AdmissionRecord, Deployment,
                       DeploymentSelection, FEATURE_CAPABILITIES, FEATURE_LIFECYCLE_GENERATION,
                       HelperInvocation, ReleaseDeclaration, RuntimeResolution, ValidationPermit,
                       atomic_json, atomic_write, canonical_digest, contract_uuid, digest, record_document, require)
from topology import ExecutableRole, parse_profile, parse_topology


def context(host, entry, journal):
    require(host.host_lock_fd is not None, 'operation_locked', 'Recovery callbacks require the caller-held host lock.')
    intent = journal['intent']
    operation = contract_uuid(intent['operation_id'])
    origin = entry.get('recovered_from', {})
    require(intent['mode'] in ('takeover', 'rehearsal') and origin.get('operation_id') == operation
            and origin.get('mode') == intent['mode'] and entry['profile'] == intent['effective_profile'],
            'recovery_identity', 'Recovery entry differs from its immutable target intent.')
    require(intent['mode'] != 'takeover' or journal.get('old_server_stopped_confirmed_at'),
            'old_server_confirmation', 'Takeover requires recorded old-server-stopped confirmation.')
    root = operations.directory(host, operation)
    admission.protected_directory(host, root, 0o700)
    return intent, root


def prepared(host, entry, journal):
    intent, root = context(host, entry, journal)
    record = admission.read_owned(host, root / 'recovery.json', lambda v: v)
    require(record['intent_sha256'] == canonical_digest(intent), 'recovery_conflict', 'Recovery preparation belongs to another intent.')
    inventory, launch = lifecycle.load_resolution(host, entry, record['resolution_id'])
    require(inventory.deployment.release == record['manifest']['version'] and
            inventory.profile.projection() == intent['effective_profile'] and
            inventory.resolution.private_revision == entry['configuration_revision'],
            'recovery_conflict', 'Prepared recovery resolution or configuration changed.')
    return record, inventory, launch


def selected_roles(intent, inventory):
    enabled, holds, running = (set(intent[key]) for key in ('recovery_enabled_roles', 'recovery_maintenance_holds', 'recovery_roles'))
    require(enabled <= inventory.roles.keys() and holds <= inventory.roles.keys() and running == enabled - holds,
            'recovery_selection', 'Recovery must retain its exact reviewed enablement and holds.')
    inventory.start_order(enabled)
    inventory.start_order(running)
    if intent['mode'] == 'rehearsal':
        require(not holds and running == recovery_rehearsal.roles(inventory.topology, inventory.roles),
                'recovery_selection', 'Rehearsal selection differs from the safe HTTP prerequisite closure.')
    return enabled, holds, running


def configuration_fingerprint(host, entry):
    values = runtime_config.validate_runtime_environment(host, entry)
    paths = {host.paths.instance_config(entry['name']) / 'app.env',
             *runtime_config.service_secret_snapshot_paths(host, entry)}
    for name, value in values.items():
        if name.endswith('_FILE') and value:
            runtime_config._read_value_file(host, entry, value)
            paths.add(Path(value))
    # Credential-derived fingerprints stay in the mode-0600 operation record.
    return canonical_digest({str(path): digest(path) for path in paths})


def prepare(host, registry, entry, journal, snapshot, archive, checksum, tools_dir):
    intent, root = context(host, entry, journal)
    with host.lock(entry['profile']['instance_id']):
        require(registry['tools']['generation'] >= FEATURE_LIFECYCLE_GENERATION and
                registry['tools']['bundle_sha256'] == intent['bundle'] and checksum == intent['archive_sha256'],
                'host_upgrade_required', 'Recovery requires its reviewed generation-13 bundle and release digest.')
        manifest = release.unpack_archive(host, Path(archive), None, entry, checksum, reviewed=True)
        declaration = ReleaseDeclaration.parse(snapshot['declaration'])
        topology = parse_topology(declaration.topology_json)
        source = parse_profile(intent['source_profile'], topology)
        original = services.UnitInventory(entry['name'], topology, source,
            Deployment.parse(snapshot['deployment']), RuntimeResolution.parse(snapshot['resolution']))
        require(manifest['deployment'] == snapshot['declaration'] and manifest['version'] == original.deployment.release
                and manifest['commit'] == entry['recovered_from']['release_commit']
                and canonical_digest(snapshot['deployment']) == intent['deployment_sha256'],
                'recovery_release_changed', 'Recovery requires the exact archived release, declaration and commit.')
        profile = parse_profile(intent['effective_profile'], topology)
        feature_maintenance.stopped_roles(topology)
        source_path = root / 'recovery-source.json'
        source_evidence = {'intent_sha256': canonical_digest(intent), 'snapshot': snapshot, 'manifest': manifest}
        if source_path.exists():
            require(admission.read_owned(host, source_path, lambda v: v) == source_evidence,
                    'recovery_conflict', 'Recovery source evidence changed.')
        else:
            atomic_json(source_path, source_evidence, mode=0o600)
        if (root / 'request.json').exists():
            # Once startup has an owner, preparation must never replay stops.
            require((root / 'complete.json').exists() and _ready(host, entry, journal),
                    'recovery_reconciliation', 'Activation has an unresolved owner; inspect its cleanup journal before resuming.')
            return
        if not (root / 'recovery.json').exists():
            import provision
            if not packages.binary_available(host, 'nginx') and 'nginx' not in registry['shared']:
                registry['shared']['nginx'] = {'owner': 'foundry', 'installed_by_foundry': True, 'unit': 'nginx.service'}
                host.journal(registry, entry)
            package_plan = packages.inspect(host, feature_network.package_topology(topology), profile)
            packages.install_missing(host, package_plan['missing'], reviewed=package_plan)
            # Only operation-owned unpublished staging is discarded on replay.
            operations.clean_staging(host, entry, intent['operation_id'])
            manifest, declaration, path, roots = lifecycle.prepare_release(host, entry, host.load_state(entry),
                {'archive': str(archive), 'archive_sha256': checksum, 'operation_id': intent['operation_id']},
                recovery_declaration=snapshot['declaration'])
            inventory, secrets = lifecycle.resolve(host, entry, manifest, declaration, path, roots, intent['operation_id'],
                                                   profile=profile.projection())
            lifecycle.save_launch(host, entry, inventory, declaration, roots, secrets)
            atomic_json(root / 'recovery.json', {'schema': 1, 'intent_sha256': canonical_digest(intent),
                        'resolution_id': inventory.resolution.id, 'manifest': manifest,
                        'configuration_sha256': configuration_fingerprint(host, entry)}, mode=0o600)
        record, inventory, launch = prepared(host, entry, journal)
        enabled, holds, running = selected_roles(intent, inventory)
        state = host.load_state(entry)
        attempt = state.get('last_attempt')
        if attempt is None:
            attempt = release.begin(host, entry, state, 'restart', inventory.deployment.release,
                metadata={'operation_id': intent['operation_id'], 'recovery_mode': intent['mode'], 'running_roles': sorted(running)})
        require(attempt.get('operation_id') == intent['operation_id'] and attempt['outcome'] == 'running',
                'recovery_reconciliation', 'Recovery preparation has an unresolved prior operation.')
        store = admission.AdmissionStore(host, inventory.profile.instance_id)
        store.record_operation(attempt)
        selection = DeploymentSelection(inventory.profile.instance_id, FEATURE_LIFECYCLE_GENERATION,
            tuple(sorted(FEATURE_CAPABILITIES)), inventory.resolution.id, None,
            AdmissionRecord(inventory.profile.instance_id, inventory.resolution.id, inventory.resolution.deployment_sha256,
                            inventory.resolution.private_revision, intent['operation_id'], AdmissionDisposition.PENDING),
            tuple(sorted(enabled)), tuple(sorted(holds)))
        transition = lifecycle.transition_request(entry, inventory, intent['operation_id'])
        operations.bind_transition(host, entry, inventory, transition)
        store.begin(selection, inventory.resolution)
        store._write(store.durable / 'transition.json', transition)
        entry['deployment_selection'] = record_document(selection)
        entry['allocations']['http_port'] = next(e.port for e in inventory.resolution.endpoints if e.role == 'http' and e.listener == 'http')
        host.journal(registry, entry)
        import provision
        # A new observation safely reconciles partially installed closed network files.
        package_plan = packages.inspect(host, feature_network.package_topology(topology), profile)
        network = feature_network.plan(host, topology, profile, package_plan,
            entry=entry if intent['mode'] == 'rehearsal' else None)
        feature_network.prepare(host, entry, network, tools_dir, registry['tools']['bundle_sha256'])
        provision._ensure_nginx(host, registry)
        if intent['mode'] == 'takeover':
            provision._install_site(host, registry, entry, tools_dir)
        feature_maintenance.publish_markers(host, inventory, holds)
        lifecycle.install_units(host, registry, entry, inventory, launch, tools_dir)
        feature_network.withdraw(host, entry, transition, inventory.resolution, inventory.units)
        lifecycle.stop_units(host, inventory)
        closure = feature_network.close(host, entry, transition, inventory.resolution, inventory.units)
        store.record_boot_closure(closure)
        store._write(store.durable / 'transition.json', replace(transition, closure_id=closure.id))
        feature_maintenance.clear_markers(host, inventory, holds)
        for name, unit in inventory.units.items():
            host.run(['systemctl', 'enable' if name in enabled else 'disable', unit])
        require(set(services.enabled_units(host, inventory.units)) == enabled,
                'recovery_selection', 'Systemd enablement differs from the recovery plan.')
        atomic_json(store.durable / 'maintenance.json', {'operation_id': intent['operation_id'], 'holds': sorted(holds),
                    'enabled': sorted(enabled), 'running': sorted(enabled)}, mode=0o600)
        atomic_json(store.durable / 'enablement.json', {'schema': 1, 'authority': 'systemd', 'state': 'complete'}, mode=0o600)
        atomic_json(root / 'recovery-prepared.json', {'resolution_id': inventory.resolution.id, 'closure_id': closure.id}, mode=0o600)


def activate(host, registry, entry, journal):
    intent, root = context(host, entry, journal)
    require('application' in journal['completed'], 'recovery_phase', 'Complete recovery preparation before activation.')
    with host.lock(entry['profile']['instance_id']):
        record, inventory, _ = prepared(host, entry, journal)
        preparation = admission.read_owned(host, root / 'recovery-prepared.json', lambda v: v)
        require(preparation['resolution_id'] == inventory.resolution.id, 'recovery_conflict', 'Recovery preparation changed.')
        request = {'schema': 1, 'action': 'recover-activate', 'operation_id': intent['operation_id'],
                   'project': intent['project'], 'profile': entry['profile'], 'recovery_intent': intent,
                   'preparation_sha256': canonical_digest(record),
                   'old_server_stopped_confirmed_at': journal.get('old_server_stopped_confirmed_at')}
        if (root / 'request.json').exists():
            require(admission.read_owned(host, root / 'request.json', lambda v: v) == request,
                    'recovery_conflict', 'Recovery executor request changed.')
            if (root / 'complete.json').exists() and _ready(host, entry, journal):
                return {'version': inventory.deployment.release}
            require(False, 'recovery_reconciliation', 'Recovery activation is unresolved; inspect its pinned cleanup owner. No startup was repeated.')
        owner = operations.prepare(host, registry, entry, request)
    with operation_lock.lend(host, intent['operation_id']):
        result = operations.launch(host, request, owner)
    # The executor wrote the registry while the caller was waiting under the same flock.
    current = host.read_registry()
    registry.clear()
    registry.update(current)
    entry.clear()
    entry.update(current['instances'][intent['profile']['instance_id']])
    require(result.get('version') == inventory.deployment.release, 'recovery_release_changed', 'Recovery activated another release.')
    return {'version': result['version']}


def execute(host, request, pin):
    operations.verify_executor(host, request, pin)
    with operation_lock.acquire(host, request), host.lock(request['profile']['instance_id']):
        registry, entry = host.require_identity(request['project'], request['profile'])
        journal = {'intent': request['recovery_intent'], 'old_server_stopped_confirmed_at': request['old_server_stopped_confirmed_at']}
        record, inventory, launch = prepared(host, entry, journal)
        require(canonical_digest(record) == request['preparation_sha256'], 'recovery_conflict', 'Pinned recovery preparation changed.')
        enabled, holds, running = selected_roles(journal['intent'], inventory)
        require(set(services.enabled_units(host, inventory.units)) == enabled,
                'recovery_selection', 'Systemd enablement changed after recovery preparation.')
        store = admission.AdmissionStore(host, inventory.profile.instance_id)
        selection, resolution = store._selection()
        require(selection.admission.disposition is AdmissionDisposition.PENDING and resolution == inventory.resolution,
                'recovery_reconciliation', 'Recovery admission is not the prepared pending resolution.')
        state = host.load_state(entry)
        require(state['last_attempt']['operation_id'] == request['operation_id'] and state['last_attempt']['outcome'] == 'running',
                'recovery_reconciliation', 'Recovery operation is not pending.')
        path = release.verified_release(host, entry, record['manifest'], reviewed=True)
        env = runtime_config.effective_environment_path(host, entry, resolution)
        release.native_runtime(host, entry)
        release.protect_maps(host, entry, path)
        runtime_config.validate_runtime_environment(host, entry)
        require(configuration_fingerprint(host, entry) == record['configuration_sha256'],
                'recovery_configuration', 'Restored configuration changed after preparation.')
        release.app(host, entry, path, 'config:check', env_file=env)
        require(not release.migration_status(host, entry, path, env_file=env), 'recovery_schema',
                'Restored database schema does not match the archived release; recovery never migrates or seeds.')
        patterns = release.release_redis_inventory(host, entry, path, env_file=env)
        release.widen_redis_grants(host, registry, entry, patterns, state)
        transition = lifecycle.transition_request(entry, inventory, request['operation_id'])
        # Reobserve current-boot exit/closure immediately before issuing a permit.
        closure = feature_network.close(host, entry, transition, resolution, inventory.units)
        store.record_boot_closure(closure)
        state['current'], state['previous'] = record['manifest'], None
        lifecycle.record_selection(host, entry, state, inventory.deployment.release)
        release.switch_to(host, entry, inventory.deployment.release)
        release.phase(host, entry, state, 'health')
        store.record_operation(state['last_attempt'])
        permit = None
        if running:
            permit = ValidationPermit(str(uuid.uuid4()), request['operation_id'], admission.boot_id(host), resolution.id,
                closure.id, lifecycle.current_executor(host), tuple(inventory.specs[n].identity for n in sorted(running)))
            store.issue_permit(permit, closure)
            atomic_write(store.runtime / 'active-permit', permit.id, mode=0o600)
        deadline = time.monotonic() + release.READINESS_SECONDS
        lifecycle.start_units(host, inventory, running, deadline)
        require(not running or services.wait_ready_set(host, inventory, running, deadline),
                'readiness_failed', 'Recovered graph did not pass private readiness.')
        if running:
            feature_network.open_public(host, entry, resolution, closure, deadline=deadline)
        state['status'] = 'stopped' if not running else 'healthy' if running | holds == set(inventory.roles) else 'degraded'
        release.queue_redis_narrowing(state, patterns)
        release.finish(host, entry, state, 'succeeded')
        store.record_operation(state['last_attempt'])
        store.commit()
        if permit:
            store.revoke_permit(permit.id)
            (store.runtime / 'active-permit').unlink()
        entry['deployment_selection'] = record_document(store._selection()[0])
        host.journal(registry, entry)
        release.narrow_redis_grants(host, registry, entry, patterns, state)
        return {'ok': True, 'version': inventory.deployment.release}


def ready(host, entry, journal):
    context(host, entry, journal)
    with host.lock(entry['profile']['instance_id']):
        return _ready(host, entry, journal)


def _ready(host, entry, journal):
    record, inventory, launch = prepared(host, entry, journal)
    enabled, holds, running = selected_roles(journal['intent'], inventory)
    store, selection, current, _ = lifecycle.load(host, entry)
    require(selection.admission.disposition is AdmissionDisposition.COMMITTED and current.resolution == inventory.resolution
            and entry['deployment_selection'] == record_document(selection)
            and set(selection.maintenance_holds) == holds and set(services.enabled_units(host, inventory.units)) == enabled,
            'recovery_readiness', 'Recovered selection, holds or systemd enablement changed.')
    store.require_cleanup_clear()
    store._completed(selection)
    state = host.load_state(entry)
    require(state.get('current') == record['manifest'] and release.current_version(host, entry) == inventory.deployment.release,
            'recovery_release_changed', 'Recovered release selection changed.')
    release.verified_release(host, entry, record['manifest'], reviewed=True)
    runtime_config.validate_runtime_environment(host, entry)
    require(configuration_fingerprint(host, entry) == record['configuration_sha256'],
            'recovery_configuration', 'Current private configuration differs from recovery preparation.')
    for name in inventory.roles:
        runtime_config.effective_environment_path(host, entry, inventory.resolution, name)
        marker = host.paths.p(feature_maintenance.marker_path(inventory.specs[name].identity))
        require(marker.exists() == (name in holds), 'recovery_readiness', 'Recovery maintenance markers changed.')
    import listeners
    listeners._feature_exit(host, inventory, {n: u for n, u in inventory.units.items() if n not in running})
    for name in running:
        observed = host.run(['systemctl', 'show', inventory.units[name], '--property=MainPID,InvocationID', '--no-pager'], timeout=5).stdout
        values = dict(line.split('=', 1) for line in observed.splitlines() if '=' in line)
        identity = values.get('InvocationID', '')
        require(len(identity) == 32 and all(c in '0123456789abcdef' for c in identity), 'recovery_readiness', 'Missing recovered process identity.')
        invocation = admission.read_owned(host, store.runtime / 'admitted' / name / (identity + '.json'), HelperInvocation.parse)
        require(str(invocation.process.pid) == values.get('MainPID') and admission.executor_alive(host, invocation.process)
                and invocation.unit_spec_sha256 == canonical_digest(record_document(inventory.specs[name])),
                'recovery_readiness', 'Recovered process differs from its admission receipt.')
        with store.preparation(invocation) as authorized:
            require(authorized == inventory.resolution, 'recovery_readiness', 'Recovered process admission changed.')
        if recovery_rehearsal.active(entry) and isinstance(inventory.roles[name], ExecutableRole):
            recovery_rehearsal.isolation(host, inventory, name)
    require(not running or services.wait_ready_set(host, inventory, running, time.monotonic() + release.READINESS_SECONDS),
            'readiness_failed', 'Recovered services no longer pass private readiness.')
    if recovery_rehearsal.active(entry):
        recovery_rehearsal.observe(host, entry, inventory)
    else:
        listeners.feature_observe_open(host, entry, inventory, running)
    return True
