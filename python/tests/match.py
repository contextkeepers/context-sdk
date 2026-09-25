import json
from typing import Any, Optional


def mismatch(actual: Any, expected: Any, path: str = "$") -> Optional[str]:
    """Partial deep match: only the keys a case names are compared, so a
    new wire field does not break every fixture. Two exceptions - arrays
    are exact in length and order, and an empty expected object asserts
    an actually empty one, since otherwise {} would match anything."""
    if isinstance(expected, list):
        if not isinstance(actual, list):
            return f"{path}: expected a list, got {type(actual).__name__}"
        if len(actual) != len(expected):
            return f"{path}: expected {len(expected)} items, got {len(actual)}"
        for i, (got, want) in enumerate(zip(actual, expected)):
            found = mismatch(got, want, f"{path}[{i}]")
            if found:
                return found
        return None

    if isinstance(expected, dict):
        if not isinstance(actual, dict):
            return f"{path}: expected an object, got {json.dumps(actual)}"
        if not expected and actual:
            return f"{path}: expected empty, got {json.dumps(actual)}"
        for key, want in expected.items():
            if key not in actual:
                return f"{path}.{key}: missing"
            found = mismatch(actual[key], want, f"{path}.{key}")
            if found:
                return found
        return None

    # JSON has one number type; 49 and 49.0 are the same value.
    if isinstance(expected, (int, float)) and not isinstance(expected, bool):
        if isinstance(actual, (int, float)) and not isinstance(actual, bool):
            if float(actual) == float(expected):
                return None
    if actual != expected or type(actual) is not type(expected):
        return f"{path}: expected {json.dumps(expected)}, got {json.dumps(actual)}"
    return None
