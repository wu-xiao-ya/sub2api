package service

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type channelPerformanceRuntime struct {
	repo     ChannelPerformanceFactRepository
	db       *sql.DB
	instance string
	queue    chan ChannelPerformanceFact
	stop     chan struct{}
	done     chan struct{}
	mu       sync.RWMutex
	stopping bool
	gap      atomic.Int64
	once     sync.Once
}

func (s *ChannelPerformanceService) startRuntime(db *sql.DB) {
	repo, ok := s.repo.(ChannelPerformanceFactRepository)
	if !ok || db == nil {
		return
	}
	r := &channelPerformanceRuntime{repo: repo, db: db, instance: uuid.NewString(), queue: make(chan ChannelPerformanceFact, 1024), stop: make(chan struct{}), done: make(chan struct{})}
	s.runtime = r
	go r.run()
}

// Requests never block on performance telemetry or enqueue request bodies.
func (s *ChannelPerformanceService) RecordFact(f ChannelPerformanceFact) {
	if s == nil || s.runtime == nil || f.Validate() != nil {
		return
	}
	f.Latency = f.Latency.Clone()
	if f.Stream != nil {
		value := *f.Stream
		f.Stream = &value
	}
	r := s.runtime
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.stopping {
		r.noteGap(f.StartedAt)
		return
	}
	select {
	case r.queue <- f:
	default:
		r.noteGap(f.StartedAt)
	}
}

func (s *ChannelPerformanceService) Stop() {
	if s == nil || s.runtime == nil {
		return
	}
	r := s.runtime
	r.once.Do(func() { r.mu.Lock(); r.stopping = true; close(r.stop); r.mu.Unlock() })
	<-r.done
}

func (r *channelPerformanceRuntime) flush(batch []ChannelPerformanceFact) {
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.repo.RecordFacts(ctx, batch); err != nil {
		for _, fact := range batch {
			r.noteGap(fact.StartedAt)
		}
		slog.Warn("channel performance fact batch failed", "error", err)
	}
}

func (r *channelPerformanceRuntime) run() {
	defer close(r.done)
	ctx, cancel := context.WithCancel(context.Background())
	aggregateDone := make(chan struct{})
	go func() { defer close(aggregateDone); r.aggregate(ctx) }()
	defer func() {
		cancel()
		<-aggregateDone
		// A failed final drain must survive graceful restart even if no regular
		// aggregation tick followed it. Use a fresh bounded shutdown context.
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer flushCancel()
		r.persistGap(flushCtx)
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]ChannelPerformanceFact, 0, 64)
	for {
		select {
		case fact := <-r.queue:
			batch = append(batch, fact)
			if len(batch) == 64 {
				r.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			r.flush(batch)
			batch = batch[:0]
		case <-r.stop:
			// Stop ingress first. Drain bounded pending telemetry while database
			// dependencies remain alive; never hang shutdown indefinitely.
			deadline := time.Now().Add(6 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case fact := <-r.queue:
					batch = append(batch, fact)
				default:
					r.flush(batch)
					return
				}
				if len(batch) == 64 {
					r.flush(batch)
					batch = batch[:0]
				}
			}
			for _, fact := range batch {
				r.noteGap(fact.StartedAt)
			}
			for {
				select {
				case fact := <-r.queue:
					r.noteGap(fact.StartedAt)
				default:
					return
				}
			}
		}
	}
}

func (r *channelPerformanceRuntime) aggregate(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		runCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
		r.persistGap(runCtx)
		release, acquired := tryAcquireSingletonLeaderLock(runCtx, nil, r.db, "channel-performance-aggregator", r.instance, 2*time.Minute)
		if acquired {
			if err := r.repo.ProcessPending(runCtx, time.Now().UTC()); err != nil && ctx.Err() == nil {
				slog.Warn("channel performance aggregation deferred", "error", err)
			}
			release()
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *channelPerformanceRuntime) noteGap(at time.Time) {
	for {
		old := r.gap.Load()
		if old != 0 && old <= at.Unix() {
			return
		}
		if r.gap.CompareAndSwap(old, at.Unix()) {
			return
		}
	}
}

func (r *channelPerformanceRuntime) persistGap(ctx context.Context) {
	if gap := r.gap.Load(); gap != 0 {
		// Sticky loss cannot be cleared by a later healthy batch.
		_, err := r.db.ExecContext(ctx, "UPDATE channel_performance_watermark SET incomplete_since=LEAST(COALESCE(incomplete_since,$1),$1) WHERE id=1", time.Unix(gap, 0))
		if err != nil {
			slog.Warn("channel performance coverage marker failed", "error", err)
		}
	}
}
