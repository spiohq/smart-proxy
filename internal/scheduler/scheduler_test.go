package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_RunsJobsOnFirstTick(t *testing.T) {
	var count atomic.Int32
	jobs := []Job{
		{
			Name:     "test-job",
			Fn:       func(ctx context.Context) error { count.Add(1); return nil },
			Interval: 1 * time.Hour, // Long interval, but should fire on first tick
		},
	}

	sched := NewWithTick(jobs, 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	go sched.Run(ctx)
	time.Sleep(150 * time.Millisecond) // Wait for first tick (100ms in tests)
	cancel()

	assert.Equal(t, int32(1), count.Load())
}

func TestScheduler_RespectsInterval(t *testing.T) {
	var count atomic.Int32
	jobs := []Job{
		{
			Name:     "fast-job",
			Fn:       func(ctx context.Context) error { count.Add(1); return nil },
			Interval: 200 * time.Millisecond,
		},
	}

	sched := NewWithTick(jobs, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	go sched.Run(ctx)
	time.Sleep(550 * time.Millisecond) // Should fire ~3 times (0ms, 200ms, 400ms)
	cancel()

	// At least 2 runs, at most 4 (timing dependent)
	c := count.Load()
	assert.GreaterOrEqual(t, c, int32(2))
	assert.LessOrEqual(t, c, int32(4))
}

func TestScheduler_MultipleJobs(t *testing.T) {
	var countA, countB atomic.Int32
	jobs := []Job{
		{
			Name:     "job-a",
			Fn:       func(ctx context.Context) error { countA.Add(1); return nil },
			Interval: 100 * time.Millisecond,
		},
		{
			Name:     "job-b",
			Fn:       func(ctx context.Context) error { countB.Add(1); return nil },
			Interval: 100 * time.Millisecond,
		},
	}

	sched := NewWithTick(jobs, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	go sched.Run(ctx)
	time.Sleep(250 * time.Millisecond)
	cancel()

	assert.Greater(t, countA.Load(), int32(0))
	assert.Greater(t, countB.Load(), int32(0))
}

func TestScheduler_GracefulShutdown(t *testing.T) {
	var started, finished atomic.Bool
	var mu sync.Mutex
	var order []string

	jobs := []Job{
		{
			Name: "slow-job",
			Fn: func(ctx context.Context) error {
				started.Store(true)
				mu.Lock()
				order = append(order, "started")
				mu.Unlock()
				time.Sleep(200 * time.Millisecond)
				mu.Lock()
				order = append(order, "finished")
				mu.Unlock()
				finished.Store(true)
				return nil
			},
			Interval: 50 * time.Millisecond,
		},
	}

	sched := NewWithTick(jobs, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		sched.Run(ctx)
		close(done)
	}()

	// Wait for job to start
	time.Sleep(150 * time.Millisecond)
	require.True(t, started.Load(), "job should have started")

	cancel() // Signal shutdown

	select {
	case <-done:
		// Run returned
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not shut down in time")
	}

	assert.True(t, finished.Load(), "in-flight job should complete before shutdown")
}

func TestScheduler_JobErrorDoesNotStopOthers(t *testing.T) {
	var countGood atomic.Int32
	jobs := []Job{
		{
			Name:     "bad-job",
			Fn:       func(ctx context.Context) error { return assert.AnError },
			Interval: 100 * time.Millisecond,
		},
		{
			Name:     "good-job",
			Fn:       func(ctx context.Context) error { countGood.Add(1); return nil },
			Interval: 100 * time.Millisecond,
		},
	}

	sched := NewWithTick(jobs, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	go sched.Run(ctx)
	time.Sleep(250 * time.Millisecond)
	cancel()

	assert.Greater(t, countGood.Load(), int32(0), "good job should run despite bad job errors")
}
