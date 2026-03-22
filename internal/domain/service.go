package domain

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

type Service struct {
	store                      Store
	stickinessSalt             string
	maxSimultaneousExperiments int
}

func NewService(store Store, salt string, maxSimultaneousExperiments int) *Service {
	if maxSimultaneousExperiments <= 0 {
		maxSimultaneousExperiments = 2
	}
	return &Service{
		store:                      store,
		stickinessSalt:             salt,
		maxSimultaneousExperiments: maxSimultaneousExperiments,
	}
}

func (s *Service) CreateFlag(actor User, flag Flag) (Flag, error) {
	return s.CreateFlagIdempotent(actor, flag, "")
}

func (s *Service) CreateFlagIdempotent(actor User, flag Flag, idemKey string) (Flag, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleExperimenter {
		return Flag{}, errors.New("forbidden")
	}
	if flag.Key == "" {
		return Flag{}, errors.New("flag key is required")
	}
	if err := flag.Type.ValidateValue(flag.DefaultValue); err != nil {
		return Flag{}, err
	}
	if _, exists, err := s.store.GetFlag(flag.Key); err != nil {
		return Flag{}, err
	} else if exists {
		existing, _, err := s.store.GetFlag(flag.Key)
		if err != nil {
			return Flag{}, err
		}
		if idemKey != "" {
			return existing, nil
		}
		if sameJSON(existing.DefaultValue, flag.DefaultValue) && existing.Type == flag.Type {
			return existing, nil
		}
		return Flag{}, errors.New("flag already exists")
	}
	now := time.Now().UTC()
	flag.CreatedAt = now
	flag.UpdatedAt = now
	if err := s.store.PutFlag(flag); err != nil {
		return Flag{}, err
	}
	s.appendAudit("flag", flag.Key, "flag_created", actor.ID, map[string]interface{}{
		"idempotency_key": idemKey,
	})
	return flag, nil
}

func (s *Service) UpdateFlagDefault(actor User, key string, value json.RawMessage) (Flag, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleExperimenter {
		return Flag{}, errors.New("forbidden")
	}
	flag, ok, err := s.store.GetFlag(key)
	if err != nil {
		return Flag{}, err
	}
	if !ok {
		return Flag{}, errors.New("flag not found")
	}
	if err := flag.Type.ValidateValue(value); err != nil {
		return Flag{}, err
	}
	oldValue := append(json.RawMessage(nil), flag.DefaultValue...)
	flag.DefaultValue = value
	flag.UpdatedAt = time.Now().UTC()
	if err := s.store.PutFlag(flag); err != nil {
		return Flag{}, err
	}
	s.appendAudit("flag", key, "flag_default_updated", actor.ID, map[string]interface{}{
		"old_default": json.RawMessage(oldValue),
		"new_default": value,
	})
	return flag, nil
}

func (s *Service) CreateExperiment(actor User, exp Experiment) (*Experiment, error) {
	return s.CreateExperimentIdempotent(actor, exp, "")
}

func (s *Service) CreateExperimentIdempotent(actor User, exp Experiment, idemKey string) (*Experiment, error) {
	if actor.Role != RoleExperimenter && actor.Role != RoleAdmin {
		return nil, errors.New("forbidden")
	}
	if _, ok, err := s.store.GetFlag(exp.FlagKey); err != nil {
		return nil, err
	} else if !ok {
		return nil, errors.New("flag not found")
	}
	if err := validateVariants(exp.AudiencePercent, exp.Variants); err != nil {
		return nil, err
	}
	if existing, ok, err := s.store.FindExperimentByOwnerAndName(actor.ID, exp.Name); err == nil && ok {
		if idemKey != "" {
			return existing, nil
		}
		if existing.FlagKey == exp.FlagKey {
			return existing, nil
		}
	}
	now := time.Now().UTC()
	exp.ID = randomID("exp")
	exp.Status = StatusDraft
	exp.Version = 1
	exp.OwnerID = actor.ID
	exp.CreatedAt = now
	exp.UpdatedAt = now
	exp.Versions = []ExperimentVersion{
		{
			Version:         1,
			AudiencePercent: exp.AudiencePercent,
			Variants:        cloneVariants(exp.Variants),
			Targeting:       exp.Targeting,
			UpdatedAt:       now,
			UpdatedBy:       actor.ID,
		},
	}
	if err := s.store.PutExperiment(&exp); err != nil {
		return nil, err
	}
	s.appendAudit("experiment", exp.ID, "experiment_created", actor.ID, map[string]interface{}{
		"idempotency_key": idemKey,
		"version":         exp.Version,
	})
	return &exp, nil
}

