package domain

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestGuardrailJobEnqueueIdempotent(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	job1 := GuardrailJob{
		ID:           "job-1",
		ExperimentID: "exp-1",
		WindowFrom:   now.Truncate(time.Minute),
		WindowTo:     now.Truncate(time.Minute).Add(time.Minute),
		WindowBucket: now.Unix() / 60,
		Reason:       "attributed_event",
		Status:       GuardrailJobPending,
		AvailableAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	job2 := job1
	job2.ID = "job-2"

	if err := store.EnqueueGuardrailJob(job1); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	if err := store.EnqueueGuardrailJob(job2); err != nil {
		t.Fatalf("enqueue duplicate: %v", err)
	}

	claimed, err := store.ClaimGuardrailJobs("w1", 10, now.Add(time.Second))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected single job after idempotent enqueue, got %d", len(claimed))
	}
}

func TestGuardrailJobsClaimNoDoubleProcessing(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	for i := 0; i < 20; i++ {
		job := GuardrailJob{
			ID:           fmt.Sprintf("job-%d", i),
			ExperimentID: "exp-1",
			WindowFrom:   now.Truncate(time.Minute),
			WindowTo:     now.Truncate(time.Minute).Add(time.Minute),
			WindowBucket: now.Unix()/60 + int64(i),
			Reason:       "attributed_event",
			Status:       GuardrailJobPending,
			AvailableAt:  now,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := store.EnqueueGuardrailJob(job); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			jobs, err := store.ClaimGuardrailJobs(fmt.Sprintf("w-%d", worker), 10, now.Add(time.Second))
			if err != nil {
				t.Errorf("claim worker %d: %v", worker, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, j := range jobs {
				if seen[j.ID] {
					t.Errorf("job claimed twice: %s", j.ID)
				}
				seen[j.ID] = true
			}
		}(i)
	}
	wg.Wait()

	if len(seen) != 20 {
		t.Fatalf("expected all jobs claimed exactly once, got %d", len(seen))
	}
}

func TestGuardrailJobFailRetryAndTerminalState(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	job := GuardrailJob{
		ID:           "job-retry",
		ExperimentID: "exp-1",
		WindowFrom:   now.Truncate(time.Minute),
		WindowTo:     now.Truncate(time.Minute).Add(time.Minute),
		WindowBucket: now.Unix() / 60,
		Reason:       "attributed_event",
		Status:       GuardrailJobPending,
		AvailableAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.EnqueueGuardrailJob(job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	for i := 0; i < 5; i++ {
		claimed, err := store.ClaimGuardrailJobs("w1", 1, now.Add(time.Second))
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claim round %d failed: %v len=%d", i, err, len(claimed))
		}
		err = store.FailGuardrailJob(claimed[0].ID, now.Add(2*time.Second), "boom")
		if err != nil {
			t.Fatalf("fail round %d: %v", i, err)
		}
		now = now.Add(3 * time.Second)
	}

	claimed, err := store.ClaimGuardrailJobs("w1", 1, now.Add(time.Second))
	if err != nil {
		t.Fatalf("final claim: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("expected terminal failed job to not be claimable")
	}
}

func TestLeaderLeaseSingleActiveWorker(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()

	ok, err := store.TryAcquireLeader("guardrail", "w1", 5*time.Second, now)
	if err != nil || !ok {
		t.Fatalf("w1 should acquire lease: ok=%v err=%v", ok, err)
	}
	ok, err = store.TryAcquireLeader("guardrail", "w2", 5*time.Second, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("w2 lease check failed: %v", err)
	}
	if ok {
		t.Fatalf("w2 should not acquire active lease")
	}
	ok, err = store.TryAcquireLeader("guardrail", "w2", 5*time.Second, now.Add(6*time.Second))
	if err != nil || !ok {
		t.Fatalf("w2 should acquire expired lease: ok=%v err=%v", ok, err)
	}
}
