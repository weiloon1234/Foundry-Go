"""Recovery seams exercise real owners; machine I/O stays inside WiringHost."""
from contextlib import nullcontext
from dataclasses import replace
import copy
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import uuid

import test_feature_wiring as wiring
import admission
import contracts as c
import feature_lifecycle as lifecycle
import feature_recovery
import operation_lock
import operations
import recovery_rehearsal
import release
import runtime_config
import services
from topology import parse_profile


class FeatureRecoveryTests(wiring.FeatureWiringFixture):
    def test_feature_key_archive_round_trips_effective_http_allocation(self):
        import backup
        import recover
        from test_backup import RECIPIENT
        plan = self.request('bootstrap-plan', topology=self.topology.projection())
        self.request('bootstrap-apply', topology=self.topology.projection(), plan_token=plan['plan_token'], confirm=True)
        _, entry = self.host.require_identity(self.project, self.profile)
        base = self.host.paths.instance_config(entry['name']) / 'app.env'
        original = base.read_bytes()
        self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive))
        _, entry = self.host.require_identity(self.project, self.profile)
        _, _, inventory, _ = lifecycle.load(self.host, entry)
        endpoint = next(e for e in inventory.resolution.endpoints if e.role == 'http' and e.listener == 'http')
        self.assertEqual(entry['allocations']['http_port'], endpoint.port)
        self.assertNotEqual(c.parse_env(original.decode())['APP__HTTP__SERVER__ADDRESS'], f'127.0.0.1:{endpoint.port}')
        backup._write_private(self.host, self.host.paths.p(backup.REPOSITORY), {
            'endpoint': 'https://backup.example.invalid', 'region': 'auto', 'bucket': 'backup-fixture',
            'access_key_id': 'fixture-access', 'secret_access_key': 'fixture-secret', 'cipher_pass': 'c' * 43,
            'host_id': entry['host_id'], 'cluster_token': 'fixture-cluster'})
        backup._write_private(self.host, backup._instance_settings_path(self.host, entry),
                              {'instance_id': self.profile['instance_id'], 'age_recipients': [RECIPIENT]})
        captured = {}
        def encrypt(host, plain, encrypted, recipients):
            captured.update(recover.read_archive(plain))
            encrypted.write_bytes(b'fixture-encrypted')
        # Fake-host runtime files use the temporary root in absolute _FILE paths.
        def paths(name):
            return {**c.instance_paths(name), 'configuration': str(self.host.paths.instance_config(name))}
        host_path = self.host.paths.p
        def rooted(path):
            return Path(path) if Path(path).is_relative_to(self.host.paths.root) else host_path(path)
        with patch.object(backup, 'encrypt_archive', side_effect=encrypt), \
                patch.object(backup, 'upload_archive'), patch.object(backup, 'verify_archive'), \
                patch.object(backup, 'instance_paths', side_effect=paths), \
                patch.object(recover, 'instance_paths', side_effect=paths), \
                patch.object(self.host.paths, 'p', side_effect=rooted):
            receipt = backup.archive_keys(self.host, entry)
            recovered = recover.archive_configuration(self.project, self.profile, captured, entry['topology'])
        self.assertEqual(receipt['status'], 'complete')
        self.assertEqual(recovered['http_port'], endpoint.port)
        self.assertEqual(recovered['feature'].resolution, inventory.resolution)
        self.assertEqual(recovered['metadata']['allocations']['http_port'], endpoint.port)
        self.assertEqual(base.read_bytes(), original)


    def target(self, *, running=('http', 'relay'), holds=(), mode='takeover', public=False, public_binding=False, scheduler=False):
        self.build_archive(independent=True, secrets=True, public=public, public_binding=public_binding,
                           rehearsal_safe=mode == 'rehearsal', scheduler=scheduler)
        if mode == 'rehearsal':
            self.build_archive(secrets=True, rehearsal_safe=True, scheduler=scheduler)
        if public:
            wiring.FeatureWiringTests.configure_firewall(self)
        private = {'configuration': {'FOUNDRY_SERVICE_SECRET__RELAY_TOKEN': 'recovery-secret-canary'}}
        plan = self.request('bootstrap-plan', topology=self.topology.projection())
        self.request('bootstrap-apply', topology=self.topology.projection(), plan_token=plan['plan_token'], confirm=True, secrets=private)
        self.request('activate', archive=str(self.archive), archive_sha256=c.digest(self.archive), confirm=True)
        registry, entry = self.host.require_identity(self.project, self.profile)
        _, selected, inventory, launch = lifecycle.load(self.host, entry)
        selected = replace(selected, enabled_roles=tuple(running), maintenance_holds=tuple(holds))
        snapshot = {'deployment': c.record_document(inventory.deployment), 'resolution': c.record_document(inventory.resolution),
                    'selection': c.record_document(selected), 'declaration': launch['declaration'], 'secret_paths': launch['secret_paths']}
        source_profile = copy.deepcopy(self.profile)
        configuration = self.host.paths.instance_config(entry['name'])
        self.source_host = self.host
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.host = wiring.WiringHost(Path(temporary.name))
        self.profile = copy.deepcopy(source_profile)
        self.profile['host'].update(host_id=str(uuid.uuid4()), address='replacement.example.test')
        if public:
            for listener in self.profile['listeners'].values():
                listener['dns']['public_addresses'] = ['203.0.113.11', '2001:db8:42::11']
            wiring.FeatureWiringTests.configure_firewall(self)
        self.profile = parse_profile(self.profile, self.topology).projection()
        self.envelope = wiring.upgrades.protocol_metadata(wiring.TOOLS)
        # Model the already completed resources phase with the normal owners.
        plan = self.request('bootstrap-plan', topology=self.topology.projection())
        self.request('bootstrap-apply', topology=self.topology.projection(), plan_token=plan['plan_token'], confirm=True, secrets=private)
        registry, entry = self.host.require_identity(self.project, self.profile)
        shutil.copytree(configuration, self.host.paths.instance_config(entry['name']), dirs_exist_ok=True)
        # Physical roots are the fake filesystem boundary; real hosts both use /.
        for file in self.host.paths.instance_config(entry['name']).rglob('*'):
            if file.is_file() and file.suffix in ('.json', '.env'):
                file.write_bytes(file.read_bytes().replace(str(self.source_host.paths.root).encode(), str(self.host.paths.root).encode()))
        operation = str(uuid.uuid4())
        entry['configuration_revision'] = inventory.resolution.private_revision
        entry['recovered_from'] = {'operation_id': operation, 'mode': mode, 'release_commit': 'abcdef12'}
        entry['provisioning']['state'] = 'rehearsal' if mode == 'rehearsal' else 'preparing'
        self.host.journal(registry, entry)
        if mode == 'rehearsal':
            self.host.paths.p('/etc/nginx/sites-enabled/foundry-' + entry['name'] + '.conf').unlink()
            # The recovery resources owner disables background work before application validation.
            environment = self.host.paths.instance_config(entry['name']) / 'app.env'
            values = c.parse_env(environment.read_text())
            values[runtime_config.SCHEDULER_ENABLED] = 'false'
            values.pop(runtime_config.SCHEDULER_ENABLED + '_FILE', None)
            self.host.owned_write(environment, c.dump_env(values), mode=0o640, group=entry['name'])
        self.journal = {'intent': {'project': self.project, 'profile': self.profile, 'source_profile': source_profile,
            'effective_profile': self.profile, 'topology': entry['topology'], 'mode': mode, 'operation_id': operation,
            'archive_sha256': c.digest(self.archive), 'bundle': registry['tools']['bundle_sha256'],
            'recovery_enabled_roles': list(running), 'recovery_roles': sorted(set(running) - set(holds)),
            'recovery_maintenance_holds': list(holds), 'deployment_sha256': c.canonical_digest(snapshot['deployment'])},
            'completed': ['resources', 'database'], 'old_server_stopped_confirmed_at': c.now() if mode == 'takeover' else None}
        self.snapshot = snapshot
        # The fake systemd executor runs in this process. The real FD transport is
        # separately exercised; keep the existing caller lock for simulated units.
        original = operation_lock.acquire
        self.enterContext(patch.object(operation_lock, 'lend', side_effect=lambda *_: nullcontext()))
        self.enterContext(patch.object(operation_lock, 'acquire', side_effect=lambda h, r, **kw:
            nullcontext() if r['action'] == 'recover-activate' else original(h, r, **kw)))
        self.enterContext(patch.object(operations, 'Host', return_value=self.host))
        self.enterContext(patch.object(wiring.feature_runtime, 'Host', return_value=self.host))
        return registry, entry

    def prepare(self, registry, entry):
        lifecycle.prepare_recovery(self.host, registry, entry, self.journal, self.snapshot,
                                   self.archive, c.digest(self.archive), wiring.TOOLS)
        self.journal['completed'].append('application')

    def test_recovery_preserves_archived_template_floor_after_application_rollback(self):
        registry, entry = self.target()
        self.snapshot['selection']['minimum_generation'] = 14
        with self.host.lock('host'):
            self.prepare(registry, entry)
            self.assertEqual(lifecycle.load(self.host, entry)[1].minimum_generation, 14)

    def test_public_binding_recovery_uses_generation_fourteen_and_replacement_profile(self):
        registry, entry = self.target(public=True, public_binding=True)
        with self.host.lock('host'):
            self.prepare(registry, entry)
            _, selection, inventory, _ = lifecycle.load(self.host, entry)
            self.assertEqual(selection.minimum_generation, 14)
            binding, = inventory.specs['http'].bindings.public_listeners
            self.assertEqual(binding.public_addresses, ('203.0.113.11', '2001:db8:42::11'))

    def test_takeover_exact_release_enablement_private_health_and_fresh_replay(self):
        registry, entry = self.target(running=('relay',), public=True)
        with self.host.lock('host'):
            self.prepare(registry, entry)
            _, selection, inventory, launch = lifecycle.load(self.host, entry)
            self.assertEqual(services.enabled_units(self.host, inventory.units), ('relay',))
            self.assertFalse(self.host.running)
            self.assertEqual(launch['declaration'], self.snapshot['declaration'])
            self.assertNotEqual(launch['profile'], launch['declaration']['profile'])
            result = lifecycle.activate_recovery(self.host, registry, entry, self.journal)
            self.assertEqual(result, {'version': inventory.deployment.release})
            self.assertEqual(set(self.host.running), {inventory.units['relay']})
            self.assertTrue(lifecycle.recovery_ready(self.host, entry, self.journal))
            before = list(self.host.started)
            self.assertEqual(lifecycle.activate_recovery(self.host, registry, entry, self.journal), result)
            self.assertEqual(self.host.started, before)
            self.host.run(['systemctl', 'stop', inventory.units['relay']])
            with self.assertRaises(c.DeployError):
                lifecycle.recovery_ready(self.host, entry, self.journal)
        self.assertFalse(any('migrate' in command or 'db:seed' in command for command in self.host.commands))

    def test_empty_selection_stays_stopped_and_closed(self):
        registry, entry = self.target(running=())
        with self.host.lock('host'):
            self.prepare(registry, entry)
            lifecycle.activate_recovery(self.host, registry, entry, self.journal)
            self.assertTrue(lifecycle.recovery_ready(self.host, entry, self.journal))
        self.assertEqual(self.host.started, [])
        self.assertFalse(self.host.running)

    def test_preparation_replay_keeps_resolution_and_holds(self):
        registry, entry = self.target(holds=('relay',))
        with self.host.lock('host'):
            self.prepare(registry, entry)
            _, selected, inventory, _ = lifecycle.load(self.host, entry)
            self.prepare(registry, entry)
            self.assertEqual(lifecycle.load(self.host, entry)[1], selected)
            lifecycle.activate_recovery(self.host, registry, entry, self.journal)
            self.assertTrue(lifecycle.recovery_ready(self.host, entry, self.journal))
            self.assertEqual(set(self.host.running), {inventory.units['http']})

    def test_wrong_source_release_refuses_before_packages_or_units(self):
        registry, entry = self.target()
        self.snapshot['declaration'] = copy.deepcopy(self.snapshot['declaration'])
        self.snapshot['declaration']['profile']['host']['address'] = 'wrong.example.test'
        before = list(self.host.commands)
        with self.host.lock('host'), self.assertRaises(c.DeployError):
            self.prepare(registry, entry)
        self.assertEqual(self.host.commands, before)

    def test_failed_private_health_closes_all_work_and_never_repeats_startup(self):
        registry, entry = self.target()
        with self.host.lock('host'):
            self.prepare(registry, entry)
            with patch.object(services, 'wait_ready', return_value=False), self.assertRaises(c.DeployError):
                lifecycle.activate_recovery(self.host, registry, entry, self.journal)
            self.assertFalse(self.host.running)
            before = list(self.host.started)
            with self.assertRaises(c.DeployError):
                lifecycle.activate_recovery(self.host, registry, entry, self.journal)
            self.assertEqual(self.host.started, before)
            with self.assertRaises(c.DeployError):
                lifecycle.recovery_ready(self.host, entry, self.journal)

    def test_rehearsal_filters_only_custom_roles_and_keeps_ingress_closed(self):
        registry, entry = self.target(mode='rehearsal')
        with self.host.lock('host'):
            self.prepare(registry, entry)
            _, _, inventory, _ = lifecycle.load(self.host, entry)
            for role in ('http', 'relay'):
                content = self.host.paths.p('/etc/systemd/system/' + inventory.units[role]).read_text()
                self.assertEqual('IPAddressDeny=any' in content, role == 'relay')
                self.assertEqual('IPAddressAllow=localhost' in content, role == 'relay')
            with patch.object(recovery_rehearsal, 'isolation', return_value={'proven': True}):
                lifecycle.activate_recovery(self.host, registry, entry, self.journal)
                self.assertTrue(lifecycle.recovery_ready(self.host, entry, self.journal))
            self.assertFalse(self.host.paths.p('/run/foundry/nginx/' + inventory.profile.instance_id + '/http-open.conf').exists())

    def test_unproven_rehearsal_never_dispatches_custom_executable(self):
        registry, entry = self.target(mode='rehearsal')
        with self.host.lock('host'):
            self.prepare(registry, entry)
            with patch.object(recovery_rehearsal, 'isolation', side_effect=c.DeployError('rehearsal_isolation', 'Unsupported BPF')):
                with self.assertRaises(c.DeployError):
                    lifecycle.activate_recovery(self.host, registry, entry, self.journal)
            self.assertFalse(self.host.running)

    def test_rehearsal_with_placed_scheduler_keeps_background_disabled(self):
        registry, entry = self.target(mode='rehearsal', scheduler=True)
        self.assertEqual(entry['profile']['placement']['scheduler'], 1)
        values = runtime_config.validate_runtime_environment(self.host, entry)
        self.assertEqual(values[runtime_config.SCHEDULER_ENABLED], 'false')
        normal = {**entry, 'recovered_from': {**entry['recovered_from'], 'mode': 'takeover'}}
        with self.assertRaises(c.DeployError) as error:
            runtime_config.validate_runtime_environment(self.host, normal)
        self.assertEqual(error.exception.code, 'configuration_identity')
        environment = self.host.paths.instance_config(entry['name']) / 'app.env'
        for setting in ('true', None):
            invalid = dict(values)
            invalid.pop(runtime_config.SCHEDULER_ENABLED)
            if setting is not None:
                invalid[runtime_config.SCHEDULER_ENABLED] = setting
            self.host.owned_write(environment, c.dump_env(invalid), mode=0o640, group=entry['name'])
            with self.subTest(setting=setting), self.assertRaises(c.DeployError) as error:
                runtime_config.validate_runtime_environment(self.host, entry)
            self.assertEqual(error.exception.code, 'configuration_identity')
        self.host.owned_write(environment, c.dump_env(values), mode=0o640, group=entry['name'])
        with self.host.lock('host'):
            self.prepare(registry, entry)
            _, _, inventory, _ = lifecycle.load(self.host, entry)
            self.assertEqual(runtime_config.validate_runtime_environment(self.host, entry)[runtime_config.SCHEDULER_ENABLED], 'false')
            with patch.object(recovery_rehearsal, 'isolation', return_value={'proven': True}):
                lifecycle.activate_recovery(self.host, registry, entry, self.journal)
                self.assertTrue(lifecycle.recovery_ready(self.host, entry, self.journal))
            self.assertNotIn(inventory.units['scheduler'], self.host.running)
            self.assertNotIn(inventory.units['scheduler'], self.host.enabled)