func (s *Service) SubmitForReview(actor User, expID string) (*Experiment, error) {
	exp, err := s.requireExperiment(expID)
	if err != nil {
		return nil, err
	}
	if exp.OwnerID != actor.ID && actor.Role != RoleAdmin {
		return nil, errors.New("forbidden")
	}
	if exp.Status != StatusDraft && exp.Status != StatusRejected {
		return nil, errors.New("only draft/rejected can be submitted")
	}
	exp.Status = StatusInReview
	exp.UpdatedAt = time.Now().UTC()
	if err := s.store.PutExperiment(exp); err != nil {
		return nil, err
	}
	s.appendAudit("experiment", exp.ID, "sent_to_review", actor.ID, nil)
	return exp, nil
}

func (s *Service) Review(actor User, expID string, decision ReviewDecision, comment string) (*Experiment, error) {
	if actor.Role != RoleApprover && actor.Role != RoleAdmin {
		return nil, errors.New("forbidden")
	}
	exp, err := s.requireExperiment(expID)
	if err != nil {
		return nil, err
	}
	if exp.Status != StatusInReview {
		return nil, errors.New("experiment is not in review")
	}
	policy, err := s.store.ResolveApproverPolicy(exp.OwnerID)
	if err != nil {
		return nil, err
	}
	if !contains(policy.ApproverIDs, actor.ID) && actor.Role != RoleAdmin {
		return nil, errors.New("actor is not allowed to review this experiment")
	}
	entry := ReviewEntry{
		ExperimentID: exp.ID,
		ReviewerID:   actor.ID,
		Decision:     decision,
		Comment:      comment,
		CreatedAt:    time.Now().UTC(),
	}
	exp.ReviewHistory = append(exp.ReviewHistory, entry)
	switch decision {
	case ReviewRequestChange:
		exp.Status = StatusDraft
		s.appendAudit("experiment", exp.ID, "review_requested_changes", actor.ID, map[string]interface{}{"comment": comment})
	case ReviewReject:
		exp.Status = StatusRejected
		s.appendAudit("experiment", exp.ID, "review_rejected", actor.ID, map[string]interface{}{"comment": comment})
	case ReviewApprove:
		approvedBy := map[string]bool{}
		for _, h := range exp.ReviewHistory {
			if h.Decision == ReviewApprove {
				approvedBy[h.ReviewerID] = true
			}
		}
		if len(approvedBy) >= policy.MinApprovals {
			exp.Status = StatusApproved
		}
		s.appendAudit("experiment", exp.ID, "review_approved", actor.ID, map[string]interface{}{"comment": comment})
	default:
		return nil, errors.New("unknown review decision")
	}
	exp.UpdatedAt = time.Now().UTC()
	if err := s.store.PutExperiment(exp); err != nil {
		return nil, err
	}
	return exp, nil
}

