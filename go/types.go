// Package ctxsdk sends events to Context. The wire format it writes is
// spec/events.md; conformance/cases is that spec made executable.
package ctxsdk

import (
	"fmt"
	"time"
)

// Event is one line on the wire.
type Event struct {
	Timestamp string            `json:"timestamp"`
	TenantID  string            `json:"tenant_id"`
	Event     string            `json:"event"`
	Actor     map[string]string `json:"actor"`
	Metadata  map[string]any    `json:"metadata"`
}

// Batch is always used, even for one event, so a client that buffers
// pays one round trip rather than one per event.
type Batch struct {
	Entries []Event `json:"entries"`
}

// ActorField is one identity set on a scope. Its fields are unexported,
// so the constructors below are the only way to make one - a typo does
// not compile, where a named string type would have accepted any
// constant. Constructors rather than key/value pairs because a variadic
// pair list cannot catch an odd argument count at compile time.
type ActorField struct{ key, value string }

// UserID identifies the person an event belongs to.
func UserID(value string) ActorField { return ActorField{"user_id", value} }

// DeviceID identifies a browser or app install.
func DeviceID(value string) ActorField { return ActorField{"device_id", value} }

// ProcessID identifies a batch job or service run, where no person is
// behind the event.
func ProcessID(value string) ActorField { return ActorField{"process_id", value} }

// OrganizationID identifies the customer's own organisation - not our
// tenant, which is TenantID and is set once on the client.
func OrganizationID(value string) ActorField { return ActorField{"organization_id", value} }

// ShutdownIncompleteError reports events Close could not send before
// its deadline. They are counted in Dropped either way.
type ShutdownIncompleteError struct {
	Abandoned int
}

func (e *ShutdownIncompleteError) Error() string {
	return fmt.Sprintf("closed with %d event(s) unsent", e.Abandoned)
}

// Timer is the part of a scheduled call the client needs back.
type Timer interface {
	Stop() bool
}

// Clock is injectable so the conformance cases can drive interval
// flushing and backoff without sleeping.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
	// Sleep is a blocking wait, so Close can back off inside its
	// deadline. Real time in production, instant under the conformance
	// cases.
	Sleep(d time.Duration)
}

// MaxEntryBytes is one event's budget on the wire. Past this the server
// refuses the entry, so the client drops it at Log instead of batching
// it.
const MaxEntryBytes = 4096

// Result is what one send came back with. Status 0 means the request
// never got an answer. Rejected is how many entries the server refused,
// set only on a 400 that carried a report - the rest of that batch was
// stored.
type Result struct {
	Status   int
	Rejected int
	Reported bool
}

// Transport folds a transport failure into status 0: a refused
// connection and a 500 are the same to a caller that only decides
// whether to retry.
type Transport interface {
	Send(batch Batch) Result
}

// Dispatcher is where a background send runs. Log must never block
// on one.
type Dispatcher interface {
	Submit(fn func())
	Drain()
	Shutdown()
}

// timestamp renders exactly three decimals, which is what the column
// stores.
func timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000") + "Z"
}
