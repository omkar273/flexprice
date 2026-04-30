package webhook

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	flexent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	repoent "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSystemEventToWebhookEvent(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	se := &flexent.SystemEvent{
		ID:            "sev_test123",
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		EventName:     types.WebhookEventCustomerCreated,
		EntityType:    string(types.SystemEntityTypeCustomer),
		EntityID:      "cus_1",
		CreatedBy:     "user_1",
		CreatedAt:     created,
		Payload:       map[string]interface{}{"customer_id": "cus_1", "tenant_id": "ten_1"},
	}

	ev, err := SystemEventToWebhookEvent(se)
	require.NoError(t, err)
	require.Equal(t, se.ID, ev.ID)
	require.Equal(t, se.EventName, ev.EventName)
	require.Equal(t, se.TenantID, ev.TenantID)
	require.Equal(t, se.EnvironmentID, ev.EnvironmentID)
	require.Equal(t, se.CreatedBy, ev.UserID)
	require.True(t, ev.Timestamp.Equal(created.UTC()))
	require.Equal(t, types.SystemEntityTypeCustomer, ev.EntityType)
	require.Equal(t, se.EntityID, ev.EntityID)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(ev.Payload, &got))
	require.Equal(t, "cus_1", got["customer_id"])
}

func TestSystemEventToWebhookEvent_NilPayload(t *testing.T) {
	t.Parallel()

	se := &flexent.SystemEvent{
		ID:            "sev_empty",
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		EventName:     types.WebhookEventCustomerUpdated,
		CreatedAt:     time.Now().UTC(),
	}

	ev, err := SystemEventToWebhookEvent(se)
	require.NoError(t, err)
	require.Empty(t, ev.Payload)
}

func TestSystemEventToWebhookEvent_NilRow(t *testing.T) {
	t.Parallel()

	_, err := SystemEventToWebhookEvent(nil)
	require.Error(t, err)
}

// stubSystemEventRepo is a minimal stub for SystemEventRepository used in service tests.
type stubSystemEventRepo struct {
	rows      []*flexent.SystemEvent
	failedIDs []string
}

func (s *stubSystemEventRepo) ListStaleUndeliveredWebhooks(_ context.Context, _ repoent.ListStaleUndeliveredWebhooksParams) ([]*flexent.SystemEvent, error) {
	return s.rows, nil
}

func (s *stubSystemEventRepo) GetByID(_ context.Context, _, _, _ string) (*flexent.SystemEvent, error) {
	return nil, nil
}

func (s *stubSystemEventRepo) OnConsumed(_ context.Context, _ *types.WebhookEvent) error { return nil }

func (s *stubSystemEventRepo) OnDelivered(_ context.Context, _ string, _ *string) error { return nil }

func (s *stubSystemEventRepo) OnFailed(_ context.Context, id, _ string) error {
	s.failedIDs = append(s.failedIDs, id)
	return nil
}

// makeEvent creates a minimal SystemEvent for testing.
func makeEvent(id, tenantID, eventName string) *flexent.SystemEvent {
	return &flexent.SystemEvent{
		ID:            id,
		TenantID:      tenantID,
		EnvironmentID: "env_1",
		EventName:     types.WebhookEventName(eventName),
		CreatedAt:     time.Now().UTC().Add(-30 * time.Minute),
	}
}

func TestRetryStalePendingWebhooks_KillSwitch(t *testing.T) {
	t.Parallel()

	svc := &WebhookService{
		config: &config.Configuration{
			Webhook:         config.Webhook{Enabled: true},
			WebhookRetryJob: config.WebhookRetryJobConfig{Enabled: false, MaxAttempts: 5, RateLimit: 100},
		},
		logger: logger.NewNoopLogger(),
		systemEventRepo: &stubSystemEventRepo{
			rows: []*flexent.SystemEvent{makeEvent("sev_1", "ten_1", "invoice.finalized")},
		},
	}

	res, err := svc.RetryStalePendingWebhooks(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Total, "kill switch: no events should be processed")
}

func TestRetryStalePendingWebhooks_ExcludedTenants(t *testing.T) {
	t.Parallel()

	stub := &stubSystemEventRepo{
		rows: []*flexent.SystemEvent{
			makeEvent("sev_skip", "ten_excluded", "invoice.finalized"),
			makeEvent("sev_ok", "ten_included", "invoice.finalized"),
		},
	}

	svc := &WebhookService{
		config: &config.Configuration{
			Webhook: config.Webhook{Enabled: true},
			WebhookRetryJob: config.WebhookRetryJobConfig{
				Enabled:         true,
				MaxAttempts:     5,
				RateLimit:       100,
				ExcludedTenants: []string{"ten_excluded"},
			},
		},
		logger:          logger.NewNoopLogger(),
		systemEventRepo: stub,
	}

	res, err := svc.RetryStalePendingWebhooks(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Total, "excluded tenant events should not count toward Total")
}

func TestRetryStalePendingWebhooks_AllowedEventTypes(t *testing.T) {
	t.Parallel()

	stub := &stubSystemEventRepo{
		rows: []*flexent.SystemEvent{
			makeEvent("sev_allowed", "ten_1", "invoice.finalized"),
			makeEvent("sev_blocked", "ten_1", "subscription.created"),
		},
	}

	svc := &WebhookService{
		config: &config.Configuration{
			Webhook: config.Webhook{Enabled: true},
			WebhookRetryJob: config.WebhookRetryJobConfig{
				Enabled:           true,
				MaxAttempts:       5,
				RateLimit:         100,
				AllowedEventTypes: []string{"invoice.finalized"},
			},
		},
		logger:          logger.NewNoopLogger(),
		systemEventRepo: stub,
	}

	res, err := svc.RetryStalePendingWebhooks(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.Total, "events not in AllowedEventTypes should be skipped")
}
