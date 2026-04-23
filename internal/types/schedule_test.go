package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScheduleID_Validate(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		var id ScheduleID
		err := id.Validate()
		require.Error(t, err)
	})

	t.Run("known id", func(t *testing.T) {
		t.Parallel()
		for _, reg := range AllScheduleRegistrations() {
			err := reg.ID.Validate()
			require.NoError(t, err, "id=%q", reg.ID)
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		t.Parallel()
		err := ScheduleID("not-a-registered-cron").Validate()
		require.Error(t, err)
	})
}

func TestAllTemporalServerScheduleIDs_matches_registrations(t *testing.T) {
	t.Parallel()
	ids := AllTemporalServerScheduleIDs()
	reg := AllScheduleRegistrations()
	require.Equal(t, len(reg), len(ids), "AllTemporalServerScheduleIDs and AllScheduleRegistrations must stay aligned")
	seen := make(map[ScheduleID]struct{}, len(reg))
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	for _, r := range reg {
		_, ok := seen[r.ID]
		require.True(t, ok, "registration %q missing from AllTemporalServerScheduleIDs", r.ID)
	}
}