func (s *Service) StartExperiment(actor User, expID string) (*Experiment, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleExperimenter {
		return nil, errors.New("forbidden")
	}
	exp, err := s.requireExperiment(expID)
	if err != nil {
		return nil, err
	}
	if exp.Status != StatusApproved {
		return nil, errors.New("only approved experiment can be started")
	}
	if active, ok, err := s.store.FindActiveExperimentByFlag(exp.FlagKey); err != nil {
		return nil, err
	} else if ok && active.ID != exp.ID {
		return nil, errors.New("another active experiment already uses this flag")
	}
	exp.Status = StatusRunning
	exp.UpdatedAt = time.Now().UTC()
	if err := s.store.PutExperiment(exp); err != nil {
		return nil, err
	}
	s.appendAudit("experiment", exp.ID, "experiment_started", actor.ID, nil)
	return exp, nil
}

func (s *Service) PauseExperiment(actor User, expID string) (*Experiment, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleExperimenter {
		return nil, errors.New("forbidden")
	}
	exp, err := s.requireExperiment(expID)
	if err != nil {
		return nil, err
	}
	if exp.Status != StatusRunning {
		return nil, errors.New("only running experiment can be paused")
	}
	exp.Status = StatusPaused
	exp.UpdatedAt = time.Now().UTC()
	if err := s.store.PutExperiment(exp); err != nil {
		return nil, err
	}
	s.appendAudit("experiment", exp.ID, "experiment_paused", actor.ID, nil)
	return exp, nil
}

func (s *Service) ResumeExperiment(actor User, expID string) (*Experiment, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleExperimenter {
		return nil, errors.New("forbidden")
	}
	exp, err := s.requireExperiment(expID)
	if err != nil {
		return nil, err
	}
	if exp.Status != StatusPaused {
		return nil, errors.New("only paused experiment can be resumed")
	}
	exp.Status = StatusRunning
	exp.UpdatedAt = time.Now().UTC()
	if err := s.store.PutExperiment(exp); err != nil {
		return nil, err
	}
	s.appendAudit("experiment", exp.ID, "experiment_resumed", actor.ID, nil)
	return exp, nil
}

func (s *Service) CompleteExperiment(actor User, expID, comment string) (*Experiment, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleExperimenter {
		return nil, errors.New("forbidden")
	}
	exp, err := s.requireExperiment(expID)
	if err != nil {
		return nil, err
	}
	if exp.Status != StatusRunning && exp.Status != StatusPaused {
		return nil, errors.New("only running or paused can be completed")
	}
	exp.Status = StatusCompleted
	exp.DecisionComment = comment
	exp.UpdatedAt = time.Now().UTC()
	if err := s.store.PutExperiment(exp); err != nil {
		return nil, err
	}
	s.appendAudit("experiment", exp.ID, "experiment_completed", actor.ID, map[string]interface{}{
		"comment": comment,
	})
	return exp, nil
}

func (s *Service) UpdateExperimentConfig(actor User, expID string, upd ExperimentConfigUpdate) (*Experiment, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleExperimenter {
		return nil, errors.New("forbidden")
	}
	exp, err := s.requireExperiment(expID)
	if err != nil {
		return nil, err
	}
	if exp.OwnerID != actor.ID && actor.Role != RoleAdmin {
		return nil, errors.New("forbidden")
	}
	if exp.Status != StatusDraft {
		return nil, errors.New("only draft experiment can be updated")
	}
	if err := validateVariants(upd.AudiencePercent, upd.Variants); err != nil {
		return nil, err
	}
	oldVersion := exp.Version
	exp.AudiencePercent = upd.AudiencePercent
	exp.Variants = cloneVariants(upd.Variants)
	exp.Targeting = upd.Targeting
	exp.Guardrails = append([]GuardrailRule(nil), upd.Guardrails...)
	exp.Version++
	exp.UpdatedAt = time.Now().UTC()
	versionSnapshot := ExperimentVersion{
		Version:         exp.Version,
		AudiencePercent: exp.AudiencePercent,
		Variants:        cloneVariants(exp.Variants),
		Targeting:       exp.Targeting,
		UpdatedAt:       exp.UpdatedAt,
		UpdatedBy:       actor.ID,
	}
	exp.Versions = append(exp.Versions, versionSnapshot)
	if err := s.store.PutExperiment(exp); err != nil {
		return nil, err
	}
	s.appendAudit("experiment", exp.ID, "experiment_config_updated", actor.ID, map[string]interface{}{
		"from_version": oldVersion,
		"to_version":   exp.Version,
	})
	return exp, nil
}

