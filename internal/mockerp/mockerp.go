package mockerp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OutOfStockSKUMarker drives ERP B's partial failure off the SKU, so an order containing
// SKU-OUTOFSTOCK comes back 207 deterministically.
const OutOfStockSKUMarker = "OUTOFSTOCK"

// AlphaMaxLineItems mirrors ERP A's per-request rate limit.
const AlphaMaxLineItems = 2

// BetaMaxOrderAge mirrors ERP B's confirmation freshness window.
const BetaMaxOrderAge = 24 * time.Hour

type Server struct {
	logger *slog.Logger

	mu sync.Mutex
	// seen is keyed by "key:chunk" because a chunked order legitimately sends the same
	// order-level idempotency key several times.
	seen map[string]alphaReceipt
	// calls counts requests per path so a test can assert a retry or double-click did or
	// did not reach the ERP.
	calls map[string]int
}

type alphaReceipt struct {
	ERPOrderID string `json:"erp_order_id"`
	Duplicate  bool   `json:"duplicate"`
}

func NewServer(logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Server{
		logger: logger,
		seen:   map[string]alphaReceipt{},
		calls:  map[string]int{},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+alphaPath, s.handleAlpha)
	mux.HandleFunc("POST "+betaPath, s.handleBeta)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

// Calls reports how many requests a path has received.
func (s *Server) Calls(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[path]
}

func (s *Server) record(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[path]++
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func containsOutOfStock(sku string) bool {
	return strings.Contains(strings.ToUpper(sku), OutOfStockSKUMarker)
}
