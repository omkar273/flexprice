package models

// ===================== Refund Recovery =====================

// RefundRecoveryWorkflowInput is the input for RefundRecoveryWorkflow.
// No fields required — activities sweep all tenants themselves.
type RefundRecoveryWorkflowInput struct{}

// RefundRecoveryWorkflowResult is the combined result of both recovery activities.
type RefundRecoveryWorkflowResult struct {
	SurfaceStaleResult  *SurfaceStaleRefundsResult          `json:"surface_stale"`
	RetryWebhooksResult *RetryUnprocessedWebhookEventsResult `json:"retry_webhooks"`
}

// SurfaceStaleRefundsResult captures alerting metrics for stale PENDING refunds.
type SurfaceStaleRefundsResult struct {
	Total   int `json:"total"`
	Alerted int `json:"alerted"`
	Errors  int `json:"errors"`
}

// RetryUnprocessedWebhookEventsResult captures retry metrics for unprocessed webhook events.
type RetryUnprocessedWebhookEventsResult struct {
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Errors    int `json:"errors"`
}
