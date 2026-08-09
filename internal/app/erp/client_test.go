package erp_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"zolo-test-integration/config"
	"zolo-test-integration/internal/app/erp"
)

func testConfig(baseURL string) config.ERP {
	return config.ERP{
		AlphaBaseURL: baseURL,
		BetaBaseURL:  baseURL,
		Timeout:      2 * time.Second,
		MaxAttempts:  4,
		BackoffBase:  time.Millisecond,
		BackoffMax:   5 * time.Millisecond,
	}
}

func newClient(baseURL string) *erp.Client {
	return erp.NewClient(testConfig(baseURL), slog.New(slog.DiscardHandler))
}

// failFirst answers status for the first n calls, then 201.
func failFirst(n int32, status int, calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= n {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"erp_order_id":"SAP-1"}`))
	}
}

func always(status int, body string, calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestClientDispatch(t *testing.T) {
	cases := []struct {
		name    string
		handler func(*atomic.Int32) http.HandlerFunc
		chunks  int

		wantCalls        int32
		wantResponses    int
		wantAttempts     int
		wantStatus       int
		wantOK           bool
		wantTransportErr bool
	}{
		{
			// A 429 is a slow-down, not a rejection: retry instead of failing the order taker.
			name:          "retries a 429 until it succeeds",
			handler:       func(c *atomic.Int32) http.HandlerFunc { return failFirst(2, http.StatusTooManyRequests, c) },
			chunks:        1,
			wantCalls:     3,
			wantResponses: 1,
			wantAttempts:  3,
			wantStatus:    http.StatusCreated,
			wantOK:        true,
		},
		{
			// Retrying a 4xx just makes the user wait longer for the same answer.
			name: "does not retry a client error",
			handler: func(c *atomic.Int32) http.HandlerFunc {
				return always(http.StatusBadRequest, `{"error":"ORDER_EXPIRED"}`, c)
			},
			chunks:        1,
			wantCalls:     1,
			wantResponses: 1,
			wantAttempts:  1,
			wantStatus:    http.StatusBadRequest,
		},
		{
			// 207 is an answer the mapper interprets, so transport passes it through untouched.
			name: "passes 207 through without calling it a success",
			handler: func(c *atomic.Int32) http.HandlerFunc {
				return always(http.StatusMultiStatus, `{"line_results":[]}`, c)
			},
			chunks:        1,
			wantCalls:     1,
			wantResponses: 1,
			wantAttempts:  1,
			wantStatus:    http.StatusMultiStatus,
		},
		{
			name:             "gives up after MaxAttempts",
			handler:          func(c *atomic.Int32) http.HandlerFunc { return always(http.StatusTooManyRequests, "", c) },
			chunks:           1,
			wantCalls:        4,
			wantResponses:    1,
			wantAttempts:     4,
			wantStatus:       http.StatusTooManyRequests,
			wantTransportErr: true,
		},
		{
			// Later chunks must not be pushed when an earlier one never landed.
			name:             "stops dispatching after a transport failure",
			handler:          func(c *atomic.Int32) http.HandlerFunc { return always(http.StatusServiceUnavailable, "", c) },
			chunks:           3,
			wantCalls:        4,
			wantResponses:    1,
			wantAttempts:     4,
			wantStatus:       http.StatusServiceUnavailable,
			wantTransportErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32

			srv := httptest.NewServer(c.handler(&calls))
			defer srv.Close()

			requests := make([]erp.Request, 0, c.chunks)
			for i := range c.chunks {
				requests = append(requests, erp.Request{BaseURL: srv.URL, Path: "/orders", ChunkIndex: i})
			}

			responses := newClient(srv.URL).Dispatch(context.Background(), requests)

			if len(responses) != c.wantResponses {
				t.Fatalf("responses = %d, want %d", len(responses), c.wantResponses)
			}
			if got := calls.Load(); got != c.wantCalls {
				t.Fatalf("erp calls = %d, want %d", got, c.wantCalls)
			}

			got := responses[0]
			if got.Attempts != c.wantAttempts {
				t.Errorf("attempts = %d, want %d", got.Attempts, c.wantAttempts)
			}
			if got.StatusCode != c.wantStatus {
				t.Errorf("status = %d, want %d", got.StatusCode, c.wantStatus)
			}
			if got.OK() != c.wantOK {
				t.Errorf("OK() = %v, want %v", got.OK(), c.wantOK)
			}
			if (got.TransportErr != nil) != c.wantTransportErr {
				t.Errorf("transport error = %v, want error: %v", got.TransportErr, c.wantTransportErr)
			}
			if got.IsMultiStatus() != (c.wantStatus == http.StatusMultiStatus) {
				t.Errorf("IsMultiStatus() = %v for status %d", got.IsMultiStatus(), got.StatusCode)
			}
		})
	}
}
