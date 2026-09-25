package ctxsdk

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

const DefaultEndpoint = "https://clogging.staging.us-west-2.contextkeepers.com/logs/ingest"

// Long enough to cover a wait_for_async_insert flush, short enough that
// a hung sink does not pin the worker for a minute.
const requestTimeout = 10 * time.Second

type httpTransport struct {
	endpoint string
	client   *http.Client
}

// NewHTTPTransport is exported so a caller can point the client at
// their own endpoint or wrap it for tests.
func NewHTTPTransport(endpoint string) Transport {
	return &httpTransport{
		endpoint: endpoint,
		client:   &http.Client{Timeout: requestTimeout},
	}
}

func (t *httpTransport) Send(batch Batch) Result {
	body, err := json.Marshal(batch)
	if err != nil {
		// Unserialisable metadata is the caller's, and not retryable.
		return Result{Status: http.StatusBadRequest}
	}
	resp, err := t.client.Post(t.endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return Result{Status: 0}
	}
	defer resp.Body.Close()

	// A 400 may carry a per-entry report, which says the rest of the
	// batch was stored. Anything unreadable falls back to total loss.
	if resp.StatusCode == http.StatusBadRequest {
		var report struct {
			Rejected []struct{} `json:"rejected"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&report); err == nil &&
			report.Rejected != nil {
			return Result{Status: 400, Rejected: len(report.Rejected), Reported: true}
		}
	}
	return Result{Status: resp.StatusCode}
}
