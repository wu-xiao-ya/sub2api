package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestPerformanceRuntimePersistsEarliestGap(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	r := &channelPerformanceRuntime{db: db}
	at := time.Now().Truncate(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); r.noteGap(at.Add(time.Duration(i) * time.Second)) }(i)
	}
	wg.Wait()
	require.Equal(t, at.Unix(), r.gap.Load())
	// A transient write failure must leave the marker available to shutdown.
	mock.ExpectExec("UPDATE channel_performance_watermark").WithArgs(at).WillReturnError(errors.New("unavailable"))
	r.persistGap(context.Background())
	require.Equal(t, at.Unix(), r.gap.Load())
	mock.ExpectExec("UPDATE channel_performance_watermark").WithArgs(at).WillReturnResult(sqlmock.NewResult(0, 1))
	r.persistGap(context.Background())
	require.NoError(t, mock.ExpectationsWereMet())
	require.Equal(t, at.Unix(), r.gap.Load(), "a healthy write must not clear historic telemetry loss")
}
