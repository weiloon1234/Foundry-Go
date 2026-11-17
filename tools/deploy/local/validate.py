"""Offline deployment declaration checks. No runtime resources or private settings."""
from dataclasses import asdict, dataclass, field
import json
from pathlib import Path
import tomllib
from typing import Literal

import artifacts
import feature_maintenance
from config_templates import resolve_reference
from contracts import DeployError, MAX_JSON, check_path, require, slug, validate_project
from declaration_inputs import load_profile, read_declaration, read_declaration_json
from runtime_config import MAX_SERVICE_CONFIG_BYTES, MAX_VALUE_BYTES, render_service_config
from topology import ArgumentKind, ExecutableRole, parse_profile, parse_topology
from upgrades import protocol


@dataclass(frozen=True)
class ValidationError:
    code: str
    detail: str


@dataclass
class DeclarationCheck:
    kind: str
    path: str
    profile: str | None = None
    role: str | None = None
    status: Literal["passed", "failed", "skipped"] = "passed"
    minimum_generation: int | None = None
    errors: list[ValidationError] = field(default_factory=list)

    def run(self, operation):
        try:
            return operation()
        except DeployError as error:
            self.errors.append(ValidationError(error.code, error.detail))
        except OSError:
            self.errors.append(ValidationError("declaration_file", "Declaration file is missing or unreadable."))
        except (ValueError, TypeError, KeyError, AttributeError, RecursionError):
            # Decoder exceptions can contain source values or local paths.
            self.errors.append(ValidationError("invalid_declaration", "Declaration has an invalid value or structure."))
        self.status = "failed"
        return None

    def skip(self):
        self.status = "skipped"
        self.errors.append(ValidationError("validation_dependency", "A required declaration failed validation."))


@dataclass(frozen=True)
class ValidationResult:
    schema: int
    action: Literal["validate"]
    ok: bool
    minimum_generation: int | None
    checks: list[DeclarationCheck]


def _render(root, role, config):
    artifacts._safe_path(config.source)
    def argument_value(argument):
        if argument.kind in (ArgumentKind.LISTENER_PORT, ArgumentKind.COPY_NUMBER):
            return "1"
        return "/synthetic/value"

    def template_value(reference):
        return resolve_reference(role, reference, lambda _: "synthetic-secret",
                                 lambda name, field: {"address": "127.0.0.1", "port": "1",
                                                      "url": "http://127.0.0.1:1", "path": "/synthetic/executable"}[field])

    return len(render_service_config(read_declaration(root, root / config.source, MAX_VALUE_BYTES), config,
                                     argument_value, template_value))


def _release_declarations(topology, profile):
    platform = "linux-" + profile.architecture
    exports = artifacts.release_sources(topology, platform)
    # Digests are build outputs; only declaration paths and their overlap matter here.
    payloads = (f"deploy/artifacts/{name}/" + "0" * 64 + (".tar.gz" if artifact.format == "tar.gz" else ".bin")
                for name, artifact in topology.artifacts)
    artifacts.declared_release_files(topology, exports.values(), payloads)


