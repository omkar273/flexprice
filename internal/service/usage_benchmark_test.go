package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inMemoryBenchmarkRepo captures inserted records for assertion.
type inMemoryBenchmarkRepo struct {
	records []*events.UsageBenchmarkRecord
}

func (r *inMemoryBenchmarkRepo) Insert(_ context.Context, rec *events.UsageBenchmarkRecord) error {
	r.records = append(r.records, rec)
	return nil
}

func TestUsageBenchmarkService_ProcessMessage_InsertsRow(t *testing.T) {
	repo := &inMemoryBenchmarkRepo{}
	pubSub := testutil.NewInMemoryPubSub()

	svc := service.NewUsageBenchmarkServiceForTest(repo, pubSub)

	evt := &events.UsageBenchmarkEvent{
		SubscriptionID: "sub_test",
		StartTime:      time.Now().Add(-24 * time.Hour).UTC(),
		EndTime:        time.Now().UTC(),
		TenantID:       "ten_test",
		EnvironmentID:  "env_test",
	}
	payload, err := json.Marshal(evt)
	require.NoError(t, err)

	msg := message.NewMessage("msg-1", payload)
	msg.Metadata.Set("tenant_id", evt.TenantID)
	msg.Metadata.Set("environment_id", evt.EnvironmentID)

	err = svc.ProcessMessageForTest(msg)
	require.NoError(t, err)
	require.Len(t, repo.records, 1)

	rec := repo.records[0]
	assert.Equal(t, "sub_test", rec.SubscriptionID)
	assert.Equal(t, "ten_test", rec.TenantID)
	assert.Equal(t, "env_test", rec.EnvironmentID)
	// Both pipeline placeholders return zero since ServiceParams is empty; diff must be 0.
	assert.True(t, rec.Diff.IsZero(), "diff should be zero when both pipelines return zero")
}
