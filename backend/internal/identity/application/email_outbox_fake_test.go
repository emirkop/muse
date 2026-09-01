package application_test

import (
	"context"
	"errors"
	"sync"
	"time"

	"muse-backend/internal/identity/application"
)

type fakeOutbox struct {
	mu          sync.Mutex
	rows        map[string]*fakeOutboxRow
	enqueues    int
	failEnqueue bool
	order       []string
}

type fakeOutboxRow struct {
	job           application.EmailJob
	status        string
	nextAttemptAt time.Time
	lockedUntil   *time.Time
	lastError     string
}

func newFakeOutbox() *fakeOutbox {
	return &fakeOutbox{rows: map[string]*fakeOutboxRow{}}
}

func (f *fakeOutbox) Enqueue(_ context.Context, job application.EmailJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueues++
	if f.failEnqueue {
		return errors.New("outbox unavailable")
	}
	f.rows[job.ID] = &fakeOutboxRow{job: job, status: "pending", nextAttemptAt: job.EnqueuedAt}
	f.order = append(f.order, job.ID)
	return nil
}

func (f *fakeOutbox) Claim(_ context.Context, now time.Time, lease time.Duration, limit int) ([]application.EmailJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var claimed []application.EmailJob
	for _, id := range f.order {
		row, ok := f.rows[id]
		if !ok || row.status != "pending" {
			continue
		}
		if row.nextAttemptAt.After(now) {
			continue
		}
		if row.lockedUntil != nil && row.lockedUntil.After(now) {
			continue
		}
		until := now.Add(lease)
		row.lockedUntil = &until
		row.job.Attempts++
		claimed = append(claimed, row.job)
		if len(claimed) >= limit {
			break
		}
	}
	return claimed, nil
}

func (f *fakeOutbox) Complete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}

func (f *fakeOutbox) Reschedule(_ context.Context, id string, nextAttemptAt time.Time, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.rows[id]; ok && row.status == "pending" {
		row.nextAttemptAt = nextAttemptAt
		row.lockedUntil = nil
		row.lastError = reason
	}
	return nil
}

func (f *fakeOutbox) MarkDead(_ context.Context, id string, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.rows[id]; ok {
		row.status = "dead"
		row.job.Email = ""
		row.lockedUntil = nil
		row.lastError = reason
	}
	return nil
}

func (f *fakeOutbox) pendingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, row := range f.rows {
		if row.status == "pending" {
			n++
		}
	}
	return n
}

func (f *fakeOutbox) deadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, row := range f.rows {
		if row.status == "dead" {
			n++
		}
	}
	return n
}

func (f *fakeOutbox) rowCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

func (f *fakeOutbox) only() *fakeOutboxRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		copy := *row
		return &copy
	}
	return nil
}

func (f *fakeOutbox) resetCounters() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueues = 0
}
