package tenant

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/pkg/money"
)

// BetaSSTPercent is the static SST rate ERP B expects on the header total. Line items
// stay tax-exclusive.
const BetaSSTPercent = 8

const betaPath = "/api/v2/odoo-adapter/sales-order"

// partnerRefPattern pulls the trailing integer out of a reference like "CUST-882".
var partnerRefPattern = regexp.MustCompile(`^[A-Za-z]+-(\d+)$`)

// Beta maps ZOLO orders onto the Odoo adapter.
type Beta struct {
	baseURL string
}

func NewBeta(baseURL string) *Beta {
	return &Beta{baseURL: baseURL}
}

func (b *Beta) TenantID() string { return pkg.TenantBeta }

type BetaPayload struct {
	OrderID    string          `json:"order_ref"`
	PartnerID  int64           `json:"partner_id"`
	Currency   string          `json:"currency"`
	OrderLines []BetaOrderLine `json:"order_lines"`

	AmountUntaxed money.Amount `json:"amount_untaxed"`
	AmountTax     money.Amount `json:"amount_tax"` // charged once on the header, not per line
	AmountTotal   money.Amount `json:"amount_total"`
	TaxPercent    int64        `json:"tax_percent"`
}

type BetaOrderLine struct {
	SKU            string       `json:"product_code"`
	Qty            int64        `json:"product_uom_qty"`
	UnitPriceCents money.Amount `json:"price_unit"`
	DiscountBPS    int64        `json:"discount_bps"`
	SubtotalCents  money.Amount `json:"price_subtotal"`
}

func (b *Beta) PreValidate(order model.Order, now time.Time) *pkg.AppError {
	if len(order.Items) == 0 {
		return pkg.NewValidationError(pkg.MsgNoLineItems, nil).
			WithParam("order_id", order.OrderID)
	}

	// ERP B answers a stale order with 400 ORDER_EXPIRED. Catching it here saves the
	// round trip and keeps a doomed order from consuming an idempotency claim.
	if order.Age(now) > MaxOrderAge {
		return pkg.NewError(pkg.CodeOrderExpired, pkg.MsgOrderExpired, http.StatusUnprocessableEntity, nil).
			WithParam("order_id", order.OrderID)
	}

	if _, err := ParsePartnerID(order.ExternalRef()); err != nil {
		return pkg.NewValidationError(pkg.MsgInvalidPartnerRef, err).
			WithParam("external_ref", order.ExternalRef()).
			WithParam("order_id", order.OrderID)
	}

	return nil
}

// ParsePartnerID turns a ZOLO customer reference into ERP B's integer partner id:
// "CUST-882" becomes 882.
func ParsePartnerID(externalRef string) (int64, error) {
	ref := strings.TrimSpace(externalRef)
	if ref == "" {
		return 0, fmt.Errorf("external_ref is missing")
	}

	match := partnerRefPattern.FindStringSubmatch(ref)
	if match == nil {
		return 0, fmt.Errorf("external_ref %q does not match the PREFIX-NUMBER format", ref)
	}

	id, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("external_ref %q has an out-of-range partner id", ref)
	}
	if id <= 0 {
		return 0, fmt.Errorf("external_ref %q resolves to a non-positive partner id", ref)
	}

	return id, nil
}

func (b *Beta) Build(order model.Order) ([]erp.Request, *pkg.AppError) {
	partnerID, err := ParsePartnerID(order.ExternalRef())
	if err != nil {
		return nil, pkg.NewValidationError(pkg.MsgInvalidPartnerRef, err).
			WithParam("external_ref", order.ExternalRef())
	}

	lines := make([]BetaOrderLine, 0, len(order.Items))
	skus := make([]string, 0, len(order.Items))
	for _, item := range order.Items {
		lines = append(lines, BetaOrderLine{
			SKU:            item.SKU,
			Qty:            item.Qty,
			UnitPriceCents: item.UnitPriceCents,
			DiscountBPS:    item.DiscountBPS,
			SubtotalCents:  item.LineTotal(),
		})
		skus = append(skus, item.SKU)
	}

	untaxed := order.Subtotal()
	tax := untaxed.Percent(BetaSSTPercent)

	return []erp.Request{{
		Method:  http.MethodPost,
		BaseURL: b.baseURL,
		Path:    betaPath,
		Headers: map[string]string{
			pkg.HeaderIdempotencyKey: model.IdempotencyKey(order.OrderID, order.TenantID),
		},
		Body: BetaPayload{
			OrderID:       order.OrderID,
			PartnerID:     partnerID,
			Currency:      order.Currency,
			OrderLines:    lines,
			AmountUntaxed: untaxed,
			AmountTax:     tax,
			AmountTotal:   untaxed + tax,
			TaxPercent:    BetaSSTPercent,
		},
		SKUs: skus,
	}}, nil
}

