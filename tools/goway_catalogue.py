"""Validate the reviewed Go-way compiler catalogue against its source cases."""

import csv
from collections import Counter
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
COLUMNS = ("case", "area", "old typed rule (one line)", "disposition",
           "new-API equivalent or reason", "stage")


# One owner for valid area/stage pairs. Stage 2a denotes an existing negative
# case, while the area still describes the rule it exercises.
AREA_STAGES = {
    "Core reads, writes and codecs": {"2a", "3"},
    "Relations and pivots": {"2a", "4"},
    "Lifecycle, mutators and observers": {"5"},
    "Expressions and aggregates": {"6a"},
    "Joins and correlation": {"2a", "6b"},
    "Rows, CTEs and sets": {"2a", "6c"},
    "Windows and specialized expressions": {"6d"},
    "Bulk writes and conflict policies": {"6e"},
    "Framework integrations": {"7"},
    "Authentication and security": {"—"},
    "Cache, Redis and coordination": {"—"},
    "Events, jobs and messaging": {"—"},
    "HTTP, DTOs and transport contracts": {"—"},
    "Other framework services and contracts": {"—"},
    "Validation, values and localization": {"—"},
}


def code_only(source):
    # Consume strings before comments so comment markers inside literals do not
    # erase code. Import paths are inspected separately before this masking.
    return re.sub(r'"(?:\\.|[^"\\])*"|`[^`]*`|/\*.*?\*/|//[^\n]*', ' ', source, flags=re.S)


def generated_symbols(root=ROOT):
    """Index actual model/projection declarations, not similarly named DTO APIs."""
    symbols = {}
    directories = [root / "tests/fixtures/consumer", root / "countries"]
    for directory in directories:
        for path in directory.rglob("*_foundry.gen.go"):
            source = path.read_text()
            if "FoundryModel" not in source and "ProjectionDefinition[" not in source:
                continue
            # Receiver methods are handled by the explicit boundary checks below.
            module_root = root / "tests/fixtures/consumer" if path.is_relative_to(root / "tests/fixtures/consumer") else root
            module = re.search(r'^module (\S+)', (module_root / 'go.mod').read_text(), re.M)[1]
            package = module + '/' + path.parent.relative_to(module_root).as_posix()
            exports = symbols.setdefault(package, set())
            exports.update(re.findall(r'^func ([A-Z]\w*)[\[(]', source, re.M))
            exports.update(re.findall(r'^type ([A-Z]\w*)\b', source, re.M))
    return symbols


def generated_references(source, symbols):
    code = code_only(source)
    references = set(re.findall(r'\bFoundry(?:Reference|Identity|Model|CreateMutation|UpdateMutation)\s*(?=\()', code))
    references.update(re.findall(r'\b[A-Z]\w*Draft\b|\bQuery[A-Z]\w*\s*(?=\()', code))
    # Exact generated exports also catch hooks, changes, projections, observers
    # and aliases. ValidationFields/ConfigKeys are not model declarations.
    for name in re.findall(r'\b([A-Z]\w*(?:Fields|Relations))\s*\(', code):
        if not name.endswith(("ValidationFields", "ConfigFields")):
            references.add(name)
    for alias, path in re.findall(r'(?:\b([A-Za-z_]\w*)\s+)?"([^"\n]+)"', source):
        qualifier = alias or path.rsplit('/', 1)[-1]
        selected = set(re.findall(r'\b' + re.escape(qualifier) + r'\s*\.\s*([A-Za-z_]\w*)', code))
        references.update(qualifier + '.' + symbol for symbol in selected & symbols.get(path, set()))
        if path == "github.com/weiloon1234/Foundry-Go/countries" or re.search(r'/internal/\w*store$', path):
            qualifier = alias or path.rsplit('/', 1)[-1]
            if re.search(r'\b' + re.escape(qualifier) + r'\s*\.', code):
                references.add(qualifier + ". (framework store)")
    return sorted(reference.strip() for reference in references)


def validate_classification(row, source, symbols):
    name, area, stage = row["case"], row["area"], row["stage"]
    if stage not in AREA_STAGES.get(area, set()):
        raise ValueError(f"{name}: inconsistent area/stage {area!r}/{stage}; use AREA_STAGES")
    if row["disposition"] == "unaffected":
        references = generated_references(source, symbols)
        if references:
            raise ValueError(f"{name}: unaffected references generated data-layer symbols: {', '.join(references)}; classify under the owning port stage")


