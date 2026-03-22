package domain

import "testing"

func TestMemoryStoreEventDedupAndPendingDrain(t *testing.T) {
	store := NewMemoryStore()
	e := Event{EventID: "e1", Type: "conversion", SubjectID: "u1"}

	dup, err := store.SaveEvent(e)
	if err != nil || dup {
		t.Fatalf("first save should not be duplicate, dup=%v err=%v", dup, err)
	}
	dup, err = store.SaveEvent(e)
	if err != nil || !dup {
		t.Fatalf("second save should be duplicate, dup=%v err=%v", dup, err)
	}

	if err := store.PutPending("d1", e); err != nil {
		t.Fatalf("put pending: %v", err)
	}
	pending, err := store.DrainPending("d1")
	if err != nil {
		t.Fatalf("drain pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending event, got %d", len(pending))
	}
	again, err := store.DrainPending("d1")
	if err != nil {
		t.Fatalf("drain second time: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected empty after drain, got %d", len(again))
	}
}

func TestMemoryStoreResolveApproverPolicyFallback(t *testing.T) {
	store := NewMemoryStore()
	policy, err := store.ResolveApproverPolicy("unknown-exp")
	if err != nil {
		t.Fatalf("resolve policy: %v", err)
	}
	if policy.MinApprovals != 1 {
		t.Fatalf("expected default min approvals 1, got %d", policy.MinApprovals)
	}
	if len(policy.ApproverIDs) == 0 {
		t.Fatalf("expected fallback approvers")
	}
}

func TestMemoryStoreFindActiveExperimentByFlag(t *testing.T) {
	store := NewMemoryStore()
	exp := &Experiment{
		ID:      "exp-active",
		FlagKey: "f1",
		Status:  StatusRunning,
	}
	if err := store.PutExperiment(exp); err != nil {
		t.Fatalf("put experiment: %v", err)
	}
	got, ok, err := store.FindActiveExperimentByFlag("f1")
	if err != nil {
		t.Fatalf("find active: %v", err)
	}
	if !ok || got.ID != "exp-active" {
		t.Fatalf("unexpected active experiment, ok=%v id=%v", ok, got.ID)
	}
}

func TestMemoryStoreGuardrailTriggerDedup(t *testing.T) {
	store := NewMemoryStore()
	trigger := GuardrailTrigger{MetricKey: "error_rate"}

	inserted, err := store.AddGuardrailTrigger(trigger, "exp-1", "inc-key")
	if err != nil || !inserted {
		t.Fatalf("first insert expected true, inserted=%v err=%v", inserted, err)
	}
	inserted, err = store.AddGuardrailTrigger(trigger, "exp-1", "inc-key")
	if err != nil {
		t.Fatalf("second insert err: %v", err)
	}
	if inserted {
		t.Fatalf("expected duplicate incident key to be ignored")
	}
}
