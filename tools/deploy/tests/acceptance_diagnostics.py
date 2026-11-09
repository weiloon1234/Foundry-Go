#!/usr/bin/env python3
"""Read-only, redacted diagnostics appended to failed container phase logs."""
import http.client
import json
from pathlib import Path
import re
import sys

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lib'))
from host import Host, wall_clock_budget
import redis_acl
from topology import instance_units
from acceptance_support import guard


def safe_label(value):
    if value is None:
        return None
    return value if isinstance(value, str) and re.fullmatch('[a-z0-9_-]{1,128}', value) else '<redacted>'


def acl_summary(rows, username):
    """Never print ACL objects (keys/commands), client-info, usernames or raw errors."""
    if not isinstance(rows, list) or len(rows) > 128 or not isinstance(username, str):
        raise ValueError('Invalid ACL log summary input')
    counts = {}
    for row in rows:
        if not isinstance(row, dict):
            raise ValueError('Invalid ACL log row')
        if row.get('username') != username:
            continue
        reason = row.get('reason')
        context = row.get('context')
        reason = reason if reason in ('auth', 'key', 'channel', 'command') else 'other'
        context = context if context in ('toplevel', 'multi', 'lua', 'module') else 'other'
        count = row.get('count')
        if type(count) is not int or not 0 < count < 2**63:
            raise ValueError('Invalid ACL log count')
        counts[reason, context] = counts.get((reason, context), 0) + count
    return [{'reason': reason, 'context': context, 'count': count}
            for (reason, context), count in sorted(counts.items())]


def probe_body(payload):
    if len(payload) > 4096:
        return '<body exceeds 4096 bytes>'
    try:
        value = json.loads(payload)
    except (ValueError, UnicodeError):
        return '<redacted non-JSON body>'
    if not isinstance(value, dict):
        return '<redacted non-object body>'
    safe = {}
    for key, item in value.items():
        if key == 'ready':
            safe[key] = item if type(item) is bool else '<redacted>'
        elif key == 'status':
            safe[key] = item if item in ('ready', 'unavailable', 'up', 'down', 'ok') else '<redacted>'
    if len(safe) != len(value):
        safe['redacted_fields'] = len(value) - len(safe)
    return safe


def readiness_sample(entry):
    connection = None
    try:
        port = entry['allocations']['http_port']
        if type(port) is not int or not 0 < port < 65536:
            raise ValueError('Invalid listener port')
        with wall_clock_budget(4):
            connection = http.client.HTTPConnection('127.0.0.1', port, timeout=3)
            connection.request('GET', '/health/ready', headers={'Connection': 'close'})
            response = connection.getresponse()
            payload = response.read(4097)
            return {'status': response.status, 'body': probe_body(payload), 'sample_bytes': len(payload)}
    except Exception:
        return {'status': None, 'body': 'unavailable (unit may already be stopped)'}
    finally:
        if connection is not None:
            connection.close()


def journal_summary(output):
    if len(output.encode()) > 256 * 1024:
        raise ValueError('Journal sample exceeds bound')
    events = []
    known = {'application starting', 'application ready', 'application startup failed',
             'application shutdown started', 'application cleanup failed', 'application cleanup timed out',
             'application stopped', 'application stopped with failures', 'acceptance schedule completed'}
    for line in output.splitlines():
        record = json.loads(line)
        message = record.get('MESSAGE', '')
        if not isinstance(message, str):
            message = ''
        try:
            event = json.loads(message)
        except ValueError:
            event = {}
        if not isinstance(event, dict):
            event = {}
        if event.get('msg') in ('HTTP request completed', 'HTTP request started'):
            continue
        label = event.get('msg')
        summary = label if isinstance(label, str) and label in known else '<redacted message>'
        lifecycle = re.match(r'^(Starting|Started|Stopping|Stopped) ', message)
        if lifecycle:
            summary = 'unit ' + lifecycle[1].lower()
        if message.endswith(': Deactivated successfully.'):
            summary = 'unit deactivated successfully'
        # Retain systemd's process-exit evidence, without its free-form descriptions.
        exit_status = re.search(r'\bcode=(exited|killed|dumped), status=([0-9]{1,3})(?:/[A-Z0-9]+)?', message)
        if exit_status:
            summary = 'process exited: code=' + exit_status[1] + ', status=' + exit_status[2]
        timestamp = record.get('__REALTIME_TIMESTAMP', '')
        events.append({'timestamp_us': timestamp if isinstance(timestamp, str) and re.fullmatch('[0-9]{1,20}', timestamp) else None,
                       'level': event.get('level') if event.get('level') in ('DEBUG', 'INFO', 'WARN', 'ERROR') else None,
                       'message': summary})
    return events[-10:]


