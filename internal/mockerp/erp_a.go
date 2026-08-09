package mockerp

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const alphaPath = "/api/v1/sap-adapter/orders"

type alphaRequest struct {
	OrderID    string `json:"Order_ID"`
	CustomerID string `json:"Customer_ID"`
	Currency   string `json:"Currency"`
	ChunkIndex int    `json:"Chunk_Index"`
	LineItems  []struct {
		SKU            string `json:"SKU"`
		Qty            int64  `json:"Qty"`
		UnitPriceCents int64  `json:"Unit_Price_Cents"`
		LineTotalCents int64  `json:"Line_Total_Cents"`
	} `json:"Line_Items"`
}

// handleAlpha simulates the SAP adapter: 429 above two line items, which forces the
// client to chunk, and dedupe on the idempotency key so a double-click books one order.
func (s *Server) handleAlpha(w http.ResponseWriter, r *http.Request) {
	s.record(alphaPath)

	var req alphaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "MALFORMED_PAYLOAD",
			"message": err.Error(),
		})
		return
	}

	if req.CustomerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "MISSING_CUSTOMER",
			"message": "Customer_ID is required",
		})
		return
	}

	if len(req.LineItems) > AlphaMaxLineItems {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error":   "RATE_LIMITED",
			"message": fmt.Sprintf("at most %d line items per request", AlphaMaxLineItems),
		})
		return
	}

	// The key is order-scoped by contract, so the chunk index distinguishes the
	// legitimate repeats of a chunked order from an actual duplicate submission.
	key := fmt.Sprintf("%s:%d", r.Header.Get("X-Idempotency-Key"), req.ChunkIndex)

	s.mu.Lock()
	receipt, replayed := s.seen[key]
	if !replayed {
		receipt = alphaReceipt{ERPOrderID: fmt.Sprintf("SAP-%s-%d", req.OrderID, req.ChunkIndex)}
		s.seen[key] = receipt
	}
	s.mu.Unlock()

	if replayed {
		s.logger.Info("erp a replayed duplicate", "order_id", req.OrderID, "chunk", req.ChunkIndex)
		receipt.Duplicate = true
	}

	writeJSON(w, http.StatusCreated, receipt)
}
