"""Lane C checks called by the merged feature scenario; this module starts no containers."""
from pathlib import Path
import os
import stat

from acceptance_support import guard, run, service


def check_copies_consumers(host, entry, inventory, *, consumer, binding_name, producer,
                           secret_variable, public_binding_name=None, config_name=None,
                           previous_paths=None, check_console=False):
    """Check an already activated fixture in a disposable systemd container.

    Inputs:
      host/entry: the current Host and registered entry after activation or resize.
      inventory: A1's UnitIdentity-keyed UnitInventory for that selected resolution.
      consumer: the UnitIdentity of a running builtin fixture consumer (HTTP or scheduler).
      binding_name: its `copies` binding, granting producer's copy-scoped secret.
      producer/secret_variable: executable role name and its declared `_FILE` alias.
      public_binding_name: optional consumer public_listener binding; all public
        listeners in this inventory must be enabled and exposed for this check.
      config_name: optional producer `recreate` INI/text config containing the test
        password literally. The scenario must supply distinct, safe ASCII values.
      previous_paths: optional private snapshot returned by a previous call; use
        after grow/shrink/regrow without rotation to prove retained path identity.
      check_console: HTTP declares binding_name too; verify the console keeps its
        endpoint list but omits the unit-only secret grants.

    Returns the private name-to-path snapshot for the next resize assertion.
    Never put that return value in public acceptance output. No value is printed.
    Call after count 2, growth to 3, shrink to 1 and regrowth to 3. A1 owns those
    transitions and must keep every placed producer enabled during these checks.
    """
    guard()
    import edge
    import firewall
    import listeners
    import runtime_config
    import services

    snapshot = runtime_config.service_secret_snapshot(host, entry)
    if previous_paths is not None:
        assert all(snapshot.get(name) == path for name, path in previous_paths.items()), "Resize discarded a private snapshot"
    delivered = runtime_config.unit_secret_paths(host, entry, inventory.topology,
                                                   paths=snapshot, profile=inventory.profile)
    spec = inventory.specs[consumer]
    binding = next(binding for binding in spec.bindings.copies if binding.name == binding_name)
    expected = [unit for unit in inventory.units if unit.role == producer]
    assert [item.ordinal for item in binding.copies] == [unit.ordinal for unit in expected], "Copy list differs from placement"
    assert expected, "Scenario requires placed copies"
    values, granted_paths = [], []
    for identity, item in zip(expected, binding.copies):
        path = delivered[identity][secret_variable]
        assert item.secret_file == path, "Consumer and producer have different secret selections"
        info = os.stat(path, follow_symlinks=False)
        assert stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o640, "Unsafe copy secret mode"
        assert info.st_uid == 0 and info.st_gid == entry["allocations"]["gid"], "Unsafe copy secret ownership"
        values.append(Path(path).read_bytes().rstrip(b"\n"))
        granted_paths.append(path)
        pid = str(service(inventory.units[identity])["pid"])
        namespace = ["nsenter", "--target", pid, "--mount", "--", "runuser", "-u", entry["name"], "--"]
        run([*namespace, "test", "-r", path], timeout=10)
        assert run([*namespace, "test", "-w", path], timeout=10, check=False).returncode != 0, "Producer secret is writable"
        environment = dict(part.split(b"=", 1) for part in Path(f"/proc/{pid}/environ").read_bytes().split(b"\0") if b"=" in part)
        assert environment.get(secret_variable.encode()) == path.encode(), "Producer received the wrong _FILE path"
        if config_name is not None:
            role = next(role for role in inventory.topology.roles if role.common.name == identity.role)
            config = dict(role.configs)[config_name]
            assert config.mode == "recreate", "Scenario requires a recreate config"
            destination = services.ServicePaths.for_unit(identity, inventory.resolution.id).config(config_name, config.mode, config.path)
            rendered = Path(destination).read_bytes()
            assert values[-1] in rendered, "Recreated config did not receive this copy's value"
        unit_text = run(["systemctl", "cat", inventory.units[identity]], timeout=10).stdout.encode()
        assert values[-1] not in unit_text and values[-1] not in Path(f"/proc/{pid}/cmdline").read_bytes(), "Private value escaped into a unit or argv"
    assert len(set(values)) == len(values), "Scenario must supply distinct copy passwords"
    for identity in expected:
        pid = str(service(inventory.units[identity])["pid"])
        own = delivered[identity][secret_variable]
        for path in granted_paths:
            if path != own:
                result = run(["nsenter", "--target", pid, "--mount", "--", "runuser", "-u", entry["name"], "--", "test", "-r", path],
                             timeout=10, check=False)
                assert result.returncode != 0, "Producer can read a sibling copy's private file"
    leaf = consumer.role + (f".{consumer.ordinal}" if consumer.ordinal != 1 else "")
    descriptor = host.paths.opt(entry["name"]) / "runtime" / inventory.resolution.id / (leaf + ".json")
    artisan = entry["allocations"]["paths"]["application"] + "/releases/" + inventory.deployment.release + "/artisan"
    arguments = [artisan, "feature:copies", binding_name, str(len(expected)), "granted"]
    if public_binding_name is not None:
        arguments.append(public_binding_name)
    pid = str(service(inventory.units[consumer])["pid"])
    run(["nsenter", "--target", pid, "--mount", "--", "runuser", "-u", entry["name"], "--", "env",
         "FOUNDRY_DEPLOY_RUNTIME_FILE=" + str(descriptor), *arguments], timeout=30)
    if public_binding_name is not None:
        claims = listeners.public_claims(inventory.profile)
        firewall.observe(host, inventory.profile.instance_id, claims, bindings=inventory.profile.listeners, opened=True)
        for name, public in inventory.profile.listeners:
            role, listener = name.split(".")
            for ordinal, port in enumerate(public.ports, 1):
                endpoint = next(e for e in inventory.resolution.endpoints if (e.role, e.ordinal, e.listener) == (role, ordinal, listener))
                leaf = "stream-" + name + (f".{ordinal}" if ordinal != 1 else "") + ".conf"
                rendered = host.paths.p(edge.RUNTIME + "/" + inventory.profile.instance_id + "/" + leaf).read_text()
                assert f":{port}" in rendered and f"proxy_pass {endpoint.address}:{endpoint.port};" in rendered, "Public port routes to another copy"
    if check_console:
        console = runtime_config.console_binding_descriptor(host, entry, inventory.resolution.id)
        run(["runuser", "-u", entry["name"], "--", "env", "FOUNDRY_DEPLOY_RUNTIME_FILE=" + str(console),
             artisan, "feature:copies", binding_name, str(len(expected)), "omitted"], timeout=30)
    return dict(snapshot)
