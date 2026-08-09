package mockerp

import (
	"encoding/json"
	"net/http"
)

const betaPath = "/api/v2/odoo-adapter/sales-order"

type betaRequest struct {
	OrderID    string `json:"order_ref"`
	PartnerID  int64  `json:"partner_id"`
	OrderLines []struct {
		SKU           string `json:"product_code"`
		Qty           int64  `json:"product_uom_qty"`
		SubtotalCents int64  `json:"price_subtotal"`
	} `json:"order_lines"`
	AmountUntaxed int64 `json:"amount_untaxed"`
	AmountTax     int64 `json:"amount_tax"`
	AmountTotal   int64 `json:"amount_total"`
}

type betaLineResult struct {
	SKU    string `json:"product_code"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type betaResponse struct {
	ERPOrderID string           `json:"erp_order_id,omitempty"`
	Error      string           `json:"error,omitempty"`
	Lines      []betaLineResult `json:"line_results,omitempty"`
}

// handleBeta simulates the Odoo adapter. It answers 207 when some lines are accepted and
// others are out of stock, the case the integration must not flatten into a total failure.
func (s *Server) handleBeta(w http.ResponseWriter, r *http.Request) {
	s.record(betaPath)

	var req betaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, betaResponse{Error: "MALFORMED_PAYLOAD"})
		return
	}

	if req.PartnerID <= 0 {
		writeJSON(w, http.StatusBadRequest, betaResponse{Error: "INVALID_PARTNER"})
		return
	}

	if len(req.OrderLines) == 0 {
		writeJSON(w, http.StatusBadRequest, betaResponse{Error: "EMPTY_ORDER"})
		return
	}

	lines := make([]betaLineResult, 0, len(req.OrderLines))
	rejected := 0
	for _, line := range req.OrderLines {
		if containsOutOfStock(line.SKU) {
			rejected++
			lines = append(lines, betaLineResult{
				SKU:    line.SKU,
				Status: "failed",
				Reason: "out of stock",
			})
			continue
		}
		lines = append(lines, betaLineResult{SKU: line.SKU, Status: "success"})
	}

	if rejected > 0 {
		s.logger.Info("erp b partial acceptance", "order_ref", req.OrderID, "rejected", rejected)
		writeJSON(w, http.StatusMultiStatus, betaResponse{
			ERPOrderID: "ODOO-" + req.OrderID,
			Lines:      lines,
		})
		return
	}

	writeJSON(w, http.StatusOK, betaResponse{
		ERPOrderID: "ODOO-" + req.OrderID,
		Lines:      lines,
	})
}
