//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"VK_AB_Lotty_task/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationGuardrailJobQueueLifecycle(t *testing.T) {
	store := newIntegrationStore(t)
	now := time.Now().UTC()
	expID := seedExperimentForJobs(t, store, "q")

	job := domain.GuardrailJob{
		ID:           unique("job"),
		ExperimentID: expID,
		WindowFrom:   now.Truncate(time.Minute),
		WindowTo:     now.Truncate(time.Minute).Add(time.Minute),
		WindowBucket: now.Unix() / 60,
		Reason:       "attributed_event",
		Status:       domain.GuardrailJobPending,
		AvailableAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.EnqueueGuardrailJob(job); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	dup := job
	dup.ID = unique("job")
	if err := store.EnqueueGuardrailJob(dup); err != nil {
		t.Fatalf("enqueue duplicate: %v", err)
	}

	claimed, err := store.ClaimGuardrailJobs(unique("worker"), 10, now.Add(time.Second))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected single claimed job after idempotent enqueue, got %d", len(claimed))
	}
	if err := store.CompleteGuardrailJob(claimed[0].ID, now.Add(2*time.Second)); err != nil {
		t.Fatalf("complete job: %v", err)
	}
	claimedAgain, err := store.ClaimGuardrailJobs(unique("worker"), 10, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("claim after complete: %v", err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("expected no claimable jobs after complete, got %d", len(claimedAgain))
	}
}

func TestIntegrationGuardrailJobRetriesToTerminalFailed(t *testing.T) {
	store := newIntegrationStore(t)
	now := time.Now().UTC()
	expID := seedExperimentForJobs(t, store, "retry")
	jobID := unique("job-retry")
	workerID := unique("worker")

	job := domain.GuardrailJob{
		ID:           jobID,
		ExperimentID: expID,
		WindowFrom:   now.Truncate(time.Minute),
		WindowTo:     now.Truncate(time.Minute).Add(time.Minute),
		WindowBucket: now.Unix() / 60,
		Reason:       "attributed_event",
		Status:       domain.GuardrailJobPending,
		AvailableAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.EnqueueGuardrailJob(job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	for i := 0; i < 5; i++ {
		claimed, err := store.ClaimGuardrailJobs(workerID, 1, now.Add(time.Second))
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claim round %d err=%v len=%d", i, err, len(claimed))
		}
		if err := store.FailGuardrailJob(jobID, now.Add(2*time.Second), "boom"); err != nil {
			t.Fatalf("fail round %d: %v", i, err)
		}
		now = now.Add(3 * time.Second)
	}

	claimed, err := store.ClaimGuardrailJobs(workerID, 1, now.Add(time.Second))
	if err != nil {
		t.Fatalf("final claim: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("expected terminal failed job not claimable, got %d", len(claimed))
	}
}

func TestIntegrationLeaderLeaseAndTriggerDedup(t *testing.T) {
	store := newIntegrationStore(t)
	now := time.Now().UTC()
	lockKey := unique("leader")
	w1 := unique("w1")
	w2 := unique("w2")

	ok, err := store.TryAcquireLeader(lockKey, w1, 5*time.Second, now)
	if err != nil || !ok {
		t.Fatalf("first acquire should succeed, ok=%v err=%v", ok, err)
	}
	ok, err = store.TryAcquireLeader(lockKey, w2, 5*time.Second, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("second acquire check err: %v", err)
	}
	if ok {
		t.Fatalf("second worker should not acquire active lease")
	}
	ok, err = store.TryAcquireLeader(lockKey, w2, 5*time.Second, now.Add(6*time.Second))
	if err != nil || !ok {
		t.Fatalf("second worker should acquire after ttl, ok=%v err=%v", ok, err)
	}

	expID := seedExperimentForJobs(t, store, "trig")
	trigger := domain.GuardrailTrigger{
		MetricKey:     "error_rate",
		Threshold:     0.05,
		WindowSeconds: 60,
		Action:        domain.GuardrailActionPause,
		ActualValue:   0.2,
		TriggeredAt:   now,
	}
	key := unique("incident")
	inserted, err := store.AddGuardrailTrigger(trigger, expID, key)
	if err != nil || !inserted {
		t.Fatalf("first trigger insert should succeed, inserted=%v err=%v", inserted, err)
	}
	inserted, err = store.AddGuardrailTrigger(trigger, expID, key)
	if err != nil {
		t.Fatalf("second trigger insert err: %v", err)
	}
	if inserted {
		t.Fatalf("duplicate incident key should be ignored")
	}
}

func newIntegrationStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "postgres://lotty:lotty@localhost:5433/lotty?sslmode=disable"
	}
	applySchema(t, dsn)
	s, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func applySchema(t *testing.T, dsn string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect raw pool: %v", err)
	}
	defer pool.Close()

	migrationPath := filepath.Join("..", "..", "..", "..", "migrations", "001_initial_schema.sql")
	sqlRaw, readErr := os.ReadFile(migrationPath)
	if readErr != nil {
		t.Fatalf("read migration file: %v", readErr)
	}
	if _, execErr := pool.Exec(context.Background(), string(sqlRaw)); execErr != nil {
		t.Fatalf("apply migration: %v", execErr)
	}
}

func seedExperimentForJobs(t *testing.T, s *Store, suffix string) string {
	t.Helper()
	now := time.Now().UTC()
	flagKey := unique("flag-" + suffix)
	expID := unique("exp-" + suffix)
	if err := s.PutFlag(domain.Flag{
		Key:          flagKey,
		Type:         domain.ValueTypeString,
		DefaultValue: json.RawMessage(`"A"`),
		Owner:        "exp",
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("put flag: %v", err)
	}
	err := s.PutExperiment(&domain.Experiment{
		ID:              expID,
		FlagKey:         flagKey,
		Name:            unique("name-" + suffix),
		OwnerID:         "exp",
		Status:          domain.StatusRunning,
		Version:         1,
		AudiencePercent: 100,
		Variants: []domain.Variant{
			{Name: "A", Value: json.RawMessage(`"A"`), Weight: 100, IsControl: true},
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("put experiment: %v", err)
	}
	return expID
}

func unique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
