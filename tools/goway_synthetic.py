#!/usr/bin/env python3
"""Generate and measure equivalent old/new Go-way applications on native macOS.

Generation is outside the repository. Measurements refuse reused caches and run
in a declared order, retaining inputs, command logs and compiler resource records.
Legacy generation uses a separate checkout; new apps require only enum generation.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import random
import re
import shlex
import signal
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[1]
FRAMEWORK = "github.com/weiloon1234/Foundry-Go"
MODULE = "foundry.test/gowaysynthetic"
OUTPUT_LIMIT = 256 * 1024 * 1024
FUNCTION_SYMBOL = re.compile(r'\s[Tt]\s')


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def outside(path, repo):
    path, repo = Path(path).resolve(), Path(repo).resolve()
    if path.is_relative_to(repo) or repo.is_relative_to(path):
        raise ValueError("application target must be outside, and not contain, the repository")
    return path


def environment(cache=None):
    env = {k: os.environ[k] for k in ("PATH", "HOME", "TMPDIR", "DEVELOPER_DIR", "LANG", "LC_ALL", "GOMODCACHE") if k in os.environ}
    env.update(GOENV="off", GOWORK="off", GOTOOLCHAIN="local", GOFLAGS="-p=4 -mod=readonly", GOMAXPROCS="4")
    if cache is not None:
        env["GOCACHE"] = str(Path(cache).resolve())
    elif os.environ.get("GOCACHE"):
        env["GOCACHE"] = os.environ["GOCACHE"]
    return env


def run(argv, cwd, env, logs, name, timeout=1800):
    """Bound the whole child process group, elapsed time and captured output."""
    directory = logs / name
    directory.mkdir(parents=True, exist_ok=False)
    start = time.monotonic()
    process, failure = None, None
    with (directory / "stdout.log").open("wb") as out, (directory / "stderr.log").open("wb") as err:
        try:
            process = subprocess.Popen(list(map(str, argv)), cwd=cwd, env=env, stdout=out, stderr=err, start_new_session=True)
            while process.poll() is None:
                if time.monotonic() - start > timeout:
                    raise TimeoutError(f"{name}: command timeout")
                if os.fstat(out.fileno()).st_size + os.fstat(err.fileno()).st_size > OUTPUT_LIMIT:
                    raise RuntimeError(f"{name}: output limit exceeded")
                time.sleep(0.05)
            if os.fstat(out.fileno()).st_size + os.fstat(err.fileno()).st_size > OUTPUT_LIMIT:
                raise RuntimeError(f"{name}: output limit exceeded")
        except BaseException as error:
            failure = error
        finally:
            if process is not None and (failure is not None or process.poll() is None):
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
    record = {"argv": list(map(str, argv)), "cwd": str(cwd), "seconds": time.monotonic() - start,
              "exit": process.returncode if process else None, "failure": str(failure) if failure else None}
    write_json(directory / "result.json", record)
    if failure:
        raise failure
    if record["exit"] != 0:
        raise RuntimeError(f"{name}: exit {record['exit']}; inspect {directory}")
    return (directory / "stdout.log").read_text(), record


def schema(count, seed, handlers=40, reports=5):
    if count < 2 or handlers < 1 or reports < 1:
        raise ValueError("at least two models, one handler and one report are required")
    rng = random.Random(seed)
    return {"seed": seed, "models": count,
            "handlers": [rng.sample(range(count), rng.randint(2, min(5, count))) for _ in range(handlers)],
            "reports": [rng.randrange(1, count) for _ in range(reports)]}


def model_name(index):
    return f"Entity{index:03d}"


def fields(index, count):
    owner = model_name(index)
    result = [("ID", f"model.ID[{owner}]", "Primary", "key"),
              ("Name", "string", "Text", "TextOf"),
              ("Quantity", "int64", "Number", "NumberOf"),
              ("Amount", "decimal.Decimal", "Number", "NumberOf"),
              ("Active", "bool", "Column", "ColumnOf"),
              ("Note", "value.Nullable[string]", "NullableText", "NullableTextOf"),
              ("State", "State", "Enum", "EnumOf")]
    if index > 0:
        parent = model_name(index - 1)
        result += [("ParentID", f"model.ID[{parent}]", "Column", "IDOf"),
                   ("Parent", f"relation.One[{parent}]", "One", "OneOf")]
    if index + 1 < count:
        result += [("Children", f"relation.Many[{model_name(index + 1)}]", "Many", "ManyOf")]
    if index % 2 == 0:
        result += [("CreatedAt", "temporal.DateTime", "CreatedAt", "CreatedAtOf"),
                   ("UpdatedAt", "temporal.DateTime", "UpdatedAt", "UpdatedAtOf")]
    if index % 3 == 0:
        result += [("DeletedAt", "value.Nullable[temporal.DateTime]", "SoftDelete", "SoftDeleteOf")]
    return result


def go_file(package, imports, body):
    return "package " + package + "\n\nimport (\n" + ''.join(f'"{i}"\n' for i in imports) + ")\n\n" + body + "\n"


def model_source(index, count, style):
    name = model_name(index)
    columns = fields(index, count)
    imports = [FRAMEWORK + '/' + p for p in ("model", "decimal", "value", "database/relation")]
    if index % 2 == 0 or index % 3 == 0:
        imports.append(FRAMEWORK + "/temporal")
    imports.append(FRAMEWORK + ("/database/query" if style == "old" else "/database/orm"))
    body = (f"//foundry:model table=entities_{index:03d}\n" if style == "old" else "")
    body += f"type {name} struct {{\n" + ''.join(f'{field} {typ}\n' for field, typ, _, _ in columns) + "}\n"
    if style == "old":
        bindings = []
        if index > 0:
            bindings.append(f"Parent: query.BelongsTo({name}Fields().ParentID, {model_name(index-1)}Fields().ID)")
        if index + 1 < count:
            bindings.append(f"Children: query.HasMany({name}Fields().ID, {model_name(index+1)}Fields().ParentID)")
        body += f"func define{name}Relations() {name}RelationSet {{ return {name}RelationSet{{" + ','.join(bindings) + "} }\n"
    else:
        body += f"type {name}Table struct {{ orm.Table[{name},model.ID[{name}]]\n"
        for field, typ, capability, _ in columns:
            if capability.startswith("Nullable") or capability in ("One", "Many", "SoftDelete"):
                typ = typ[typ.index('[')+1:-1]
            body += f"{field} orm.{capability}[{name},{typ}]\n"
        body += f"}}\nvar {name}s = orm.Define(\"entities_{index:03d}\",func(b orm.Builder[{name}],m *{name}) {name}Table {{\n"
        body += f"table := orm.TableOf(b,&m.ID)\nreturn {name}Table{{Table:table,\n"
        body += ''.join(f"{field}: " + ("orm.PrimaryOf(b,table.Key())" if fn == "key" else f"orm.{fn}(b,&m.{field})") + ",\n" for field, _, _, fn in columns)
        body += "} })\n"
        body += f"var {name}Relations = struct {{\n"
        if index > 0:
            body += f"Parent orm.BelongsTo[{name},{model_name(index-1)}]\n"
        if index + 1 < count:
            body += f"Children orm.HasMany[{name},{model_name(index+1)}]\n"
        body += "}{\n"
        if index > 0:
            body += f"Parent: orm.BelongsToOf({name}s.Parent,{name}s.ParentID,{model_name(index-1)}s.ID),\n"
        if index + 1 < count:
            body += f"Children: orm.HasManyOf({name}s.Children,{name}s.ID,{model_name(index+1)}s.ParentID),\n"
        body += "}\n"
    return go_file("models", imports, body)


def handler_source(number, models, count, style):
    body = 'func Run(ctx context.Context, db *database.DB) error {\n'
    for index in models:
        name = model_name(index)
        query = f"models.QueryEntities{index:03d}()" if style == "old" else f"models.{name}s.Query()"
        table = query if style == "old" else f"models.{name}s"
        field = f"models.{name}Fields()" if style == "old" else table
        relation = "Parent" if index > 0 else "Children"
        relations = f"models.{name}Relations" + ("()" if style == "old" else "")
        body += f"{{\nf := {field}\nq := {query}\nid := model.ID[models.{name}]{{}}\n"
        operations = [f'q.Where(f.Quantity.Gt(0)).OrderBy(f.Name.Asc()).Limit(20).All(ctx,db)',
                      f'{table}.Find(ctx,db,id)', 'q.Count(ctx,db)',
                      f'{table}.Update(ctx,db,id,' + (f'models.{name}Draft{{}}.SetName("updated").SetQuantity(2)' if style == "old" else 'f.Name.Set("updated"),f.Quantity.Set(2)') + ')',
                      f'q.With({relations}.{relation}).Limit(20).All(ctx,db)']
        body += ''.join(f"if _, err := {operation}; err != nil {{return err}}\n" for operation in operations)
        body += "}\n"
    body += "return nil\n}"
    return go_file(f"handler{number:03d}", ["context", FRAMEWORK + "/database", FRAMEWORK + "/model", MODULE + "/models"], body)


def report_source(number, index, style):
    child, parent = model_name(index), model_name(index - 1)
    imports = ["context", FRAMEWORK + "/database", FRAMEWORK + "/model", FRAMEWORK + "/decimal", MODULE + "/models"]
    body = ("//foundry:projection\n" if style == "old" else "")
    body += f'type Row struct {{ ID model.ID[models.{child}]; ParentName string; Amount decimal.Decimal }}\ntype parentSide struct{{}}\ntype childSide struct{{}}\n'
    if style == "old":
        imports.append(FRAMEWORK + "/database/query")
        body += f'''func Run(ctx context.Context, db *database.DB) ([]Row,error) {{
parents := query.As[parentSide](models.QueryEntities{index-1:03d}(),"p")
children := query.As[childSide](models.QueryEntities{index:03d}(),"c")
p,c := models.{parent}FieldsAt(parents.Scope()),models.{child}FieldsAt(children.Scope())
joined := query.InnerJoin(parents,children,query.On(p.ID,c.ParentID))
pf := models.{parent}FieldsAt(query.LeftScope(joined,parents.Scope()))
cf := models.{child}FieldsAt(query.RightScope(joined,children.Scope()))
return ProjectRow(joined).SelectID(query.Value(cf.ID)).SelectParentName(query.Value(pf.Name)).SelectAmount(query.Value(cf.Amount)).Query().All(ctx,db)
}}'''
    else:
        imports += [FRAMEWORK + "/database/orm/rows", FRAMEWORK + "/database/orm/expr"]
        body += f'''type RowFields struct {{ rows.Row[Row]; ID rows.Field[Row,model.ID[models.{child}]]; ParentName rows.Field[Row,string]; Amount rows.Field[Row,decimal.Decimal] }}
var RowSchema = rows.Define(func(b rows.Builder[Row],r *Row) RowFields {{return RowFields{{Row:b.Row(),ID:rows.IDOf(b,&r.ID),ParentName:rows.TextOf(b,&r.ParentName),Amount:rows.NumberOf(b,&r.Amount)}}}})
func Run(ctx context.Context, db *database.DB) ([]Row,error) {{
parents := rows.As[parentSide](models.{parent}s.Query(),"p")
children := rows.As[childSide](models.{child}s.Query(),"c")
p,c := rows.Scope(parents),rows.Scope(children)
joined := rows.InnerJoin(parents,children,rows.On(expr.At(p,models.{parent}s.ID),expr.At(c,models.{child}s.ParentID)))
pf,cf := rows.LeftScope(joined,p),rows.RightScope(joined,c)
return rows.Read(rows.Select(joined,RowSchema,rows.Map(RowSchema.ID,expr.At(cf,models.{child}s.ID)),rows.Map(RowSchema.ParentName,expr.At(pf,models.{parent}s.Name)),rows.Map(RowSchema.Amount,expr.At(cf,models.{child}s.Amount)))).All(ctx,db)
}}'''
    return go_file(f"report{number:03d}", imports, body)


def sources(spec, style):
    count = spec["models"]
    result = {f"models/entity{i:03d}.go": model_source(i, count, style) for i in range(count)}
    # Enum generation is retained by the program; neither style hand-maintains it.
    result["models/state.go"] = 'package models\n//foundry:enum\ntype State string\nconst (Active State="active"; Archived State="archived")\n'
    for i, selected in enumerate(spec["handlers"]):
        result[f"handlers/handler{i:03d}/handler.go"] = handler_source(i, selected, count, style)
    for i, child in enumerate(spec["reports"]):
        result[f"reports/report{i:03d}/report.go"] = report_source(i, child, style)
    # A composition package makes the whole consumer surface reachable together.
    imports = ['context', FRAMEWORK + '/database']
    imports += [f'{MODULE}/handlers/handler{i:03d}' for i in range(len(spec['handlers']))]
    imports += [f'{MODULE}/reports/report{i:03d}' for i in range(len(spec['reports']))]
    body = 'func Run(ctx context.Context, db *database.DB) error {\n'
    body += ''.join(f'if err := handler{i:03d}.Run(ctx,db); err != nil {{return err}}\n' for i in range(len(spec['handlers'])))
    body += ''.join(f'if _,err := report{i:03d}.Run(ctx,db); err != nil {{return err}}\n' for i in range(len(spec['reports'])))
    result['app/app.go'] = go_file('app', imports, body + 'return nil\n}')
    # Time a real source test after the cold build: compile every model query.
    checks = []
    for i in range(count):
        compile_call = (f"QueryEntities{i:03d}().Compile()" if style == 'old' else
                        f"orm.Compile({model_name(i)}s.Query())")
        checks.append(f'{{ statement, err := {compile_call}; if err != nil {{ t.Fatal(err) }}; '
                      f'if !strings.Contains(statement.SQL(), `FROM "entities_{i:03d}"`) {{ t.Fatal("wrong model SQL") }} }}')
    test_imports = ['testing', 'strings'] + ([FRAMEWORK + '/database/orm'] if style == 'new' else [])
    result['models/schema_test.go'] = go_file('models', test_imports,
        'func TestModelQueries(t *testing.T) {\n' + '\n'.join(checks) + '\n}')
    return result


def fingerprint(directory):
    result = {}
    for parent, directories, names in os.walk(directory):
        directories[:] = sorted(d for d in directories if not d.startswith('.') and d not in ('node_modules', 'vendor', '__pycache__'))
        for name in sorted(names):
            path = Path(parent) / name
            if path.suffix == '.go' or name in ('go.mod', 'go.sum', '.foundry-gen.json'):
                result[str(path.relative_to(directory))] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def generate(target, repo, spec, go='go', old_repo=None, styles=('old', 'new')):
    if Path(target).is_symlink():
        raise ValueError("application target must not be a symbolic link")
    repos = {'old': Path(old_repo or repo).resolve(), 'new': Path(repo).resolve()}
    target = outside(target, repo)
    for style in styles:
        outside(target, repos[style])
    target.mkdir(parents=True, exist_ok=False)
    env, logs = environment(), target / '.logs'
    for style in styles:
        source_repo = repos[style]
        # Main predates the goway fixture; use its own consumer dependency graph.
        fixture = source_repo / 'tests/fixtures/goway'
        if not (fixture / 'go.mod').exists():
            fixture = source_repo / 'tests/fixtures/consumer'
        mod = (fixture / 'go.mod').read_text()
        version = re.search(r'^go (.+)$', (source_repo / 'go.mod').read_text(), re.M)[1]
        mod = re.sub(r'^module .+$', 'module ' + MODULE, mod, flags=re.M)
        mod = re.sub(r'^go .+$', 'go ' + version, mod, flags=re.M)
        # Fixture-only plugin modules are not synthetic dependencies. Keep the
        # framework graph, replacing only its local root.
        mod = re.sub(r'^\s*foundry\.test/[^\n]+\n', '', mod, flags=re.M)
        mod = re.sub(r'^replace .+\n?', '', mod, flags=re.M)
        mod += '\nreplace ' + FRAMEWORK + ' => ' + json.dumps(str(source_repo)) + '\n'
        directory = target / style
        for name, content in sources(spec, style).items():
            path = directory / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        (directory / 'go.mod').write_text(mod)
        (directory / 'go.sum').write_bytes((fixture / 'go.sum').read_bytes())
        run(['gofmt', '-w', *sorted(directory.rglob('*.go'))], source_repo, env, logs, style + '-format')
        # Legacy models/projections stay in main. New apps generate enums only.
        run([go, 'run', './cmd/foundry', 'generate', '--recursive', '--dir', directory], source_repo, env, logs, style + '-generate')
    write_json(target / 'synthetic.json', {'format': 2, 'repo': str(repo), 'spec': spec,
               'repos': {s: str(repos[s]) for s in styles},
               'framework_sources': {s: fingerprint(repos[s]) for s in styles},
               'sources': {s: fingerprint(target / s) for s in styles}})
    return target


def tool_wrapper(arguments, directory):
    """Go -toolexec adapter; /usr/bin/time records each compile's peak RSS."""
    if not arguments:
        raise ValueError('missing Go tool')
    if Path(arguments[0]).name != 'compile' or '-V=full' in arguments:
        os.execv(arguments[0], arguments)
    directory = Path(directory)
    resource = directory / f'{os.getpid()}.time'
    completed = subprocess.run(['/usr/bin/time', '-l', '-o', str(resource), *arguments], check=False)
    data = resource.read_text()
    peak = re.search(r'^\s*(\d+)\s+maximum resident set size\s*$', data, re.M)
    package = arguments[arguments.index('-p') + 1] if '-p' in arguments else ''
    write_json(directory / f'{os.getpid()}.json', {'package': package, 'rss_bytes': int(peak[1]) if peak else None, 'exit': completed.returncode})
    return completed.returncode


