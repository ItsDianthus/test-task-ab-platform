package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecideDeterministicForSameSubject(t *testing.T) {
	store := NewStore()
	svc := NewService(store, "test-salt", 2)
	actor := User{ID: "exp", Role: RoleExperimenter}

	_, err := svc.CreateFlag(actor, Flag{
		Key:          "button_color",
		Type:         ValueTypeString,
		DefaultValue: rawJSON(t, `"green"`),
	})
	if err != nil {
		t.Fatalf("create flag: %v", err)
	}

	exp, err := svc.CreateExperiment(actor, Experiment{
		FlagKey:         "button_color",
		Name:            "color test",
		AudiencePercent: 100,
		Variants: []Variant{
			{Name: "A", Value: rawJSON(t, `"blue"`), Weight: 50, IsControl: true},
			{Name: "B", Value: rawJSON(t, `"red"`), Weight: 50, IsControl: false},
		},
	})
	if err != nil {
		t.Fatalf("create experiment: %v", err)
	}
	exp.Status = StatusRunning
	if err := store.PutExperiment(exp); err != nil {
		t.Fatalf("save experiment: %v", err)
	}

	first, err := svc.Decide("u42", map[string]interface{}{"platform": "ios"}, []string{"button_color"})
	if err != nil {
		t.Fatalf("first decide: %v", err)
	}
	second, err := svc.Decide("u42", map[string]interface{}{"platform": "ios"}, []string{"button_color"})
	if err != nil {
		t.Fatalf("second decide: %v", err)
	}

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected decision count")
	}
	if string(first[0].Value) != string(second[0].Value) {
		t.Fatalf("expected deterministic variant, got %s and %s", string(first[0].Value), string(second[0].Value))
	}
}

