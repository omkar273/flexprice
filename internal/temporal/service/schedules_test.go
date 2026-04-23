package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// TestAllTemporalScheduleConfigsMatchRegistrations keeps schedule worker configs and
// the public catalog in sync (one source for EnsureSchedule and GET /v1/temporal/schedules).
func TestAllTemporalScheduleConfigsMatchRegistrations(t *testing.T) {
	t.Parallel()
	configs := AllTemporalScheduleConfigs()
	reg := types.AllScheduleRegistrations()
	require.Equal(t, len(reg), len(configs), "each registered schedule must have a config entry")

	byID := make(map[types.ScheduleID]types.ScheduleRegistration, len(reg))
	for _, r := range reg {
		byID[r.ID] = r
	}
	for _, cfg := range configs {
		_, ok := byID[cfg.ID]
		require.True(t, ok, "config id %q not in AllScheduleRegistrations", cfg.ID)
	}
}
