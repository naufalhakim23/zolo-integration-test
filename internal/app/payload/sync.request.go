package payload

// BatchSyncRequest request body for POST /api/v1/orders/batch-sync
type BatchSyncRequest struct {
	OrderIDs []string `json:"order_ids" validate:"required,min=1,max=100,dive,required"`
}