func TestOutOfOrderEventsAreAttributedAfterExposure(t *testing.T) {
	store := NewStore()
	svc := NewService(store, "test-salt", 2)

	d := Decision{
		DecisionID:   "d1",
		SubjectID:    "u1",
		FlagKey:      "button_color",
		ExperimentID: "exp1",
		VariantName:  "A",
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.PutDecision(d); err != nil {
		t.Fatalf("put decision: %v", err)
	}

	result := svc.IngestEvents([]Event{
		{EventID: "c1", Type: "conversion", SubjectID: "u1", DecisionID: "d1", OccurredAt: time.Now().UTC()},
	})
	if result.Accepted != 1 || result.Rejected != 0 {
		t.Fatalf("unexpected ingest result: %+v", result)
	}
	attributedBefore, err := store.ListAttributed()
	if err != nil {
		t.Fatalf("list attributed before exposure: %v", err)
	}
	if got := len(attributedBefore); got != 0 {
		t.Fatalf("expected no attributed events before exposure, got %d", got)
	}

	_ = svc.IngestEvents([]Event{
		{EventID: "e1", Type: "exposure", SubjectID: "u1", DecisionID: "d1", OccurredAt: time.Now().UTC()},
	})
	attributedAfter, err := store.ListAttributed()
	if err != nil {
		t.Fatalf("list attributed after exposure: %v", err)
	}
	if got := len(attributedAfter); got != 2 {
		t.Fatalf("expected both exposure and queued conversion to be attributed, got %d", got)
	}
}

func TestVariantWeightsMustMatchAudience(t *testing.T) {
	err := validateVariants(20, []Variant{
		{Name: "A", Weight: 5, IsControl: true},
		{Name: "B", Weight: 5},
	})
	if err == nil {
		t.Fatal("expected weight validation error")
	}
}

func TestDuplicateEventWithDifferentPayloadRejected(t *testing.T) {
	store := NewStore()
	svc := NewService(store, "test-salt", 2)
	if err := store.PutEventType(EventType{Key: "conversion", RequiresExposure: false}); err != nil {
		t.Fatalf("put event type: %v", err)
	}

	first := svc.IngestEvents([]Event{{
		EventID:    "dup1",
		Type:       "conversion",
		SubjectID:  "u1",
		OccurredAt: time.Now().UTC(),
		Properties: map[string]interface{}{"v": 1.0},
	}})
	if first.Accepted != 1 {
		t.Fatalf("expected first accepted, got %+v", first)
	}

	second := svc.IngestEvents([]Event{{
		EventID:    "dup1",
		Type:       "conversion",
		SubjectID:  "u1",
		OccurredAt: time.Now().UTC(),
		Properties: map[string]interface{}{"v": 2.0},
	}})
	if second.Rejected != 1 {
		t.Fatalf("expected rejection for payload mismatch, got %+v", second)
	}
}

func TestDuplicateEventCountsWhenOccurredAtDiffersOnlyBelowMicrosecond(t *testing.T) {
	store := NewStore()
	svc := NewService(store, "test-salt", 2)
	if err := store.PutEventType(EventType{Key: "exposure", RequiresExposure: false}); err != nil {
		t.Fatalf("put event type: %v", err)
	}
	base := time.Now().UTC()
	tsAsInDB := base.Truncate(time.Microsecond)
	tsJSON := tsAsInDB.Add(123 * time.Nanosecond)
	if tsJSON.Equal(tsAsInDB) {
		t.Fatal("sanity: need sub-microsecond delta")
	}
	ev := Event{
		EventID:    "same-id",
		Type:       "exposure",
		SubjectID:  "u1",
		DecisionID: "d1",
		OccurredAt: tsAsInDB,
	}
	if !equivalentEvents(ev, Event{
		EventID:    "same-id",
		Type:       "exposure",
		SubjectID:  "u1",
		DecisionID: "d1",
		OccurredAt: tsJSON,
	}) {
		t.Fatal("expected equivalentEvents for PG-rounded vs RFC3339Nano")
	}
	first := svc.IngestEvents([]Event{{
		EventID:    "exp-dup",
		Type:       "exposure",
		SubjectID:  "u1",
		DecisionID: "d1",
		OccurredAt: tsAsInDB,
	}})
	if first.Accepted != 1 {
		t.Fatalf("expected first accepted, got %+v", first)
	}
	second := svc.IngestEvents([]Event{{
		EventID:    "exp-dup",
		Type:       "exposure",
		SubjectID:  "u1",
		DecisionID: "d1",
		OccurredAt: tsJSON,
	}})
	if second.Duplicate != 1 {
		t.Fatalf("expected duplicate when only sub-microsecond time differs, got %+v", second)
	}
}

func TestUpdateExperimentConfigCreatesNewVersion(t *testing.T) {
	store := NewStore()
	svc := NewService(store, "test-salt", 2)
	actor := User{ID: "exp", Role: RoleExperimenter}

	_, err := svc.CreateFlag(actor, Flag{
		Key:          "layout",
		Type:         ValueTypeString,
		DefaultValue: rawJSON(t, `"A"`),
	})
	if err != nil {
		t.Fatalf("create flag: %v", err)
	}
	exp, err := svc.CreateExperiment(actor, Experiment{
		FlagKey:         "layout",
		Name:            "layout-test",
		AudiencePercent: 100,
		Variants: []Variant{
			{Name: "A", Value: rawJSON(t, `"v1"`), Weight: 50, IsControl: true},
			{Name: "B", Value: rawJSON(t, `"v2"`), Weight: 50, IsControl: false},
		},
	})
	if err != nil {
		t.Fatalf("create experiment: %v", err)
	}

	updated, err := svc.UpdateExperimentConfig(actor, exp.ID, ExperimentConfigUpdate{
		AudiencePercent: 100,
		Variants: []Variant{
			{Name: "A", Value: rawJSON(t, `"v1"`), Weight: 20, IsControl: true},
			{Name: "B", Value: rawJSON(t, `"v2"`), Weight: 80, IsControl: false},
		},
	})
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
	if len(updated.Versions) != 2 {
		t.Fatalf("expected two snapshots, got %d", len(updated.Versions))
	}
}

func TestHashBucketDeterministic(t *testing.T) {
	svc := NewService(NewStore(), "test-salt", 2)
	a := svc.HashBucket("u42|exp1|v1|salt", 100)
	b := svc.HashBucket("u42|exp1|v1|salt", 100)
	if a != b {
		t.Fatalf("expected deterministic bucket, got %d and %d", a, b)
	}
}

func rawJSON(t *testing.T, v string) json.RawMessage {
	t.Helper()
	return json.RawMessage(v)
}
