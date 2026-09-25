// Partial deep match: only the keys a case names are compared, so a new
// wire field does not break every fixture. Two exceptions - arrays are
// exact in length and order, and an empty expected object asserts an
// actually empty one, since otherwise `{}` would match anything.
export function mismatch(actual: unknown, expected: unknown, path = "$"): string | null {
  if (Array.isArray(expected)) {
    if (!Array.isArray(actual)) return `${path}: expected an array, got ${typeof actual}`;
    if (actual.length !== expected.length) {
      return `${path}: expected ${expected.length} items, got ${actual.length}`;
    }
    for (let i = 0; i < expected.length; i++) {
      const found = mismatch(actual[i], expected[i], `${path}[${i}]`);
      if (found) return found;
    }
    return null;
  }

  if (expected !== null && typeof expected === "object") {
    if (actual === null || typeof actual !== "object" || Array.isArray(actual)) {
      return `${path}: expected an object, got ${JSON.stringify(actual)}`;
    }
    const want = expected as Record<string, unknown>;
    const got = actual as Record<string, unknown>;
    const keys = Object.keys(want);
    if (keys.length === 0 && Object.keys(got).length > 0) {
      return `${path}: expected empty, got ${JSON.stringify(got)}`;
    }
    for (const key of keys) {
      if (!(key in got)) return `${path}.${key}: missing`;
      const found = mismatch(got[key], want[key], `${path}.${key}`);
      if (found) return found;
    }
    return null;
  }

  if (actual !== expected) {
    return `${path}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`;
  }
  return null;
}
