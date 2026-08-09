// Package erp carries the transport contract between tenant adapters and the ERP backends,
// so adding a tenant never touches dispatch, retry or wire encoding.
package erp

import (
	"encoding/json"
	"net/http"
	"time"
)

// Request is one HTTP call an adapter wants made. Adapters return a slice because chunking
// is tenant-local: ERP A caps a request at two line items, ERP B takes the whole order.
type Request struct {
	Method  string
	BaseURL string
	Path    string
	Headers map[string]string
	Body    any

	// SKUs lets a failure be reported against real line items, not a chunk number.
	ChunkIndex int
	SKUs       []string
}

// Response is the outcome of one Request. A non-2xx status is not an error here: 207 and
// 400 ORDER_EXPIRED are answers the adapter interprets.
type Response struct {
	Request    Request
	StatusCode int
	Body       json.RawMessage
	Attempts   int

	// TransportErr is set when no HTTP response was obtained at all.
	TransportErr error

	// retryAfter carries the Retry-After header so backoff honours the ERP's own pacing.
	retryAfter time.Duration
}

// OK excludes 207 on purpose: a generic 2xx check would mark a partially refused order SYNCED.
func (r Response) OK() bool {
	return r.TransportErr == nil &&
		r.StatusCode >= 200 && r.StatusCode < 300 &&
		r.StatusCode != http.StatusMultiStatus
}

func (r Response) IsMultiStatus() bool {
	return r.StatusCode == http.StatusMultiStatus
}

func (r Response) Decode(v any) error {
	if len(r.Body) == 0 {
		return nil
	}
	return json.Unmarshal(r.Body, v)
}
