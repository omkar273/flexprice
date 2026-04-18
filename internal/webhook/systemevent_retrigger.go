package webhook

import (
	"encoding/json"

	flexent "github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// SystemEventToWebhookEvent maps a persisted system_events row to the payload used by webhook delivery.
func SystemEventToWebhookEvent(se *flexent.SystemEvent) (*types.WebhookEvent, error) {
	if se == nil {
		return nil, ierr.NewError("system event is nil").
			Mark(ierr.ErrValidation)
	}

	var payload json.RawMessage
	if se.Payload != nil {
		b, err := json.Marshal(se.Payload)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Stored system event payload could not be serialized").
				Mark(ierr.ErrInternal)
		}
		payload = b
	}

	return &types.WebhookEvent{
		ID:            se.ID,
		EventName:     se.EventName,
		TenantID:      se.TenantID,
		EnvironmentID: se.EnvironmentID,
		UserID:        se.CreatedBy,
		Timestamp:     se.CreatedAt.UTC(),
		Payload:       payload,
		EntityType:    types.SystemEntityType(se.EntityType),
		EntityID:      se.EntityID,
	}, nil
}