def role_journals(host, entry):
    result = {}
    for role, unit in instance_units(entry).items():
        try:
            response = host.run(['journalctl', '--unit', unit, '--no-pager', '--quiet', '--output=json',
                                 '--output-fields=MESSAGE,__REALTIME_TIMESTAMP', '--lines=10',
                                 '--grep=^(?!.*HTTP request (?:completed|started))'], timeout=5)
            result[role] = journal_summary(response.stdout)
        except Exception:
            result[role] = 'unavailable'
    return result


def unit_failure_details(host, unit):
    try:
        response = host.run(['systemctl', 'show', unit,
                             '--property=ActiveState,Result,ExecMainStatus', '--no-pager'], timeout=5)
        values = dict(line.split('=', 1) for line in response.stdout.splitlines() if '=' in line)
        status = values.get('ExecMainStatus', '')
        return {'ActiveState': safe_label(values.get('ActiveState')),
                'Result': safe_label(values.get('Result')),
                'ExecMainStatus': status if re.fullmatch('[0-9]{1,10}', status) else '<redacted>'}
    except Exception:
        return {'ActiveState': 'unavailable', 'Result': 'unavailable', 'ExecMainStatus': 'unavailable'}


def artisan_stderr(stderr):
    # Return fixed diagnostic phrases, never arbitrary config values or paths.
    known = ('schedule declarations require scheduler to be enabled',
             'HTTP declarations require HTTP to be enabled',
             'permission denied', 'connection refused', 'no such file or directory',
             'context deadline exceeded')
    sample = stderr.encode('utf-8', errors='replace')[:4096].decode('utf-8', errors='replace')
    lines = []
    for line in sample.splitlines()[:8]:
        lines.append('; '.join(message for message in known if message.lower() in line.lower()) or '<redacted>')
    return {'lines': lines, 'truncated': len(stderr.encode('utf-8', errors='replace')) > 4096 or len(sample.splitlines()) > 8}


def artisan_failure(error):
    """Identify an in-process artisan failure; only config:check may be retried."""
    trace = error.__traceback__
    while trace is not None:
        frame = trace.tb_frame
        app = frame.f_globals.get('app')
        if (frame.f_code.co_name == 'app' and Path(frame.f_code.co_filename).name == 'release.py'
                and getattr(app, '__code__', None) is frame.f_code):
            values = frame.f_locals
            command = values['command']
            result = {'command': command if re.fullmatch('[a-z][a-z0-9:-]{0,79}', command) else '<redacted>',
                      'instance': safe_label(values['entry']['name']), 'reinvoked': False}
            if command == 'config:check':
                try:
                    response = app(values['host'], values['entry'], values['release'], 'config:check',
                                   env_file=values.get('env_file'), timeout=10, check=False)
                    result.update(reinvoked=True, returncode=response.returncode,
                                  stderr=artisan_stderr(response.stderr))
                except Exception:
                    result['stderr'] = 'read-only diagnostic invocation unavailable'
            else:
                result['stderr'] = 'not reinvoked; only config:check is permitted'
            print('ARTISAN FAILURE DIAGNOSTICS: ' + json.dumps(result, sort_keys=True), flush=True)
            return
        trace = trace.tb_next


def emit_diagnostics(host):
    try:
        registry = host.read_registry(missing=True)
        if registry is None:
            raise ValueError('No registry')
    except Exception:
        print('ACCEPTANCE DIAGNOSTICS: registry unavailable', flush=True)
        return
    try:
        rows = redis_acl._redis(host, 'ACL', 'LOG', '128')
    except Exception:
        rows = None
    for entry in registry['instances'].values():
        result = {'instance': safe_label(entry['name'])}
        try:
            attempt = host.load_state(entry).get('last_attempt') or {}
            result['last_attempt'] = {key: safe_label(attempt.get(key))
                                      for key in ('action', 'phase', 'error', 'outcome')}
        except Exception:
            result['last_attempt'] = 'unavailable'
        try:
            result['recent_acl_log'] = acl_summary(rows, entry['allocations']['redis_user'])
        except Exception:
            result['recent_acl_log'] = 'unavailable'
        result['readiness_sample'] = readiness_sample(entry)
        try:
            result['role_journals'] = role_journals(host, entry)
        except Exception:
            result['role_journals'] = 'unavailable'
        print('ACCEPTANCE DIAGNOSTICS: ' + json.dumps(result, sort_keys=True), flush=True)


if __name__ == '__main__':
    guard()
    emit_diagnostics(Host())
