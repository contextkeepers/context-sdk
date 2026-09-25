package ctxsdk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

type conformanceCase struct {
	Name      string            `json:"name"`
	Why       string            `json:"why"`
	Config    map[string]any    `json:"config"`
	Responses []map[string]any  `json:"responses"`
	Steps     []map[string]any  `json:"steps"`
	Skip      map[string]string `json:"skip"`
	Expect    struct {
		Requests     []any  `json:"requests"`
		RequestCount *int   `json:"request_count"`
		Dropped      *int   `json:"dropped"`
		Error        string `json:"error"`
	} `json:"expect"`
}

// The runner's defaults, not the library's - the two differ on purpose
// (a browser batches smaller than a server), and a case must mean the
// same thing in every language.
const (
	runnerTenantID      = "tenant_test"
	runnerBatchSize     = 20
	runnerFlushInterval = 5000 * time.Millisecond
	runnerMaxQueueSize  = 10000
	runnerMaxRetries    = 3
)

func number(config map[string]any, key string, fallback int) int {
	if raw, ok := config[key]; ok {
		if value, ok := raw.(float64); ok {
			return int(value)
		}
	}
	return fallback
}

func TestConformance(t *testing.T) {
	dir := filepath.Join("..", "conformance", "cases")
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no cases found in %s: %v", dir, err)
	}
	sort.Strings(files)

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		var kase conformanceCase
		if err := json.Unmarshal(raw, &kase); err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}

		t.Run(kase.Name, func(t *testing.T) {
			if reason, skipped := kase.Skip["go"]; skipped {
				t.Skip(reason)
			}
			runCase(t, kase)
		})
	}
}

func runCase(t *testing.T, kase conformanceCase) {
	t.Helper()

	results := make([]Result, 0, len(kase.Responses))
	for _, response := range kase.Responses {
		result := Result{Status: int(response["status"].(float64))}
		if rejected, ok := response["rejected"].(float64); ok {
			result.Rejected = int(rejected)
			result.Reported = true
		}
		results = append(results, result)
	}

	clock := newFakeClock()
	transport := newFakeTransport(results)
	tenantID := runnerTenantID
	if raw, ok := kase.Config["tenant_id"].(string); ok {
		tenantID = raw
	}

	client := New(Options{
		TenantID:      tenantID,
		BatchSize:     number(kase.Config, "batch_size", runnerBatchSize),
		FlushInterval: time.Duration(number(kase.Config, "flush_interval_ms", 5000)) * time.Millisecond,
		MaxQueueSize:  number(kase.Config, "max_queue_size", runnerMaxQueueSize),
		MaxRetries:    number(kase.Config, "max_retries", runnerMaxRetries),
		CloseTimeout: time.Duration(
			number(kase.Config, "close_timeout_ms", 5000)) * time.Millisecond,
		Clock:      clock,
		Transport:  transport,
		Dispatcher: InlineDispatcher{},
	})

	// current is the scope an unqualified step uses; via picks a named
	// one and as stores one without changing current.
	type scope interface {
		Log(event string, metadata map[string]any)
	}
	var current scope = client
	scopes := map[string]*Logger{}

	identifier := func(step map[string]any) *Logger {
		if name, ok := step["via"].(string); ok {
			return scopes[name]
		}
		if logger, ok := current.(*Logger); ok {
			return logger
		}
		return nil
	}

	raised := ""
	for _, step := range kase.Steps {
		switch {
		case step["log"] != nil:
			entry := step["log"].(map[string]any)
			metadata, _ := entry["metadata"].(map[string]any)
			target := current
			if name, ok := step["via"].(string); ok {
				target = scopes[name]
			}
			target.Log(entry["event"].(string), metadata)

		case step["scope"] != nil:
			current = client

		case step["identify"] != nil:
			var fields []ActorField
			for key, value := range step["identify"].(map[string]any) {
				switch key {
				case "user_id":
					fields = append(fields, UserID(value.(string)))
				case "device_id":
					fields = append(fields, DeviceID(value.(string)))
				case "process_id":
					fields = append(fields, ProcessID(value.(string)))
				case "organization_id":
					fields = append(fields, OrganizationID(value.(string)))
				default:
					// Unreachable for a real caller - the constructor
					// would not exist. Only a case Go skips gets here.
					t.Fatalf("case uses actor key %q, which Go cannot express", key)
				}
			}
			var made *Logger
			if from := identifier(step); from != nil {
				made = from.Identify(fields...)
			} else {
				made = client.Identify(fields...)
			}
			if name, ok := step["as"].(string); ok {
				scopes[name] = made
			} else {
				current = made
			}

		case step["advance_ms"] != nil:
			clock.advance(time.Duration(step["advance_ms"].(float64)) * time.Millisecond)

		case step["flush"] != nil:
			client.Flush()

		case step["close"] != nil:
			// The abandoned count is asserted through Dropped, which
			// every language exposes.
			_ = client.Close()

		default:
			t.Fatalf("unknown step: %v", step)
		}
	}

	if kase.Expect.Error != "" {
		if raised != kase.Expect.Error {
			t.Errorf("expected error %q, got %q\n  why: %s", kase.Expect.Error, raised, kase.Why)
		}
	} else if raised != "" {
		t.Errorf("unexpected error: %s", raised)
	}

	if kase.Expect.Requests != nil {
		if found := mismatch(toAny(transport.sent), kase.Expect.Requests, "requests"); found != "" {
			t.Errorf("%s\n  why: %s", found, kase.Why)
		}
	}
	if kase.Expect.RequestCount != nil && len(transport.sent) != *kase.Expect.RequestCount {
		t.Errorf("expected %d requests, got %d\n  why: %s",
			*kase.Expect.RequestCount, len(transport.sent), kase.Why)
	}
	if kase.Expect.Dropped != nil && client.Dropped() != *kase.Expect.Dropped {
		t.Errorf("expected %d dropped, got %d\n  why: %s",
			*kase.Expect.Dropped, client.Dropped(), kase.Why)
	}
}

func toAny(values []any) any {
	out := make([]any, len(values))
	copy(out, values)
	return out
}
