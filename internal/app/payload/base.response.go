package payload

type BaseResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Error   any    `json:"error,omitempty"`
}

type ErrorDetail struct {
	Code    string   `json:"code"`
	Details []string `json:"details,omitempty"`
}