def archive_functions(lines):
    return sum(bool(FUNCTION_SYMBOL.search(line)) for line in lines)


def stream_functions(argv, cwd, env, logs, name, timeout=1800):
    """Count nm function symbols without retaining stdout; a generated package's
    listing can exceed OUTPUT_LIMIT by gigabytes. Stderr stays bounded and logged."""
    directory = logs / name
    directory.mkdir(parents=True, exist_ok=False)
    start, expired, functions, failure = time.monotonic(), threading.Event(), None, None
    with (directory / "stderr.log").open("wb") as err:
        process = subprocess.Popen(list(map(str, argv)), cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=err,
                                   start_new_session=True, text=True, errors="replace")

        def expire():
            expired.set()
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass

        timer = threading.Timer(timeout, expire)
        timer.start()
        try:
            functions = archive_functions(process.stdout)
            process.wait()
            if expired.is_set():
                raise TimeoutError(f"{name}: command timeout")
            if os.fstat(err.fileno()).st_size > OUTPUT_LIMIT:
                raise RuntimeError(f"{name}: output limit exceeded")
        except BaseException as error:
            failure = error
        finally:
            timer.cancel()
            if process.poll() is None:
                expire()
                process.wait()
            process.stdout.close()
    record = {"argv": list(map(str, argv)), "cwd": str(cwd), "seconds": time.monotonic() - start,
              "exit": process.returncode, "failure": str(failure) if failure else None, "functions": functions}
    write_json(directory / "result.json", record)
    if failure:
        raise failure
    if record["exit"] != 0:
        raise RuntimeError(f"{name}: exit {record['exit']}; inspect {directory}")
    return functions


