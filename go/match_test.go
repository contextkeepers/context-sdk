package ctxsdk

import (
	"encoding/json"
	"fmt"
)

// mismatch is a partial deep match: only the keys a case names are
// compared, so a new wire field does not break every fixture. Two
// exceptions - arrays are exact in length and order, and an empty
// expected object asserts an actually empty one, since otherwise {}
// would match anything.
func mismatch(actual, expected any, path string) string {
	switch want := expected.(type) {
	case []any:
		got, ok := actual.([]any)
		if !ok {
			return fmt.Sprintf("%s: expected an array, got %T", path, actual)
		}
		if len(got) != len(want) {
			return fmt.Sprintf("%s: expected %d items, got %d", path, len(want), len(got))
		}
		for i := range want {
			if found := mismatch(got[i], want[i], fmt.Sprintf("%s[%d]", path, i)); found != "" {
				return found
			}
		}
		return ""

	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: expected an object, got %s", path, encode(actual))
		}
		if len(want) == 0 && len(got) > 0 {
			return fmt.Sprintf("%s: expected empty, got %s", path, encode(got))
		}
		for key, value := range want {
			found, present := got[key]
			if !present {
				return fmt.Sprintf("%s.%s: missing", path, key)
			}
			if problem := mismatch(found, value, path+"."+key); problem != "" {
				return problem
			}
		}
		return ""

	default:
		if actual != expected {
			return fmt.Sprintf("%s: expected %s, got %s", path, encode(expected), encode(actual))
		}
		return ""
	}
}

func encode(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(out)
}
