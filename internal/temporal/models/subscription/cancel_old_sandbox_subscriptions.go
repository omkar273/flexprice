package subscription

// CancelOldSandboxSubscriptionsWorkflowInput represents the input for the cancel old sandbox subscriptions workflow
type CancelOldSandboxSubscriptionsWorkflowInput struct {
	// Empty for now, can be extended if needed
}

// Validate validates the cancel old sandbox subscriptions workflow input
func (i *CancelOldSandboxSubscriptionsWorkflowInput) Validate() error {
	// No validation needed for empty input
	return nil
}

// CancelOldSandboxSubscriptionsWorkflowResult represents the result of the cancel old sandbox subscriptions workflow
type CancelOldSandboxSubscriptionsWorkflowResult struct {
	TotalCancelled int `json:"total_cancelled"`
}