func (s *Service) Decide(subjectID string, attrs map[string]interface{}, flagKeys []string) ([]Decision, error) {
	if subjectID == "" {
		return nil, errors.New("subject_id is required")
	}
	type candidate struct {
		flagKey string
		exp     *Experiment
		score   float64
	}
	candidates := make([]candidate, 0, len(flagKeys))
	for _, key := range flagKeys {
		exp, ok, err := s.store.FindActiveExperimentByFlag(key)
		if err != nil {
			return nil, err
		}
		if !ok || exp.Status != StatusRunning {
			continue
		}
		allowed, err := EvaluateRule(exp.Targeting, attrs)
		if err != nil || !allowed {
			continue
		}
		eligible := inAudience(subjectID, exp.AudiencePercent, exp.ID, s.stickinessSalt)
		if !eligible {
			continue
		}
		candidates = append(candidates, candidate{
			flagKey: key,
			exp:     exp,
			score:   hashFraction(subjectID + "|" + exp.ID + "|participation|" + s.stickinessSalt),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})
	selected := map[string]*Experiment{}
	for i := 0; i < len(candidates) && i < s.maxSimultaneousExperiments; i++ {
		selected[candidates[i].flagKey] = candidates[i].exp
	}

	decisions := make([]Decision, 0, len(flagKeys))
	now := time.Now().UTC()
	for _, key := range flagKeys {
		flag, ok, err := s.store.GetFlag(key)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		decision := Decision{
			DecisionID: randomID("dcs"),
			SubjectID:  subjectID,
			FlagKey:    key,
			Value:      flag.DefaultValue,
			CreatedAt:  now,
		}
		if exp, has := selected[key]; has {
			variant, err := chooseVariant(subjectID, exp, s.stickinessSalt)
			if err == nil {
				decision.Value = variant.Value
				decision.ExperimentID = exp.ID
				decision.VariantName = variant.Name
				decision.ConfigVersion = exp.Version
			}
		}
		if err := s.store.PutDecision(decision); err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return decisions, nil
}

type EventBatchResult struct {
	Accepted  int      `json:"accepted"`
	Duplicate int      `json:"duplicate"`
	Rejected  int      `json:"rejected"`
	Errors    []string `json:"errors"`
}

func (s *Service) IngestEvents(events []Event) EventBatchResult {
	now := time.Now().UTC()
	out := EventBatchResult{Errors: []string{}}
	for _, e := range events {
		e.ReceivedAt = now
		if e.EventID == "" {
			out.Rejected++
			out.Errors = append(out.Errors, "event_id is required")
			continue
		}
		if e.OccurredAt.IsZero() {
			e.OccurredAt = now
		}
		if now.Sub(e.OccurredAt) > 7*24*time.Hour {
			out.Rejected++
			out.Errors = append(out.Errors, "event is older than 7 days")
			continue
		}
		et, ok, err := s.store.GetEventType(e.Type)
		if err != nil {
			out.Rejected++
			out.Errors = append(out.Errors, err.Error())
			continue
		}
		if !ok || et.Archived {
			out.Rejected++
			out.Errors = append(out.Errors, fmt.Sprintf("unknown or archived event type %q", e.Type))
			continue
		}
		if dup, err := s.store.SaveEvent(e); err != nil {
			out.Rejected++
			out.Errors = append(out.Errors, err.Error())
			continue
		} else if dup {
			existing, ok, err := s.store.GetEventByID(e.EventID)
			if err != nil {
				out.Rejected++
				out.Errors = append(out.Errors, err.Error())
				continue
			}
			if ok && !equivalentEvents(existing, e) {
				out.Rejected++
				out.Errors = append(out.Errors, fmt.Sprintf("duplicate event_id %s with different payload", e.EventID))
				continue
			}
			out.Duplicate++
			continue
		}
		if et.Key == "exposure" {
			_ = s.store.MarkExposed(e.DecisionID)
			s.tryAttribute(e, et)
			pendingEvents, _ := s.store.DrainPending(e.DecisionID)
			for _, pending := range pendingEvents {
				pt, _, _ := s.store.GetEventType(pending.Type)
				s.tryAttribute(pending, pt)
			}
			out.Accepted++
			continue
		}
		if et.RequiresExposure {
			d, ok, err := s.store.GetDecision(e.DecisionID)
			if err != nil {
				out.Rejected++
				out.Errors = append(out.Errors, err.Error())
				continue
			}
			if !ok {
				out.Rejected++
				out.Errors = append(out.Errors, fmt.Sprintf("decision %s not found", e.DecisionID))
				continue
			}
			if !d.Exposed {
				_ = s.store.PutPending(e.DecisionID, e)
				out.Accepted++
				continue
			}
		}
		s.tryAttribute(e, et)
		out.Accepted++
	}
	return out
}

func (s *Service) tryAttribute(e Event, et EventType) {
	if e.DecisionID == "" {
		return
	}
	d, ok, err := s.store.GetDecision(e.DecisionID)
	if err != nil {
		return
	}
	if !ok || d.ExperimentID == "" {
		return
	}
	if err := s.store.PutAttributed(AttributedEvent{
		EventID:      e.EventID,
		ExperimentID: d.ExperimentID,
		VariantName:  d.VariantName,
		Type:         e.Type,
		OccurredAt:   e.OccurredAt,
		Properties:   e.Properties,
	}); err == nil {
		s.enqueueGuardrailJob(d.ExperimentID, e.OccurredAt, "attributed_event")
	}
	_ = et
}

type VariantReport struct {
	VariantName string             `json:"variant_name"`
	Metrics     map[string]float64 `json:"metrics"`
}

type Report struct {
	ExperimentID string          `json:"experiment_id"`
	From         time.Time       `json:"from"`
	To           time.Time       `json:"to"`
	Variants     []VariantReport `json:"variants"`
}

func (s *Service) BuildReport(experimentID string, from, to time.Time) (Report, error) {
	exp, err := s.requireExperiment(experimentID)
	if err != nil {
		return Report{}, err
	}
	rows := map[string][]AttributedEvent{}
	for _, v := range exp.Variants {
		rows[v.Name] = []AttributedEvent{}
	}
	attrEvents, err := s.store.ListAttributed()
	if err != nil {
		return Report{}, err
	}
	for _, e := range attrEvents {
		if e.ExperimentID != experimentID {
			continue
		}
		if e.OccurredAt.Before(from) || !e.OccurredAt.Before(to) {
			continue
		}
		rows[e.VariantName] = append(rows[e.VariantName], e)
	}
	out := Report{
		ExperimentID: experimentID,
		From:         from,
		To:           to,
		Variants:     make([]VariantReport, 0, len(rows)),
	}
	for variant, events := range rows {
		impressions := 0.0
		conversions := 0.0
		errorsCount := 0.0
		latencies := []float64{}
		for _, e := range events {
			switch e.Type {
			case "exposure":
				impressions++
			case "conversion":
				conversions++
			case "error":
				errorsCount++
			case "latency":
				if ms, ok := toFloat(e.Properties["ms"]); ok {
					latencies = append(latencies, ms)
				}
			}
		}
		avgLatency, p95 := computeLatency(latencies)
		metrics := map[string]float64{
			"impressions":     impressions,
			"conversions":     conversions,
			"conversion_rate": safeDiv(conversions, impressions),
			"errors":          errorsCount,
			"error_rate":      safeDiv(errorsCount, impressions),
			"avg_latency":     avgLatency,
			"p95_latency":     p95,
		}
		out.Variants = append(out.Variants, VariantReport{VariantName: variant, Metrics: metrics})
	}
	return out, nil
}

func (s *Service) EvaluateGuardrails(now time.Time) {
	experiments, err := s.store.ListExperiments()
	if err != nil {
		return
	}
	for _, exp := range experiments {
		if exp.Status != StatusRunning {
			continue
		}
		for _, gr := range exp.Guardrails {
			windowStart := now.Add(-time.Duration(gr.WindowSeconds) * time.Second)
			rep, err := s.BuildReport(exp.ID, windowStart, now)
			if err != nil {
				continue
			}
			actual := guardrailMetric(rep, gr.MetricKey)
			if actual > gr.Threshold && gr.Action == GuardrailActionPause {
				exp.Status = StatusPaused
				exp.GuardrailLog = append(exp.GuardrailLog, GuardrailTrigger{
					MetricKey:     gr.MetricKey,
					Threshold:     gr.Threshold,
					WindowSeconds: gr.WindowSeconds,
					Action:        gr.Action,
					ActualValue:   actual,
					TriggeredAt:   now,
				})
				exp.UpdatedAt = now
				incidentKey := fmt.Sprintf("%s|%s|%s|%d", exp.ID, gr.MetricKey, gr.Action, now.Unix()/int64(gr.WindowSeconds))
				inserted, _ := s.store.AddGuardrailTrigger(exp.GuardrailLog[len(exp.GuardrailLog)-1], exp.ID, incidentKey)
				if !inserted {
					continue
				}
				_ = s.store.PutExperiment(exp)
				s.appendAudit("experiment", exp.ID, "guardrail_triggered", "system", map[string]interface{}{
					"metric":     gr.MetricKey,
					"threshold":  gr.Threshold,
					"actual":     actual,
					"action":     gr.Action,
					"incident":   incidentKey,
					"window_sec": gr.WindowSeconds,
				})
				break
			}
		}
	}
}

func (s *Service) EvaluateGuardrailForExperimentWindow(experimentID string, from, to time.Time) error {
	exp, err := s.requireExperiment(experimentID)
	if err != nil {
		return err
	}
	if exp.Status != StatusRunning {
		return nil
	}
	for _, gr := range exp.Guardrails {
		windowStart := to.Add(-time.Duration(gr.WindowSeconds) * time.Second)
		rep, err := s.BuildReport(exp.ID, windowStart, to)
		if err != nil {
			return err
		}
		actual := guardrailMetric(rep, gr.MetricKey)
		if actual <= gr.Threshold || gr.Action != GuardrailActionPause {
			continue
		}
		exp.Status = StatusPaused
		trigger := GuardrailTrigger{
			MetricKey:     gr.MetricKey,
			Threshold:     gr.Threshold,
			WindowSeconds: gr.WindowSeconds,
			Action:        gr.Action,
			ActualValue:   actual,
			TriggeredAt:   to,
		}
		exp.GuardrailLog = append(exp.GuardrailLog, trigger)
		exp.UpdatedAt = to
		incidentKey := fmt.Sprintf("%s|%s|%s|%d", exp.ID, gr.MetricKey, gr.Action, to.Unix()/int64(maxInt(gr.WindowSeconds, 1)))
		inserted, _ := s.store.AddGuardrailTrigger(trigger, exp.ID, incidentKey)
		if !inserted {
			continue
		}
		if err := s.store.PutExperiment(exp); err != nil {
			return err
		}
		s.appendAudit("experiment", exp.ID, "guardrail_triggered", "system", map[string]interface{}{
			"metric":     gr.MetricKey,
			"threshold":  gr.Threshold,
			"actual":     actual,
			"action":     gr.Action,
			"incident":   incidentKey,
			"window_sec": gr.WindowSeconds,
			"from":       from.Format(time.RFC3339),
			"to":         to.Format(time.RFC3339),
		})
		break
	}
	return nil
}

func (s *Service) ProcessGuardrailJobs(workerID string, batch int, now time.Time) (int, error) {
	jobs, err := s.store.ClaimGuardrailJobs(workerID, batch, now)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, job := range jobs {
		err := s.EvaluateGuardrailForExperimentWindow(job.ExperimentID, job.WindowFrom, job.WindowTo)
		if err != nil {
			delay := time.Duration(minInt(job.Attempts, 5)) * time.Second
			if delay < time.Second {
				delay = time.Second
			}
			_ = s.store.FailGuardrailJob(job.ID, now.Add(delay), err.Error())
			continue
		}
		_ = s.store.CompleteGuardrailJob(job.ID, now)
		processed++
	}
	return processed, nil
}

func (s *Service) ReplayAttribution(experimentID string, from, to time.Time) (int, error) {
	exp, err := s.requireExperiment(experimentID)
	if err != nil {
		return 0, err
	}
	if err := s.store.DeleteAttributedForExperimentInRange(experimentID, from, to); err != nil {
		return 0, err
	}
	rawEvents, err := s.store.ListRawEventsInRange(from, to)
	if err != nil {
		return 0, err
	}
	rebuilt := 0
	for _, e := range rawEvents {
		if e.DecisionID == "" {
			continue
		}
		decision, ok, err := s.store.GetDecision(e.DecisionID)
		if err != nil || !ok || decision.ExperimentID != experimentID {
			continue
		}
		if e.Type != "exposure" {
			et, found, err := s.store.GetEventType(e.Type)
			if err != nil || !found {
				continue
			}
			if et.RequiresExposure && !decision.Exposed {
				continue
			}
		}
		if err := s.store.PutAttributed(AttributedEvent{
			EventID:      e.EventID,
			ExperimentID: decision.ExperimentID,
			VariantName:  decision.VariantName,
			Type:         e.Type,
			OccurredAt:   e.OccurredAt,
			Properties:   e.Properties,
		}); err == nil {
			s.enqueueGuardrailJob(decision.ExperimentID, e.OccurredAt, "replay")
			rebuilt++
		}
	}
	s.appendAudit("experiment", exp.ID, "attribution_replayed", "system", map[string]interface{}{
		"from":    from.Format(time.RFC3339),
		"to":      to.Format(time.RFC3339),
		"rebuilt": rebuilt,
	})
	return rebuilt, nil
}

func (s *Service) requireExperiment(id string) (*Experiment, error) {
	exp, ok, err := s.store.GetExperiment(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("experiment not found")
	}
	return exp, nil
}

func validateVariants(audience int, variants []Variant) error {
	if audience <= 0 || audience > 100 {
		return errors.New("audience_percent must be within 1..100")
	}
	if len(variants) == 0 {
		return errors.New("at least one variant required")
	}
	sum := 0
	control := 0
	for _, v := range variants {
		if strings.TrimSpace(v.Name) == "" {
			return errors.New("variant name is required")
		}
		sum += v.Weight
		if v.IsControl {
			control++
		}
	}
	if sum != audience {
		return errors.New("sum of variant weights must match audience_percent")
	}
	if control != 1 {
		return errors.New("exactly one control variant is required")
	}
	return nil
}

func chooseVariant(subjectID string, exp *Experiment, salt string) (Variant, error) {
	if len(exp.Variants) == 0 {
		return Variant{}, errors.New("no variants configured")
	}
	score := int(hashFraction(subjectID+"|"+exp.ID+"|"+fmt.Sprintf("%d", exp.Version)+"|"+salt) * float64(exp.AudiencePercent))
	cursor := 0
	for _, v := range exp.Variants {
		cursor += v.Weight
		if score < cursor {
			return v, nil
		}
	}
	return exp.Variants[len(exp.Variants)-1], nil
}

func inAudience(subjectID string, audience int, expID, salt string) bool {
	score := int(hashFraction(subjectID+"|"+expID+"|audience|"+salt) * 100.0)
	return score < audience
}

func hashFraction(v string) float64 {
	sum := sha1.Sum([]byte(v))
	hexStr := hex.EncodeToString(sum[:8])
	var n uint64
	for _, ch := range hexStr {
		n = n*16 + uint64(strings.IndexByte("0123456789abcdef", byte(ch)))
	}
	return float64(n%10000) / 10000.0
}

func cloneVariants(in []Variant) []Variant {
	out := make([]Variant, len(in))
	copy(out, in)
	return out
}

func randomID(prefix string) string {
	base := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	sum := sha1.Sum([]byte(base))
	return prefix + "_" + hex.EncodeToString(sum[:])[:12]
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func computeLatency(values []float64) (avg float64, p95 float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	avg = sum / float64(len(values))
	sort.Float64s(values)
	idx := int(float64(len(values)-1) * 0.95)
	p95 = values[idx]
	return avg, p95
}

func guardrailMetric(report Report, metricKey string) float64 {
	var controlValue float64
	var maxDelta float64
	first := true
	for _, v := range report.Variants {
		if strings.EqualFold(v.VariantName, "A") || strings.EqualFold(v.VariantName, "control") {
			controlValue = v.Metrics[metricKey]
			first = false
			break
		}
	}
	if first && len(report.Variants) > 0 {
		controlValue = report.Variants[0].Metrics[metricKey]
	}
	for _, v := range report.Variants {
		delta := v.Metrics[metricKey] - controlValue
		if delta > maxDelta {
			maxDelta = delta
		}
	}
	return maxDelta
}

func (s *Service) appendAudit(entityType, entityID, action, actorID string, payload map[string]interface{}) {
	_ = s.store.AppendAudit(AuditLog{
		ID:         randomID("aud"),
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		ActorID:    actorID,
		Payload:    payload,
		CreatedAt:  time.Now().UTC(),
	})
}

func equivalentEvents(a, b Event) bool {
	return a.Type == b.Type &&
		a.SubjectID == b.SubjectID &&
		a.DecisionID == b.DecisionID &&
		occurredAtEquivalent(a.OccurredAt, b.OccurredAt) &&
		reflect.DeepEqual(normalizeMap(a.Properties), normalizeMap(b.Properties))
}

// occurredAtEquivalent compares instants at microsecond resolution so duplicate
// detection matches PostgreSQL timestamptz storage (sub-microsecond RFC3339Nano
// from the client still matches the row read back from the database).
func occurredAtEquivalent(a, b time.Time) bool {
	return a.UTC().Truncate(time.Microsecond).Equal(b.UTC().Truncate(time.Microsecond))
}

func normalizeMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	return in
}

func (s *Service) enqueueGuardrailJob(experimentID string, occurredAt time.Time, reason string) {
	bucketTime := occurredAt.UTC().Truncate(time.Minute)
	job := GuardrailJob{
		ID:           randomID("grj"),
		ExperimentID: experimentID,
		WindowFrom:   bucketTime,
		WindowTo:     bucketTime.Add(time.Minute),
		WindowBucket: bucketTime.Unix() / 60,
		Reason:       reason,
		Status:       GuardrailJobPending,
		Attempts:     0,
		AvailableAt:  time.Now().UTC(),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	_ = s.store.EnqueueGuardrailJob(job)
}

func (s *Service) HashBucket(input string, modulo int) int {
	if modulo <= 0 {
		return 0
	}
	return int(hashFraction(input) * float64(modulo))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sameJSON(a, b json.RawMessage) bool {
	var va interface{}
	var vb interface{}
	if err := json.Unmarshal(a, &va); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}