def validate_declarations(root: Path, environment: str | None = None, bindings: Path | None = None) -> ValidationResult:
    checks = []

    def check(kind, path, profile=None, role=None):
        item = DeclarationCheck(kind, path, profile, role)
        checks.append(item)
        return item

    def read_topology():
        topology = parse_topology(read_declaration_json(root, root / "deploy/topology.json"))
        feature_maintenance.stopped_roles(topology)
        return topology

    project = check("project", "deploy/project.json").run(lambda: validate_project(read_declaration_json(root, root / "deploy/project.json")))
    topology = check("topology", "deploy/topology.json").run(read_topology)
    config = check("configuration", "config/deploy.toml").run(
        lambda: tomllib.loads(read_declaration(root, root / "config/deploy.toml", MAX_JSON).decode("utf-8")))

    def profiles():
        require(environment is None or slug(environment), "invalid_profile", "Select a valid ENVIRONMENT profile name.")
        require(bindings is None or environment is not None, "rehearsal_bindings", "Explicit bindings require ENVIRONMENT.")
        directory = check_path(root / "deploy/environments", "directory")
        paths = [directory / (environment + ".json")] if environment is not None else sorted(directory.glob("*.json"))
        require(paths, "invalid_profile", "Declare at least one environment profile.")
        return paths

    selection = check("selection", "deploy/environments", environment)
    paths = selection.run(profiles) or []
    for path in paths:
        name = path.stem
        item = check("profile", path.relative_to(root).as_posix(), name)

        def read_profile():
            from build_metadata import render_public_env
            value = load_profile(root, name, project)
            render_public_env(value)
            return value

        value = item.run(read_profile)
        parsed = None
        if value is not None:
            if topology is None or project is None:
                item.skip()
            else:
                parsed = item.run(lambda: parse_profile(value, topology, project))
                if parsed is not None:
                    item.minimum_generation = protocol()["minimum_client_generation"]
        release = check("release", "deploy/topology.json", name)
        if parsed is None:
            release.skip()
        else:
            release.run(lambda: _release_declarations(topology, parsed))

        binding_path = bindings if bindings is not None else Path("deploy") / (name + "-bindings.json")
        if not binding_path.is_absolute():
            binding_path = root / binding_path
        # Explicit paths must stay within the application, and never dereference links.
        if bindings is not None or binding_path.exists() or binding_path.is_symlink():
            binding_check = check("rehearsal", "deploy/" + name + "-bindings.json" if bindings is None else "bindings", name)

            def validate_rehearsal():
                from rehearsal import acceptance_profile, network_plan, validate_bindings
                require(binding_path.is_relative_to(root) and ".." not in binding_path.parts,
                        "unsafe_path", "Rehearsal declarations must be inside the application.")
                binding_check.path = binding_path.relative_to(root).as_posix()
                acceptance_profile(project, value, topology.projection())
                network_plan(topology, value)
                validate_bindings(read_declaration_json(root, binding_path), name, config)

            if parsed is None or config is None:
                binding_check.skip()
            else:
                binding_check.run(validate_rehearsal)

    if topology is not None:
        for role in topology.roles:
            if not isinstance(role, ExecutableRole):
                continue
            total = 0
            for _, entry_config in role.configs:
                for declaration in entry_config.iter_files():
                    item = check("service_configuration", declaration.source, role=role.common.name)

                    def render():
                        nonlocal total
                        total += _render(root, role, declaration)
                        require(total <= MAX_SERVICE_CONFIG_BYTES, "service_config_size", "Service configuration view exceeds its bound.")

                    item.run(render)
    profiles_checked = [item for item in checks if item.kind == "profile"]
    minimum = (max(item.minimum_generation for item in profiles_checked)
               if profiles_checked and all(item.minimum_generation is not None for item in profiles_checked) else None)
    return ValidationResult(1, "validate", all(item.status == "passed" for item in checks), minimum, checks)


def validate_action(app_root, environment, format, bindings=None):
    invocation = DeclarationCheck("invocation", ".")

    def validate():
        require(format in ("text", "json"), "validation_format", "FORMAT must be text or json.")
        require(bool(app_root), "invalid_app_root", "Supply an application directory.")
        root = Path(app_root).expanduser().resolve(strict=True)
        require(root.is_dir() and (root / "go.mod").is_file(), "invalid_app_root", "Application directory must contain go.mod.")
        return validate_declarations(root, environment, bindings)

    result = invocation.run(validate)
    if result is None:
        result = ValidationResult(1, "validate", False, None, [invocation])
    if format == "json":
        print(json.dumps(asdict(result), sort_keys=True, indent=2))
    else:
        for item in result.checks:
            scope = " (" + ", ".join(v for v in (item.profile, item.role) if v) + ")" if item.profile or item.role else ""
            print(f"{item.status.upper()}: {item.path}{scope} [{item.kind}]")
            for error in item.errors:
                print(f"  {error.code}: {error.detail}")
        generation = result.minimum_generation if result.minimum_generation is not None else "undetermined"
        print(f"Minimum deploy-tool generation: {generation}")
        print("Declarations valid." if result.ok else "Declaration validation failed.")
    return 0 if result.ok else 1
