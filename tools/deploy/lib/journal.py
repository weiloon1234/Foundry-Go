"""Shared journal selection, line limits and command arguments."""
from contracts import require
from topology import selected_units

DEFAULT_LINES = 100
MAX_LINES = 2000
LINES_ERROR = f"LINES must be an integer from 1 to {MAX_LINES}."


def validate_lines(value):
    require(type(value) is int and 1 <= value <= MAX_LINES, "invalid_lines", LINES_ERROR)
    return value


def arguments(entry, role, lines, *, follow=False):
    lines = validate_lines(lines)
    units = selected_units(entry, role).values()
    return ["journalctl", *[arg for unit in units for arg in ("--unit", unit)],
            f"--lines={lines}", *(["--follow"] if follow else []), "--no-pager", "--output=short-iso"]