// betaResponse is ERP B's reply. On 207 the per-line results decide which SKUs the
// dashboard shows as failed.
type betaResponse struct {
	Error string `json:"error"`
	Lines []struct {
		SKU    string `json:"product_code"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"line_results"`
}

func (b *Beta) Interpret(order model.Order, responses []erp.Response) Outcome {
	if len(responses) == 0 {
		return betaFailure(order, pkg.CodeERPUnavailable, "no response from ERP B")
	}
	resp := responses[0]

	if resp.TransportErr != nil {
		return Outcome{
			Status:      pkg.StatusFailed,
			ErrorCode:   pkg.CodeERPUnavailable,
			MessageCode: pkg.MsgERPUnavailable,
			Params:      map[string]string{"order_id": order.OrderID},
			Lines:       betaLines(order, false, resp.TransportErr.Error()),
		}
	}

	var body betaResponse
	_ = resp.Decode(&body)

	// Marking a 207 wholly FAILED would tell the order taker to re-send items the ERP
	// already accepted, so the per-line detail is kept.
	if resp.IsMultiStatus() {
		return betaPartial(order, body)
	}

	if resp.OK() {
		return Outcome{
			Status:      pkg.StatusSynced,
			MessageCode: pkg.MsgSyncSuccess,
			Params:      map[string]string{"order_id": order.OrderID},
			Lines:       betaLines(order, true, ""),
		}
	}

	reason := body.Error
	if reason == "" {
		reason = fmt.Sprintf("ERP B returned HTTP %d", resp.StatusCode)
	}

	// PreValidate normally catches expiry, but a slow request can cross the boundary
	// in flight, so the server-side code is mapped rather than flattened.
	errorCode := pkg.CodeERPRejected
	messageCode := pkg.MsgSyncFailed
	if strings.Contains(strings.ToUpper(body.Error), pkg.CodeOrderExpired) {
		errorCode = pkg.CodeOrderExpired
		messageCode = pkg.MsgOrderExpired
	}

	return Outcome{
		Status:      pkg.StatusFailed,
		ErrorCode:   errorCode,
		MessageCode: messageCode,
		Params: map[string]string{
			"order_id": order.OrderID,
			"reason":   reason,
		},
		Lines: betaLines(order, false, reason),
	}
}

func betaPartial(order model.Order, body betaResponse) Outcome {
	reasonBySKU := map[string]string{}
	acceptedBySKU := map[string]bool{}
	for _, line := range body.Lines {
		ok := strings.EqualFold(line.Status, "success") || strings.EqualFold(line.Status, "accepted")
		acceptedBySKU[line.SKU] = ok
		if !ok {
			reasonBySKU[line.SKU] = line.Reason
		}
	}

	var (
		lines    []model.SyncLineResult
		rejected []string
		accepted int
	)
	for _, item := range order.Items {
		ok := acceptedBySKU[item.SKU]
		line := model.SyncLineResult{SKU: item.SKU, Accepted: ok}
		if ok {
			accepted++
		} else {
			reason := reasonBySKU[item.SKU]
			if reason == "" {
				reason = "rejected by ERP B"
			}
			line.Reason = &reason
			rejected = append(rejected, item.SKU)
		}
		lines = append(lines, line)
	}

	// A 207 where nothing succeeded is a failure wearing a partial status code.
	if accepted == 0 {
		return Outcome{
			Status:      pkg.StatusFailed,
			ErrorCode:   pkg.CodeERPRejected,
			MessageCode: pkg.MsgSyncFailed,
			Params: map[string]string{
				"order_id": order.OrderID,
				"reason":   "every line item was rejected: " + strings.Join(rejected, ", "),
			},
			Lines: lines,
		}
	}

	return Outcome{
		Status:      pkg.StatusPartialSuccess,
		ErrorCode:   pkg.CodeERPRejected,
		MessageCode: pkg.MsgSyncPartial,
		Params: map[string]string{
			"order_id": order.OrderID,
			"skus":     strings.Join(rejected, ", "),
		},
		Lines: lines,
	}
}

func betaLines(order model.Order, accepted bool, reason string) []model.SyncLineResult {
	lines := make([]model.SyncLineResult, 0, len(order.Items))
	for _, item := range order.Items {
		line := model.SyncLineResult{SKU: item.SKU, Accepted: accepted}
		if !accepted && reason != "" {
			r := reason
			line.Reason = &r
		}
		lines = append(lines, line)
	}
	return lines
}

func betaFailure(order model.Order, code, reason string) Outcome {
	return Outcome{
		Status:      pkg.StatusFailed,
		ErrorCode:   code,
		MessageCode: pkg.MsgSyncFailed,
		Params: map[string]string{
			"order_id": order.OrderID,
			"reason":   reason,
		},
		Lines: betaLines(order, false, reason),
	}
}
