package domain

import (
	"errors"
	"testing"
	"time"
)

func TestJobTransitions(t *testing.T) {
	all := []JobStatus{JobQueued, JobRunning, JobCompleted, JobFailed, JobBlocked, JobCancelled}
	allowed := map[JobStatus]map[JobStatus]bool{
		JobQueued:  {JobRunning: true, JobCancelled: true, JobFailed: true},
		JobRunning: {JobCompleted: true, JobFailed: true, JobBlocked: true, JobCancelled: true},
	}
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	for _, from := range all {
		for _, to := range all {
			job := Job{Status: from}
			err := job.Transition(to, now)
			if allowed[from][to] {
				if err != nil || job.Status != to {
					t.Errorf("%s -> %s debería permitirse: %v", from, to, err)
				}
			} else if !errors.Is(err, ErrInvalidTransition) || job.Status != from {
				t.Errorf("%s -> %s debería rechazarse (err=%v, estado=%s)", from, to, err, job.Status)
			}
		}
	}
}

func TestJobTransitionSetsTimestamps(t *testing.T) {
	start := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	job := Job{Status: JobQueued}

	_ = job.Transition(JobRunning, start)
	if job.StartedAt == nil || !job.StartedAt.Equal(start) || job.FinishedAt != nil {
		t.Fatalf("running: %+v", job)
	}
	_ = job.Transition(JobCompleted, end)
	if job.FinishedAt == nil || !job.FinishedAt.Equal(end) {
		t.Fatalf("completed: %+v", job)
	}
}

func TestStatusAndEventTerminal(t *testing.T) {
	for status, want := range map[JobStatus]bool{JobQueued: false, JobRunning: false, JobCompleted: true, JobFailed: true, JobBlocked: true, JobCancelled: true, "raro": false} {
		if status.Terminal() != want {
			t.Errorf("%s.Terminal() = %v", status, !want)
		}
		if want && !(JobEvent{Type: TerminalEventType(status)}).Terminal() {
			t.Errorf("el evento de %s debería ser final", status)
		}
	}
	if (JobEvent{Type: EventPageOpened}).Terminal() {
		t.Error("page.opened no es final")
	}
}