def cache_bytes(directory):
    return sum(path.stat().st_size for path in directory.rglob('*') if path.is_file())


def totals(packages, prefix):
    selected = [p for p in packages if p['package'] == prefix or p['package'].startswith(prefix + '/')]
    return {'packages': len(selected), 'archive_bytes': sum(p['archive_bytes'] for p in selected),
            'functions': sum(p['functions'] for p in selected),
            'max_compile_rss_bytes': max((p['compile_rss_bytes'] for p in selected), default=0)}


def markdown(report):
    lines = ['# Synthetic Go-way build measurement', '',
             f"Seed {report['spec']['seed']}; {report['spec']['models']} models; {len(report['spec']['handlers'])} handlers; {len(report['spec']['reports'])} reports.",
             '', 'Cold build caches; shared dependency downloads. Order: ' + ', '.join(report['profiles']) + '. Function counts are archive T/t symbols, including wrappers, not unique source functions.',
             'Compiler RSS is the maximum of individual compiler processes, not aggregate concurrent RSS. Archive totals do not represent a linked executable.', '',
             '| Style | Build seconds | Test seconds (build-warmed) | Cache bytes | Models bytes / functions | Handlers bytes / functions | Max compiler RSS bytes |',
             '| --- | ---: | ---: | ---: | --- | --- | ---: |']
    for style, result in report['profiles'].items():
        m, h = result['models'], result['handlers']
        lines.append(f"| {style} | {result['cold_build_seconds']:.3f} | {result['warm_build_test_seconds']:.3f} | {result['cache_bytes']} | {m['archive_bytes']} / {m['functions']} | {h['archive_bytes']} / {h['functions']} | {result['max_compile_rss_bytes']} |")
    lines += ['', 'Per-package archives, compiler RSS and source fingerprints are in `measurement.json`. Logs retain the actual command and exit status. This is one build per style, not a statistical speed comparison.', '']
    return '\n'.join(lines)


