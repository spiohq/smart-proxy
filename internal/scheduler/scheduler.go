package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// DefaultTick is the base tick interval for production use (1 minute).
const DefaultTick = 1 * time.Minute

// Job defines a named function to run at a fixed interval.
type Job struct {
	Name     string
	Fn       func(ctx context.Context) error
	Interval time.Duration
}

// Scheduler runs registered jobs at their configured intervals.
type Scheduler struct {
	jobs    []Job
	lastRun map[string]time.Time
	tick    time.Duration
}

// New creates a scheduler with the given jobs using the default tick interval.
func New(jobs []Job) *Scheduler {
	return NewWithTick(jobs, DefaultTick)
}

// NewWithTick creates a scheduler with a custom tick interval (useful for tests).
func NewWithTick(jobs []Job, tick time.Duration) *Scheduler {
	return &Scheduler{
		jobs:    jobs,
		lastRun: make(map[string]time.Time),
		tick:    tick,
	}
}

// Run blocks until ctx is cancelled, executing jobs when their interval elapses.
// Jobs run sequentially  -  no concurrent execution. In-flight jobs complete
// before Run returns on context cancellation.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for _, job := range s.jobs {
				// Stop scheduling new jobs if shutdown was requested
				if ctx.Err() != nil {
					return
				}
				if now.Sub(s.lastRun[job.Name]) >= job.Interval {
					s.lastRun[job.Name] = now
					// Use a detached context for the job so in-flight DB operations
					// are not cancelled mid-query during graceful shutdown.
					if err := job.Fn(context.Background()); err != nil {
						slog.Error("scheduler job failed", "job", job.Name, "error", err)
					}
				}
			}
		}
	}
}
