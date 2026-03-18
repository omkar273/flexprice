package webhook

// PaddleEventType represents Paddle webhook event types
type PaddleEventType string

const (
	// EventTransactionCompleted occurs when a transaction is completed
	EventTransactionCompleted PaddleEventType = "transaction.completed"
)
