package tenant

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"zolo-test-integration/internal/app/erp"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
	"zolo-test-integration/pkg/money"
)

// AlphaMaxLinesPerRequest is ERP A's undocumented but enforced limit: a third line
// item in one call comes back as HTTP 429.
const AlphaMaxLinesPerRequest = 2

const alphaPath = "/api/v1/sap-adapter/orders"

// Alpha maps ZOLO orders onto the SAP adapter.
type Alpha struct {
	baseURL string
}

func NewAlpha(baseURL string) *Alpha {
	return &Alpha{baseURL: baseURL}
}

func (a *Alpha) TenantID() string { return pkg.TenantAlpha }

// AlphaPayload is ERP A's wire format, prices in integer cents.
type AlphaPayload struct {
	OrderID    string          `json:"Order_ID"`
	CustomerID string          `json:"Customer_ID"`
	Currency   string          `json:"Currency"`
	ChunkIndex int             `json:"Chunk_Index"`
	ChunkTotal int             `json:"Chunk_Total"`
	LineItems  []AlphaLineItem `json:"Line_Items"`
}

type AlphaLineItem struct {
	SKU            string       `json:"SKU"`
	Qty            int64        `json:"Qty"`
	UnitPriceCents money.Amount `json:"Unit_Price_Cents"`
	DiscountBPS    int64        `json:"Discount_Bps"`
	LineTotalCents money.Amount `json:"Line_Total_Cents"`
}

func (a *Alpha) PreValidate(order model.Order, _ time.Time) *pkg.AppError {
	if len(order.Items) == 0 {
		return pkg.NewValidationError(pkg.MsgNoLineItems, nil).
			WithParam("order_id", order.OrderID)
	}
	if alphaCustomerID(order) == "" {
		return pkg.NewValidationError(pkg.MsgInvalidPartnerRef, nil).
			WithParam("order_id", order.OrderID)
	}
	return nil
}

func (a *Alpha) Build(order model.Order) ([]erp.Request, *pkg.AppError) {
	chunks := chunkItems(order.Items, AlphaMaxLinesPerRequest)
	key := model.IdempotencyKey(order.OrderID, order.TenantID)

	requests := make([]erp.Request, 0, len(chunks))
	for i, chunk := range chunks {
		lines := make([]AlphaLineItem, 0, len(chunk))
		skus := make([]string, 0, len(chunk))

		for _, item := range chunk {
			lines = append(lines, AlphaLineItem{
				SKU:            item.SKU,
				Qty:            item.Qty,
				UnitPriceCents: item.UnitPriceCents,
				DiscountBPS:    item.DiscountBPS,
				LineTotalCents: item.LineTotal(),
			})
			skus = append(skus, item.SKU)
		}

		requests = append(requests, erp.Request{
			Method:  http.MethodPost,
			BaseURL: a.baseURL,
			Path:    alphaPath,
			Headers: map[string]string{
				pkg.HeaderIdempotencyKey: key,
				// Every chunk carries the same key, so the chunk index disambiguates them.
				// Without it ERP A dedupes the whole order down to its first chunk.
				pkg.HeaderChunkIndex: fmt.Sprintf("%d", i),
			},
			Body: AlphaPayload{
				OrderID:    order.OrderID,
				CustomerID: alphaCustomerID(order),
				Currency:   order.Currency,
				ChunkIndex: i,
				ChunkTotal: len(chunks),
				LineItems:  lines,
			},
			ChunkIndex: i,
			SKUs:       skus,
		})
	}

	return requests, nil
}

// Interpret reports a half-accepted chunked order as partial, because the earlier
// chunks are already in ERP A and re-sending them would duplicate the lines.
func (a *Alpha) Interpret(order model.Order, responses []erp.Response) Outcome {
	var (
		lines    []model.SyncLineResult
		anyOK    bool
		anyFail  bool
		firstErr string
	)

	for _, resp := range responses {
		reason := alphaFailureReason(resp)
		accepted := reason == ""

		for _, sku := range resp.Request.SKUs {
			line := model.SyncLineResult{SKU: sku, Accepted: accepted}
			if !accepted {
				r := reason
				line.Reason = &r
			}
			lines = append(lines, line)
		}

		if accepted {
			anyOK = true
			continue
		}
		anyFail = true
		if firstErr == "" {
			firstErr = reason
		}
	}

	switch {
	case !anyFail:
		return Outcome{
			Status:      pkg.StatusSynced,
			MessageCode: pkg.MsgSyncSuccess,
			Params:      map[string]string{"order_id": order.OrderID},
			Lines:       lines,
		}
	case anyOK:
		return Outcome{
			Status:      pkg.StatusPartialSuccess,
			ErrorCode:   pkg.CodeERPRejected,
			MessageCode: pkg.MsgSyncPartial,
			Params: map[string]string{
				"order_id": order.OrderID,
				"skus":     strings.Join(model.SyncAttempt{Lines: lines}.RejectedSKUs(), ", "),
			},
			Lines: lines,
		}
	default:
		return Outcome{
			Status:      pkg.StatusFailed,
			ErrorCode:   pkg.CodeERPRejected,
			MessageCode: pkg.MsgSyncFailed,
			Params: map[string]string{
				"order_id": order.OrderID,
				"reason":   firstErr,
			},
			Lines: lines,
		}
	}
}

// alphaCustomerID prefers external_ref, falling back to the phone with "+" and
// spaces stripped, per the ERP A mapping rules.
func alphaCustomerID(order model.Order) string {
	if ref := strings.TrimSpace(order.ExternalRef()); ref != "" {
		return ref
	}

	phone := order.Phone()
	phone = strings.ReplaceAll(phone, "+", "")
	phone = strings.ReplaceAll(phone, " ", "")
	return phone
}

func alphaFailureReason(resp erp.Response) string {
	if resp.TransportErr != nil {
		return resp.TransportErr.Error()
	}
	if resp.OK() {
		return ""
	}

	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = resp.Decode(&body)

	switch {
	case body.Message != "":
		return body.Message
	case body.Error != "":
		return body.Error
	default:
		return fmt.Sprintf("ERP A returned HTTP %d", resp.StatusCode)
	}
}

func chunkItems(items []model.OrderItem, size int) [][]model.OrderItem {
	var chunks [][]model.OrderItem
	for start := 0; start < len(items); start += size {
		chunks = append(chunks, items[start:min(start+size, len(items))])
	}
	return chunks
}
