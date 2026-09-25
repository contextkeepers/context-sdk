// A backend service emitting events. Run the mock first:
//
//	python3 examples/mock_ingest.py
//	cd examples/go && go run .
package main

import (
	"fmt"

	ctxsdk "github.com/contextkeepers/context-sdk/go"
)

func main() {
	client := ctxsdk.New(ctxsdk.Options{
		TenantID:  "org_acme",
		Endpoint:  "http://127.0.0.1:8099/logs/ingest",
		BatchSize: 3,
	})
	// Drains on the way out, which is what sends a batch that never
	// reached its threshold. The error names what it could not send, so
	// a silent loss on every run is not possible.
	defer func() {
		if err := client.Close(); err != nil {
			fmt.Println("shutdown:", err)
		}
	}()

	// The job itself is the actor for anything not tied to a customer.
	job := client.Identify(ctxsdk.ProcessID("nightly_rollup_2026_09_25"))

	records := []struct{ org, user string }{
		{"org_42", "user_1"}, {"org_42", "user_2"}, {"org_77", "user_3"},
	}
	for _, record := range records {
		// A scope per record, narrowed from the job's. The job scope is
		// untouched, so concurrent work cannot cross-attribute.
		actor := job.Identify(
			ctxsdk.OrganizationID(record.org),
			ctxsdk.UserID(record.user),
		)
		actor.Log("rows_rolled_up", map[string]any{"rows": 4096, "duration_ms": 120})
	}
	fmt.Println("done")
}
