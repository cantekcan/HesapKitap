package cheque

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }

// fakeStore records the last SetStatus call; current is the status the "row" has.
type fakeStore struct {
	Store
	current Status
	exists  bool
	to      Status
	from    []Status
}

func (f *fakeStore) SetStatus(_ context.Context, _ uuid.UUID, to Status, from []Status, _ time.Time) (bool, error) {
	f.to, f.from = to, from
	if !f.exists {
		return false, nil
	}
	for _, s := range from {
		if s == f.current {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) Get(context.Context, uuid.UUID) (Cheque, error) {
	if !f.exists {
		return Cheque{}, ErrNotFound
	}
	return Cheque{Status: f.current}, nil
}

func TestSetStatusRules(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name    string
		current Status
		to      Status
		exists  bool
		want    error
	}{
		{"portföyden tahsil", StatusPortfolio, StatusCollected, true, nil},
		{"portföyden karşılıksız", StatusPortfolio, StatusBounced, true, nil},
		{"karşılıksızdan portföye", StatusBounced, StatusPortfolio, true, nil},
		{"tahsilden karşılıksıza geçilmez", StatusCollected, StatusBounced, true, ErrBadTransition},
		{"ciro doğrudan yapılamaz", StatusPortfolio, StatusEndorsed, true, ErrBadTransition},
		{"ciro edilmiş portföye alınamaz", StatusEndorsed, StatusPortfolio, true, ErrBadTransition},
		{"olmayan çek", StatusPortfolio, StatusCollected, false, ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Service{Store: &fakeStore{current: c.current, exists: c.exists}}
			err := s.SetStatus(t.Context(), id, c.to)
			if c.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, c.want)
			}
		})
	}
}

func TestBuildFlowBucketsDues(t *testing.T) {
	today := d(2026, time.March, 15)
	in := []Due{
		{d(2026, time.March, 10), 100}, // vadesi geçmiş
		{d(2026, time.March, 15), 200}, // bugün: bu ay
		{d(2026, time.April, 1), 300},
		{d(2026, time.August, 1), 400}, // 3 aylık tabloda son aya toplanır
	}
	out := []Due{{d(2026, time.April, 20), 50}, {d(2026, time.February, 1), 70}}
	got := BuildFlow(today, in, out, 3)

	require.Len(t, got, 4)
	assert.True(t, got[0].Overdue)
	assert.Equal(t, int64(100), got[0].In)
	assert.Equal(t, int64(70), got[0].Out)
	assert.Equal(t, d(2026, time.March, 1), got[1].Month)
	assert.Equal(t, int64(200), got[1].In)
	assert.Equal(t, int64(300), got[2].In)
	assert.Equal(t, int64(50), got[2].Out)
	assert.Equal(t, int64(400), got[3].In)
	assert.Equal(t, int64(250), got[2].Net())
	assert.Equal(t, int64(400), got[3].Net())
}

func TestHabitChequeShare(t *testing.T) {
	assert.Equal(t, 0, Habit{}.ChequeShare())
	assert.Equal(t, 75, Habit{Paid: 400, ByCheque: 300}.ChequeShare())
}
