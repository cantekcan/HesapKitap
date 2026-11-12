package queue_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/platform/db/dbtest"
	"hesapkitap/internal/platform/queue"
)

func TestInsertOnlyClientAndRiverSchema(t *testing.T) {
	pool := dbtest.NewPool(t)

	c, err := queue.New(pool, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	require.NotNil(t, c)

	// `hesapkitap migrate` River tablolarını da kurmuş olmalı.
	var exists bool
	require.NoError(t, pool.QueryRow(t.Context(), `select to_regclass('river_job') is not null`).Scan(&exists))
	assert.True(t, exists, "river_job tablosu migration ile oluşmalı")
}