def measure(target, cache, go='go', timeout=3600, order=None):
    if platform.system() != 'Darwin':
        raise ValueError('compile RSS measurement requires native macOS /usr/bin/time -l')
    if Path(cache).is_symlink():
        raise ValueError('measurement cache must not be a symbolic link')
    target, cache = Path(target).resolve(), Path(cache).resolve()
    manifest = json.loads((target / 'synthetic.json').read_text())
    if manifest['format'] not in (1, 2):
        raise ValueError('unsupported synthetic manifest')
    outside(target, manifest['repo'])
    order = tuple(order or (s for s in ('old', 'new') if s in manifest['sources']))
    if len(order) != len(set(order)) or set(order) != set(manifest['sources']):
        raise ValueError('measurement order must contain every generated style exactly once')
    repos = manifest.get('repos', {s: manifest['repo'] for s in order})
    for style in order:
        outside(target, repos[style])
        if 'framework_sources' in manifest and fingerprint(Path(repos[style])) != manifest['framework_sources'][style]:
            raise ValueError(f'{style}: framework changed since generation')
        if fingerprint(target / style) != manifest['sources'][style]:
            raise ValueError(f'{style}: generated sources changed since generation')
    if cache.is_relative_to(target) or target.is_relative_to(cache):
        raise ValueError('cache must be separate from the generated source tree')
    # Refuse reused caches, including symlinks; never delete user data to get cold.
    cache.mkdir(parents=True, exist_ok=False)
    output = target / 'measurement'
    output.mkdir(exist_ok=False)
    report = {'format': 1, 'spec': manifest['spec'], 'sources': manifest['sources'],
              'framework_sources': {s: fingerprint(Path(repos[s])) for s in order},
              'repos': repos,
              'platform': platform.platform(), 'machine': platform.machine(),
              'cpu_count': os.cpu_count(), 'load_average_start': os.getloadavg(), 'goparallel': 4, 'gomaxprocs': 4, 'profiles': {}}
    logs = output / 'logs'
    report['toolchain'] = run([go, 'version'], target, environment(), logs, 'toolchain')[0].strip()
    for style in order:
        load_start = os.getloadavg()
        module, env = target / style, environment(cache / style)
        rss = output / (style + '-compile')
        rss.mkdir()
        wrapper = shlex.join([sys.executable, str(Path(__file__).resolve()), '_tool', str(rss)])
        flags = ['-p=4', '-toolexec=' + wrapper]
        _, build = run([go, 'build', *flags, './...'], module, env, logs, style + '-build', timeout)
        compiles = [json.loads(p.read_text()) for p in rss.glob('*.json')]
        if not compiles or any(p['exit'] or p['rss_bytes'] is None for p in compiles):
            raise ValueError('missing or failed compiler resource records')
        compiled = {p['package']: p['rss_bytes'] for p in compiles}
        size = cache_bytes(cache / style)
        listing, _ = run([go, 'list', '-export', *flags, '-f', '{{.ImportPath}}\t{{.Export}}', './...'], module, env, logs, style + '-exports', timeout)
        if len(list(rss.glob('*.json'))) != len(compiles):
            raise ValueError('export listing compiled additional packages outside the measured build')
        packages = []
        for i, line in enumerate(listing.splitlines()):
            package, archive = line.split('\t')
            functions = stream_functions([go, 'tool', 'nm', archive], module, env, logs, f'{style}-nm-{i}', timeout)
            if package not in compiled:
                raise ValueError(f'no compiler RSS for {package}')
            packages.append({'package': package, 'archive_bytes': Path(archive).stat().st_size,
                             'functions': functions, 'compile_rss_bytes': compiled[package]})
        _, test = run([go, 'test', *flags, '-count=1', './...'], module, env, logs, style + '-test', timeout)
        report['profiles'][style] = {'warm_build_test_seconds': test['seconds'],
                                     'cache_bytes_after_test': cache_bytes(cache / style), 'cold_build_seconds': build['seconds'], 'cache_bytes': size,
                                     'load_average_start': load_start, 'load_average_end': os.getloadavg(),
                                     'max_compile_rss_bytes': max(p['rss_bytes'] for p in compiles),
                                     'models': totals(packages, MODULE + '/models'),
                                     'handlers': totals(packages, MODULE + '/handlers'),
                                     'reports': totals(packages, MODULE + '/reports'),
                                     'whole_app': totals(packages, MODULE), 'packages': packages}
        write_json(output / 'measurement.json', report)
    for style in order:
        if fingerprint(target / style) != manifest['sources'][style] or fingerprint(Path(repos[style])) != report['framework_sources'][style]:
            raise ValueError(f'{style}: sources changed during measurement')
    report['complete'] = True
    write_json(output / 'measurement.json', report)
    (output / 'summary.md').write_text(markdown(report))
    return output


