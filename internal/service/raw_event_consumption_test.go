package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	domainSettings "github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

// ---------------------------------------------------------------------------
// Suite setup
// ---------------------------------------------------------------------------

type RawEventConsumptionSuite struct {
	testutil.BaseServiceTestSuite
	svc         *rawEventConsumptionService
	outputPubSub *testutil.InMemoryPubSub
	settingsRepo *testutil.InMemorySettingsStore
}

func TestRawEventConsumptionService(t *testing.T) {
	suite.Run(t, new(RawEventConsumptionSuite))
}

func (s *RawEventConsumptionSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.outputPubSub = testutil.NewInMemoryPubSub()
	s.settingsRepo = s.GetStores().SettingsRepo.(*testutil.InMemorySettingsStore)

	params := ServiceParams{
		Logger:       s.GetLogger(),
		Config:       s.GetConfig(),
		DB:           s.GetDB(),
		SettingsRepo: s.settingsRepo,
	}

	s.svc = &rawEventConsumptionService{
		ServiceParams: params,
		outputPubSub:  s.outputPubSub,
		sentryService: sentry.NewSentryService(s.GetConfig(), s.GetLogger()),
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	testTenantID      = types.DefaultTenantID
	testEnvironmentID = "env_sandbox"
	testOutputTopic   = "events"
)

// validBentoPayload returns a minimal valid Bento event JSON for the given orgID.
func validBentoPayload(orgID, eventID string) string {
	return `{
		"orgId":"` + orgID + `",
		"id":"` + eventID + `",
		"methodName":"CHAT_COMPLETION",
		"providerName":"openai",
		"createdAt":"2024-01-15T10:00:00Z"
	}`
}

// buildBatchMsg serialises a RawEventBatch into a Watermill message.
func buildBatchMsg(batch RawEventBatch) *message.Message {
	payload, _ := json.Marshal(batch)
	return message.NewMessage("test-uuid", payload)
}

// makeFilterSetting stores an EventIngestionFilterConfig in the in-memory settings
// repo under the test tenant+environment.
func (s *RawEventConsumptionSuite) makeFilterSetting(enabled bool, allowedIDs []string) {
	value, _ := json.Marshal(types.EventIngestionFilterConfig{
		Enabled:                    enabled,
		AllowedExternalCustomerIDs: allowedIDs,
	})
	var valueMap map[string]interface{}
	_ = json.Unmarshal(value, &valueMap)

	setting := &domainSettings.Setting{
		ID:            types.GenerateUUID(),
		Key:           types.SettingKeyEventIngestionFilter,
		Value:         valueMap,
		EnvironmentID: testEnvironmentID,
	}
	setting.TenantID = testTenantID
	setting.Status = types.StatusPublished
	setting.CreatedAt = time.Now()
	setting.UpdatedAt = time.Now()

	ctx := testutil.SetupContext()
	_ = s.settingsRepo.Create(ctx, setting)
}

// publishedCount returns how many messages landed on the output topic.
func (s *RawEventConsumptionSuite) publishedCount() int {
	return len(s.outputPubSub.GetMessages(testOutputTopic))
}

// configure the output topic on the embedded config
func (s *RawEventConsumptionSuite) setOutputTopic(topic string) {
	s.svc.Config.RawEventConsumption.OutputTopic = topic
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestFilterDisabled_AllEventsForwarded — setting absent, all events pass through.
func (s *RawEventConsumptionSuite) TestFilterDisabled_AllEventsForwarded() {
	s.setOutputTopic(testOutputTopic)

	batch := RawEventBatch{
		TenantID:      testTenantID,
		EnvironmentID: testEnvironmentID,
		Data: []json.RawMessage{
			json.RawMessage(validBentoPayload("org_001", "evt_001")),
			json.RawMessage(validBentoPayload("org_002", "evt_002")),
			json.RawMessage(validBentoPayload("org_003", "evt_003")),
		},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.NoError(err)
	s.Equal(3, s.publishedCount(), "all 3 events should be forwarded when filter is absent")
}

// TestFilterEnabled_AllowlistedIDsForwarded — only IDs in the allowlist pass.
func (s *RawEventConsumptionSuite) TestFilterEnabled_AllowlistedIDsForwarded() {
	s.setOutputTopic(testOutputTopic)
	s.makeFilterSetting(true, []string{"org_001", "org_002"})

	batch := RawEventBatch{
		TenantID:      testTenantID,
		EnvironmentID: testEnvironmentID,
		Data: []json.RawMessage{
			json.RawMessage(validBentoPayload("org_001", "evt_001")), // allowed
			json.RawMessage(validBentoPayload("org_002", "evt_002")), // allowed
			json.RawMessage(validBentoPayload("org_003", "evt_003")), // filtered out
			json.RawMessage(validBentoPayload("org_004", "evt_004")), // filtered out
		},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.NoError(err)
	s.Equal(2, s.publishedCount(), "only org_001 and org_002 should be forwarded")
}

// TestFilterEnabled_NoAllowlistedIDs — filter is on but the batch has no matching IDs.
func (s *RawEventConsumptionSuite) TestFilterEnabled_NoAllowlistedIDs() {
	s.setOutputTopic(testOutputTopic)
	s.makeFilterSetting(true, []string{"org_allowed"})

	batch := RawEventBatch{
		TenantID:      testTenantID,
		EnvironmentID: testEnvironmentID,
		Data: []json.RawMessage{
			json.RawMessage(validBentoPayload("org_not_in_list", "evt_001")),
			json.RawMessage(validBentoPayload("org_also_not",    "evt_002")),
		},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.NoError(err)
	s.Equal(0, s.publishedCount(), "no events should be forwarded when no IDs match")
}

// TestFilterEnabledFalse_AllEventsForwarded — setting exists but enabled=false, all pass.
func (s *RawEventConsumptionSuite) TestFilterEnabledFalse_AllEventsForwarded() {
	s.setOutputTopic(testOutputTopic)
	s.makeFilterSetting(false, []string{"org_001"}) // disabled

	batch := RawEventBatch{
		TenantID:      testTenantID,
		EnvironmentID: testEnvironmentID,
		Data: []json.RawMessage{
			json.RawMessage(validBentoPayload("org_001", "evt_001")),
			json.RawMessage(validBentoPayload("org_999", "evt_002")), // would be blocked if enabled
		},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.NoError(err)
	s.Equal(2, s.publishedCount(), "all events should pass when filter is disabled")
}

// TestFilterEnabled_EmptyAllowlist — filter on with empty list blocks everything.
func (s *RawEventConsumptionSuite) TestFilterEnabled_EmptyAllowlist() {
	s.setOutputTopic(testOutputTopic)
	s.makeFilterSetting(true, []string{}) // enabled but empty

	batch := RawEventBatch{
		TenantID:      testTenantID,
		EnvironmentID: testEnvironmentID,
		Data: []json.RawMessage{
			json.RawMessage(validBentoPayload("org_001", "evt_001")),
		},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.NoError(err)
	s.Equal(0, s.publishedCount(), "empty allowlist should block all events")
}

// TestInvalidEvent_Skipped — events that fail transformer validation are counted as skips,
// not errors, regardless of filter state.
func (s *RawEventConsumptionSuite) TestInvalidEvent_Skipped() {
	s.setOutputTopic(testOutputTopic)
	s.makeFilterSetting(true, []string{"org_001"})

	invalidPayload := `{"orgId":"org_001"}` // missing required fields (methodName, providerName, id, createdAt)

	batch := RawEventBatch{
		TenantID:      testTenantID,
		EnvironmentID: testEnvironmentID,
		Data: []json.RawMessage{
			json.RawMessage(invalidPayload),
			json.RawMessage(validBentoPayload("org_001", "evt_002")),
		},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.NoError(err)
	// Invalid event is dropped before filter check; valid+allowed event goes through
	s.Equal(1, s.publishedCount())
}

// TestMalformedBatchPayload_ReturnsError — a non-JSON batch returns a non-retriable error.
func (s *RawEventConsumptionSuite) TestMalformedBatchPayload_ReturnsError() {
	// sentryService is nil, but we guard against panics by catching the error path
	msg := message.NewMessage("test-uuid", []byte("not-json"))
	err := s.svc.processMessage(msg)
	s.Error(err)
	s.Contains(err.Error(), "non-retriable unmarshal error")
}

// TestTenantFallbackFromConfig — when batch has no tenant/env, config values are used
// and the filter setting stored under config tenant/env is picked up correctly.
func (s *RawEventConsumptionSuite) TestTenantFallbackFromConfig() {
	s.setOutputTopic(testOutputTopic)

	// Config tenant/env is what SetupContext uses (DefaultTenantID / env_sandbox)
	s.GetConfig().Billing.TenantID = testTenantID
	s.GetConfig().Billing.EnvironmentID = testEnvironmentID
	s.makeFilterSetting(true, []string{"org_001"})

	batch := RawEventBatch{
		// TenantID and EnvironmentID intentionally omitted → falls back to config
		Data: []json.RawMessage{
			json.RawMessage(validBentoPayload("org_001", "evt_001")), // allowed
			json.RawMessage(validBentoPayload("org_002", "evt_002")), // filtered
		},
	}

	err := s.svc.processMessage(buildBatchMsg(batch))
	s.NoError(err)
	s.Equal(1, s.publishedCount())
}
