package erp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"zolo-test-integration/config"
)

type Client struct {
	http   *http.Client
	cfg    config.ERP
	logger *slog.Logger
}

func NewClient(cfg config.ERP, logger *slog.Logger) *Client {
	return &Client{
		http:   &http.Client{Timeout: cfg.Timeout},
		cfg:    cfg,
		logger: logger,
	}
}

// Dispatch returns one Response per Request, stopping at the first transport failure so later
// chunks never land for an order whose earlier chunks did not.
func (c *Client) Dispatch(ctx context.Context, requests []Request) []Response {
	responses := make([]Response, 0, len(requests))

	for _, req := range requests {
		resp := c.send(ctx, req)
		responses = append(responses, resp)

		if resp.TransportErr != nil {
			break
		}
	}

	return responses
}

// send retries only 429 and 5xx: a 4xx means the payload is wrong, so repeating it wastes the wait.
func (c *Client) send(ctx context.Context, req Request) Response {
	body, err := json.Marshal(req.Body)
	if err != nil {
		return Response{Request: req, TransportErr: fmt.Errorf("encode request: %w", err)}
	}

	var last Response

	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		last = c.attempt(ctx, req, body)
		last.Attempts = attempt

		if !retryable(last) {
			return last
		}

		if attempt == c.cfg.MaxAttempts {
			break
		}

		wait := c.backoff(attempt, last)
		c.logger.Warn("erp call failed, retrying",
			"path", req.Path,
			"chunk", req.ChunkIndex,
			"status", last.StatusCode,
			"attempt", attempt,
			"retry_in", wait.String(),
		)

		select {
		case <-ctx.Done():
			last.TransportErr = ctx.Err()
			return last
		case <-time.After(wait):
		}
	}

	if last.TransportErr == nil {
		last.TransportErr = fmt.Errorf("erp returned %d after %d attempts", last.StatusCode, last.Attempts)
	}
	return last
}

func (c *Client) attempt(ctx context.Context, req Request, body []byte) Response {
	method := req.Method
	if method == "" {
		method = http.MethodPost
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, req.BaseURL+req.Path, bytes.NewReader(body))
	if err != nil {
		return Response{Request: req, TransportErr: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return Response{Request: req, TransportErr: err}
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{Request: req, StatusCode: httpResp.StatusCode, TransportErr: err}
	}

	return Response{
		Request:    req,
		StatusCode: httpResp.StatusCode,
		Body:       raw,
		retryAfter: parseRetryAfter(httpResp.Header.Get("Retry-After")),
	}
}

func retryable(r Response) bool {
	if r.TransportErr != nil {
		return true
	}
	return r.StatusCode == http.StatusTooManyRequests || r.StatusCode >= 500
}

// backoff is exponential with jitter, so a batch sync does not re-collide on the rate limit in lockstep.
func (c *Client) backoff(attempt int, resp Response) time.Duration {
	if resp.retryAfter > 0 {
		return min(resp.retryAfter, c.cfg.BackoffMax)
	}

	delay := c.cfg.BackoffBase << (attempt - 1)
	delay = min(delay, c.cfg.BackoffMax)

	return time.Duration(rand.Int64N(int64(delay)) + int64(delay)/2)
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return 0
}
