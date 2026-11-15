"""Source guard for accidental role-string access to the concrete unit inventory."""
import ast
from pathlib import Path
import unittest


def inventory_key_violations(source):
    findings = []
    for node in ast.walk(ast.parse(source)):
        if isinstance(node, ast.Subscript) and isinstance(node.slice, ast.Constant) and isinstance(node.slice.value, str):
            value = node.value
            if isinstance(value, ast.Attribute) and value.attr in {"units", "specs"}:
                findings.append((node.lineno, "Use inventory.identity(role) to index concrete units or specs."))
            elif isinstance(value, ast.Call) and (
                    isinstance(value.func, ast.Name) and value.func.id == "instance_units" or
                    isinstance(value.func, ast.Attribute) and value.func.attr == "instance_units"):
                findings.append((node.lineno, "Use selected_units(entry, role) to select a role."))
        if not isinstance(node, ast.Call) or not isinstance(node.func, ast.Attribute):
            continue
        method = node.func.attr
        if method in {"start_order", "stop_order", "start_in_order", "prerequisites"} and node.args:
            selection = node.args[0]
            if isinstance(selection, ast.Attribute) and selection.attr == "roles":
                findings.append((node.lineno, "Pass inventory.units or inventory.select(), not role policies."))
        if not isinstance(node.func.value, ast.Name) or node.func.value.id != "services":
            continue
        argument = 1 if method == "render_unit" else 2 if method in {"probe", "wait_ready", "wait_ready_set"} else None
        if argument is not None and len(node.args) > argument:
            selection = node.args[argument]
            if isinstance(selection, ast.Constant) and isinstance(selection.value, str):
                findings.append((node.lineno, "Pass a UnitIdentity selected by the inventory."))
    return findings


class InventoryKeySourceTests(unittest.TestCase):
    def test_guard_detects_previous_role_key_patterns(self):
        for source in ('inventory.units["http"]', 'inventory.specs["relay"]',
                       'instance_units(entry)["http"]', 'inventory.start_order(inventory.roles)',
                       'services.wait_ready(host, inventory, "http", deadline)',
                       'services.render_unit(inventory, "relay", entry, digest)'):
            with self.subTest(source=source):
                self.assertTrue(inventory_key_violations(source))
        self.assertEqual(inventory_key_violations('inventory.units[inventory.identity("http")]'), [])
        self.assertEqual(inventory_key_violations('inventory.roles[identity.role]'), [])
        self.assertEqual(inventory_key_violations('rehearsal.probe(context, target, "container")'), [])

    def test_current_python_callers_use_identity_keys(self):
        root = Path(__file__).resolve().parents[3]
        findings = []
        for path in sorted(root.rglob("*.py")):
            # These are frozen historical implementations, not callers of current UnitInventory.
            if "network_fixtures" in path.parts or any(part in {".git", ".venv", "node_modules"} for part in path.parts):
                continue
            findings.extend(f"{path.relative_to(root)}:{line}: {fix}"
                            for line, fix in inventory_key_violations(path.read_text()))
        self.assertEqual(findings, [], "\n".join(findings))