def main():
    if len(sys.argv) > 1 and sys.argv[1] == '_tool':
        return tool_wrapper(sys.argv[3:], sys.argv[2])
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    create = commands.add_parser('generate')
    create.add_argument('target', type=Path)
    create.add_argument('--repo', type=Path, default=ROOT)
    create.add_argument('--models', type=int, default=70)
    create.add_argument('--handlers', type=int, default=40)
    create.add_argument('--reports', type=int, default=5)
    create.add_argument('--seed', type=int, default=1)
    create.add_argument('--go', default='go')
    create.add_argument('--old-repo', type=Path, help='main checkout with legacy model/projection generator')
    create.add_argument('--style', choices=('both', 'old', 'new'), default='both')
    bench = commands.add_parser('measure')
    bench.add_argument('target', type=Path)
    bench.add_argument('--cache', type=Path, required=True)
    bench.add_argument('--go', default='go')
    bench.add_argument('--timeout', type=int, default=3600)
    bench.add_argument('--order', help='comma-separated old,new or new,old (all generated styles)')
    args = parser.parse_args()
    if args.command == 'generate':
        result = generate(args.target, args.repo.resolve(), schema(args.models, args.seed, args.handlers, args.reports), args.go,
                          args.old_repo, ('old', 'new') if args.style == 'both' else (args.style,))
    else:
        result = measure(args.target, args.cache, args.go, args.timeout, args.order.split(',') if args.order else None)
    print(result)
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, RuntimeError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