def cases(root=ROOT):
    directory = root / "tests/fixtures/consumer"
    expected = dict(re.findall(r'\{"([a-z0-9_]+)", "([^"]+)"\}',
                               (directory / "types_test.go").read_text()))
    sources = {p.parent.name: p.read_text() for p in
               (directory / "testdata/compilefail").glob("*/invalid.go")}
    if expected.keys() != sources.keys():
        raise ValueError("compiler source/diagnostic catalogue differs")
    return {name: (sources[name], diagnostic) for name, diagnostic in expected.items()}


def rule(source, diagnostic):
    # Retain the actual failing expression and supporting local types. Imports
    # remain available in the linked invalid.go; diagnostics alone are ambiguous.
    source = re.sub(r'(?s)import\s*\(.*?\)|import\s+[^\n]+', '', source)
    source = re.sub(r'//[^\n]*', '', source)
    source = re.sub(r'^\s*package\s+\w+', '', source)
    return 'Reject `' + ' '.join(source.split()) + '`; expected: ' + diagnostic


def read_mapping(root=ROOT):
    with (root / "blueprint/goway-data-layer/compilefail-mapping.tsv").open() as stream:
        reader = csv.DictReader(stream, delimiter="\t")
        if tuple(reader.fieldnames) != COLUMNS:
            raise ValueError("mapping columns differ")
        return list(reader)


def validate(root=ROOT):
    source = cases(root)
    rows = read_mapping(root)
    names = [row["case"] for row in rows]
    if len(set(names)) != len(names) or set(names) != source.keys():
        raise ValueError("mapping must cover each compiler case exactly once")
    new = {p.parent.name for p in (root / "tests/fixtures/goway/testdata/compilefail").glob("*/invalid.go")}
    symbols = generated_symbols(root)
    for row in rows:
        name, disposition = row["case"], row["disposition"]
        if any(not row.get(column) for column in COLUMNS):
            raise ValueError(f"{name}: empty cell")
        if row[COLUMNS[2]] != rule(*source[name]):
            raise ValueError(f"{name}: source/diagnostic changed; review disposition")
        if disposition not in {"ported", "port", "merge", "obsolete", "unaffected"}:
            raise ValueError(f"{name}: invalid disposition")
        validate_classification(row, source[name][0], symbols)
        if (disposition == "unaffected") != (row["stage"] == "—"):
            raise ValueError(f"{name}: affected rules need an owning stage")
        reason = row[COLUMNS[4]]
        if disposition == "ported" and reason.split(':', 1)[0] not in new:
            raise ValueError(f"{name}: missing new compiler case")
        if disposition == "merge" and (reason.split(':', 1)[0] not in source or reason.split(':', 1)[0] == name):
            raise ValueError(f"{name}: missing merge target")
        if disposition == "obsolete" and len(reason.split()) < 12:
            raise ValueError(f"{name}: explain why the rule cannot arise")
    return rows


def summary(rows):
    dispositions = Counter(r["disposition"] for r in rows)
    areas = Counter(r["area"] for r in rows)
    lines = ["| Disposition | Cases |", "| --- | ---: |"]
    lines += [f"| {name} | {dispositions[name]} |" for name in ("ported", "port", "merge", "obsolete", "unaffected")]
    lines += [f"| **Total** | **{len(rows)}** |", "", "| Area | Cases |", "| --- | ---: |"]
    lines += [f"| {name} | {count} |" for name, count in sorted(areas.items())]
    stages = Counter(r["stage"] for r in rows)
    lines += ["", "| Stage | Cases |", "| --- | ---: |"]
    lines += [f"| {name} | {count} |" for name, count in sorted(stages.items())]
    ported = [r for r in rows if r["disposition"] == "ported"]
    distinct = {r[COLUMNS[4]].split(':', 1)[0] for r in ported}
    lines += ["", f"{len(ported)} old catalogue rows are covered by {len(distinct)} distinct Go-way cases."]
    return '\n'.join(lines)


if __name__ == "__main__":
    print(summary(validate()))
