package dashboard

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTimeRange_Defaults(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	from, to, err := parseTimeRange(req, 1*time.Hour)

	require.NoError(t, err)
	now := time.Now().UTC()
	// "to" should be approximately now.
	assert.WithinDuration(t, now, to, 2*time.Second)
	// "from" should be approximately 1 hour ago.
	assert.WithinDuration(t, now.Add(-1*time.Hour), from, 2*time.Second)
}

func TestParseTimeRange_ExplicitValues(t *testing.T) {
	now := time.Now().UTC()
	fromStr := now.Add(-2 * time.Hour).Format(time.RFC3339)
	toStr := now.Add(-1 * time.Hour).Format(time.RFC3339)

	req := httptest.NewRequest("GET", "/?from="+fromStr+"&to="+toStr, nil)
	from, to, err := parseTimeRange(req, 1*time.Hour)

	require.NoError(t, err)
	assert.WithinDuration(t, now.Add(-2*time.Hour), from, 2*time.Second)
	assert.WithinDuration(t, now.Add(-1*time.Hour), to, 2*time.Second)
}

func TestParseTimeRange_InvalidFrom(t *testing.T) {
	req := httptest.NewRequest("GET", "/?from=not-a-date", nil)
	_, _, err := parseTimeRange(req, 1*time.Hour)
	assert.Error(t, err)
}

func TestParseTimeRange_InvalidTo(t *testing.T) {
	req := httptest.NewRequest("GET", "/?to=garbage", nil)
	_, _, err := parseTimeRange(req, 1*time.Hour)
	assert.Error(t, err)
}

func TestParseTimeRange_ToClampedToNow(t *testing.T) {
	future := time.Now().UTC().Add(365 * 24 * time.Hour) // 1 year in the future
	req := httptest.NewRequest("GET", "/?to="+future.Format(time.RFC3339), nil)
	_, to, err := parseTimeRange(req, 1*time.Hour)

	require.NoError(t, err)
	now := time.Now().UTC()
	// "to" must be clamped to now, not in the future.
	assert.WithinDuration(t, now, to, 2*time.Second)
	assert.False(t, to.After(now.Add(2*time.Second)), "to must not be in the future")
}

func TestParseTimeRange_FromAfterTo(t *testing.T) {
	now := time.Now().UTC()
	fromStr := now.Add(-1 * time.Hour).Format(time.RFC3339)
	toStr := now.Add(-2 * time.Hour).Format(time.RFC3339)

	req := httptest.NewRequest("GET", "/?from="+fromStr+"&to="+toStr, nil)
	from, to, err := parseTimeRange(req, 30*time.Minute)

	require.NoError(t, err)
	// "from" must be corrected to be before "to".
	assert.True(t, from.Before(to), "from (%v) must be before to (%v)", from, to)
}

func TestParseTimeRange_RangeCappedAt90Days(t *testing.T) {
	now := time.Now().UTC()
	fromStr := now.Add(-200 * 24 * time.Hour).Format(time.RFC3339) // 200 days ago
	toStr := now.Format(time.RFC3339)

	req := httptest.NewRequest("GET", "/?from="+fromStr+"&to="+toStr, nil)
	from, to, err := parseTimeRange(req, 1*time.Hour)

	require.NoError(t, err)
	duration := to.Sub(from)
	assert.LessOrEqual(t, duration, maxTimeRange, "range must be capped at %v, got %v", maxTimeRange, duration)
	// "from" should be exactly 90 days before "to".
	assert.WithinDuration(t, to.Add(-maxTimeRange), from, 2*time.Second)
}

func TestParseTimeRange_ExactlyAtMaxRange(t *testing.T) {
	now := time.Now().UTC()
	fromStr := now.Add(-maxTimeRange).Format(time.RFC3339)
	toStr := now.Format(time.RFC3339)

	req := httptest.NewRequest("GET", "/?from="+fromStr+"&to="+toStr, nil)
	from, to, err := parseTimeRange(req, 1*time.Hour)

	require.NoError(t, err)
	duration := to.Sub(from)
	// At exactly maxTimeRange, should be accepted without adjustment.
	assert.WithinDuration(t, now.Add(-maxTimeRange), from, 2*time.Second)
	assert.LessOrEqual(t, duration, maxTimeRange+2*time.Second)
}

func TestParseTimeRange_FutureFromAndTo(t *testing.T) {
	future1 := time.Now().UTC().Add(24 * time.Hour)
	future2 := time.Now().UTC().Add(48 * time.Hour)

	req := httptest.NewRequest("GET", "/?from="+future1.Format(time.RFC3339)+"&to="+future2.Format(time.RFC3339), nil)
	from, to, err := parseTimeRange(req, 1*time.Hour)

	require.NoError(t, err)
	now := time.Now().UTC()
	// Both should be clamped: "to" to now, "from" adjusted to be before "to".
	assert.WithinDuration(t, now, to, 2*time.Second)
	assert.True(t, from.Before(to), "from must be before to")
}
