package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// TestAllTemporalScheduleConfigsMatchServerScheduleIDs keeps schedule config ids aligned
// with types.AllTemporalServerScheduleIDs (used for schedule_id validation).
func TestAllTemporalScheduleConfigsMatchServerScheduleIDs(t *testing.T) {
	t.Parallel()
	configs := AllTemporalScheduleConfigs()
	ids := types.AllTemporalServerScheduleIDs()
	require.Equal(t, len(ids), len(configs), "each managed schedule must have a config entry")

	seen := make(map[types.ScheduleID]struct{}, len(ids))
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	for _, cfg := range configs {
		_, ok := seen[cfg.ID]
		require.True(t, ok, "config id %q not in AllTemporalServerScheduleIDs", cfg.ID)
	}
}
