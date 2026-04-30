package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestWebhookRetryJob_Defaults(t *testing.T) {
	yaml := `
webhook_retry_job:
  enabled: true
  max_attempts: 5
  rate_limit: 5
  excluded_tenants: []
  allowed_event_types: []
`
	v := viper.New()
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadConfig(strings.NewReader(yaml)))

	var cfg Configuration
	require.NoError(t, v.Unmarshal(&cfg))

	j := cfg.WebhookRetryJob
	require.True(t, j.Enabled)
	require.Equal(t, 5, j.MaxAttempts)
	require.Equal(t, 5, j.RateLimit)
	require.Empty(t, j.ExcludedTenants)
	require.Empty(t, j.AllowedEventTypes)
}

func TestWebhookRetryJob_ExcludedTenants(t *testing.T) {
	yaml := `
webhook_retry_job:
  enabled: true
  max_attempts: 3
  rate_limit: 10
  excluded_tenants:
    - "ten_skip_1"
    - "ten_skip_2"
  allowed_event_types:
    - "invoice.finalized"
`
	v := viper.New()
	v.SetConfigType("yaml")
	require.NoError(t, v.ReadConfig(strings.NewReader(yaml)))

	var cfg Configuration
	require.NoError(t, v.Unmarshal(&cfg))

	j := cfg.WebhookRetryJob
	require.Equal(t, 3, j.MaxAttempts)
	require.Equal(t, 10, j.RateLimit)
	require.Equal(t, []string{"ten_skip_1", "ten_skip_2"}, j.ExcludedTenants)
	require.Equal(t, []string{"invoice.finalized"}, j.AllowedEventTypes)
}
