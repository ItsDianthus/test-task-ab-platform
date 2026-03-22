package domain

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Store interface {
	Ping(ctx context.Context) error
	Close() error

	PutFlag(flag Flag) error
	GetFlag(key string) (Flag, bool, error)
	ListFlags() ([]Flag, error)
	FindFlagByOwnerAndKey(owner, key string) (Flag, bool, error)

	PutExperiment(exp *Experiment) error
	GetExperiment(id string) (*Experiment, bool, error)
	ListExperiments() ([]*Experiment, error)
	FindActiveExperimentByFlag(flagKey string) (*Experiment, bool, error)
	FindExperimentByOwnerAndName(ownerID, name string) (*Experiment, bool, error)
	AppendExperimentVersion(experimentID string, version ExperimentVersion) error

	PutUser(user User) error
	GetUser(id string) (User, bool, error)
	ListUsersByRoles(roles ...Role) ([]User, error)

	PutApproverPolicy(policy ApproverPolicy) error
	ResolveApproverPolicy(experimenterID string) (ApproverPolicy, error)

	PutDecision(d Decision) error
	GetDecision(id string) (Decision, bool, error)
	MarkExposed(decisionID string) error

	PutEventType(t EventType) error
	GetEventType(key string) (EventType, bool, error)
	ListEventTypes() ([]EventType, error)

	SaveEvent(e Event) (duplicate bool, err error)
	GetEventByID(eventID string) (Event, bool, error)
	PutPending(decisionID string, e Event) error
	DrainPending(decisionID string) ([]Event, error)
	PutAttributed(a AttributedEvent) error
	ListAttributed() ([]AttributedEvent, error)
	DeleteAttributedForExperimentInRange(experimentID string, from, to time.Time) error
	ListRawEventsInRange(from, to time.Time) ([]Event, error)

	AppendAudit(log AuditLog) error
	ListAudit(entityType, entityID string) ([]AuditLog, error)
	AddGuardrailTrigger(trigger GuardrailTrigger, experimentID string, incidentKey string) (bool, error)

	EnqueueGuardrailJob(job GuardrailJob) error
	ClaimGuardrailJobs(workerID string, limit int, now time.Time) ([]GuardrailJob, error)
	CompleteGuardrailJob(jobID string, finishedAt time.Time) error
	FailGuardrailJob(jobID string, retryAt time.Time, lastError string) error
	TryAcquireLeader(lockKey, workerID string, ttl time.Duration, now time.Time) (bool, error)
	ReleaseLeader(lockKey, workerID string) error
}

type MemoryStore struct {
	mu sync.RWMutex

	flags             map[string]Flag
	experiments       map[string]*Experiment
	users             map[string]User
	approverPolicies  map[string]ApproverPolicy
	eventTypes        map[string]EventType
	eventsByID        map[string]Event
	decisionsByID     map[string]Decision
	pendingByDecision map[string][]Event
	attributed        []AttributedEvent
	auditLogs         []AuditLog
	incidentKeys      map[string]bool
	guardrailJobs     map[string]GuardrailJob
	leaseOwners       map[string]GuardrailLease
}

type GuardrailLease struct {
	WorkerID   string
	LeaseUntil time.Time
}

func NewStore() Store {
	return NewMemoryStore()
}

func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{
		flags:             map[string]Flag{},
		experiments:       map[string]*Experiment{},
		users:             map[string]User{},
		approverPolicies:  map[string]ApproverPolicy{},
		eventTypes:        map[string]EventType{},
		eventsByID:        map[string]Event{},
		decisionsByID:     map[string]Decision{},
		pendingByDecision: map[string][]Event{},
		attributed:        []AttributedEvent{},
		auditLogs:         []AuditLog{},
		incidentKeys:      map[string]bool{},
		guardrailJobs:     map[string]GuardrailJob{},
		leaseOwners:       map[string]GuardrailLease{},
	}
	s.users["admin"] = User{ID: "admin", Role: RoleAdmin}
	s.users["exp"] = User{ID: "exp", Role: RoleExperimenter}
	s.users["approver"] = User{ID: "approver", Role: RoleApprover}
	s.users["viewer"] = User{ID: "viewer", Role: RoleViewer}
	s.approverPolicies["exp"] = ApproverPolicy{
		ExperimenterID: "exp",
		ApproverIDs:    []string{"approver"},
		MinApprovals:   1,
	}
	s.eventTypes["exposure"] = EventType{
		Key:              "exposure",
		Description:      "Fact of shown variant",
		RequiresExposure: false,
	}
	s.eventTypes["conversion"] = EventType{
		Key:              "conversion",
		Description:      "Target action",
		RequiresExposure: true,
	}
	s.eventTypes["error"] = EventType{
		Key:              "error",
		Description:      "Error happened",
		RequiresExposure: true,
	}
	s.eventTypes["latency"] = EventType{
		Key:              "latency",
		Description:      "Latency sample",
		RequiresExposure: true,
	}
	return s
}

func (s *MemoryStore) Ping(_ context.Context) error {
	return nil
}

func (s *MemoryStore) Close() error {
	return nil
}

func (s *MemoryStore) PutFlag(flag Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flags[flag.Key] = flag
	return nil
}

func (s *MemoryStore) GetFlag(key string) (Flag, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.flags[key]
	return v, ok, nil
}

func (s *MemoryStore) ListFlags() ([]Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Flag, 0, len(s.flags))
	for _, f := range s.flags {
		out = append(out, f)
	}
	return out, nil
}

func (s *MemoryStore) FindFlagByOwnerAndKey(owner, key string) (Flag, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flags[key]
	if !ok {
		return Flag{}, false, nil
	}
	if owner != "" && f.Owner != "" && f.Owner != owner {
		return Flag{}, false, nil
	}
	return f, true, nil
}

func (s *MemoryStore) PutExperiment(exp *Experiment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.experiments[exp.ID] = exp
	return nil
}

func (s *MemoryStore) GetExperiment(id string) (*Experiment, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.experiments[id]
	return exp, ok, nil
}

func (s *MemoryStore) ListExperiments() ([]*Experiment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Experiment, 0, len(s.experiments))
	for _, e := range s.experiments {
		out = append(out, e)
	}
	return out, nil
}

func (s *MemoryStore) FindActiveExperimentByFlag(flagKey string) (*Experiment, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.experiments {
		if e.FlagKey == flagKey && (e.Status == StatusRunning || e.Status == StatusPaused) {
			return e, true, nil
		}
	}
	return nil, false, nil
}

func (s *MemoryStore) FindExperimentByOwnerAndName(ownerID, name string) (*Experiment, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.experiments {
		if e.OwnerID == ownerID && e.Name == name {
			return e, true, nil
		}
	}
	return nil, false, nil
}

func (s *MemoryStore) AppendExperimentVersion(experimentID string, version ExperimentVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.experiments[experimentID]
	if !ok {
		return errors.New("experiment not found")
	}
	e.Versions = append(e.Versions, version)
	e.Version = version.Version
	s.experiments[experimentID] = e
	return nil
}

func (s *MemoryStore) PutUser(user User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[user.ID] = user
	return nil
}

func (s *MemoryStore) GetUser(id string) (User, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok, nil
}

func (s *MemoryStore) ListUsersByRoles(roles ...Role) ([]User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	allowed := map[Role]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	out := make([]User, 0)
	for _, u := range s.users {
		if allowed[u.Role] {
			out = append(out, u)
		}
	}
	return out, nil
}

func (s *MemoryStore) PutApproverPolicy(policy ApproverPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approverPolicies[policy.ExperimenterID] = policy
	return nil
}

func (s *MemoryStore) ResolveApproverPolicy(experimenterID string) (ApproverPolicy, error) {
	s.mu.RLock()
	policy, ok := s.approverPolicies[experimenterID]
	s.mu.RUnlock()
	if ok && policy.MinApprovals > 0 {
		return policy, nil
	}
	approvers, _ := s.ListUsersByRoles(RoleAdmin, RoleApprover)
	ids := make([]string, 0, len(approvers))
	for _, u := range approvers {
		ids = append(ids, u.ID)
	}
	return ApproverPolicy{
		ExperimenterID: experimenterID,
		ApproverIDs:    ids,
		MinApprovals:   1,
	}, nil
}

func (s *MemoryStore) PutDecision(d Decision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decisionsByID[d.DecisionID] = d
	return nil
}

func (s *MemoryStore) GetDecision(id string) (Decision, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.decisionsByID[id]
	return d, ok, nil
}

func (s *MemoryStore) MarkExposed(decisionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.decisionsByID[decisionID]
	if !ok {
		return errors.New("decision not found")
	}
	d.Exposed = true
	s.decisionsByID[decisionID] = d
	return nil
}

func (s *MemoryStore) PutEventType(t EventType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventTypes[t.Key] = t
	return nil
}

func (s *MemoryStore) GetEventType(key string) (EventType, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.eventTypes[key]
	return t, ok, nil
}

func (s *MemoryStore) ListEventTypes() ([]EventType, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EventType, 0, len(s.eventTypes))
	for _, t := range s.eventTypes {
		out = append(out, t)
	}
	return out, nil
}

func (s *MemoryStore) SaveEvent(e Event) (duplicate bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.eventsByID[e.EventID]; ok {
		return true, nil
	}
	s.eventsByID[e.EventID] = e
	return false, nil
}

func (s *MemoryStore) GetEventByID(eventID string) (Event, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.eventsByID[eventID]
	return e, ok, nil
}

func (s *MemoryStore) PutPending(decisionID string, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingByDecision[decisionID] = append(s.pendingByDecision[decisionID], e)
	return nil
}

func (s *MemoryStore) DrainPending(decisionID string) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.pendingByDecision[decisionID]
	delete(s.pendingByDecision, decisionID)
	return list, nil
}

func (s *MemoryStore) PutAttributed(a AttributedEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attributed = append(s.attributed, a)
	return nil
}

func (s *MemoryStore) ListAttributed() ([]AttributedEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AttributedEvent, len(s.attributed))
	copy(out, s.attributed)
	return out, nil
}

func (s *MemoryStore) DeleteAttributedForExperimentInRange(experimentID string, from, to time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := make([]AttributedEvent, 0, len(s.attributed))
	for _, a := range s.attributed {
		if a.ExperimentID == experimentID && !a.OccurredAt.Before(from) && a.OccurredAt.Before(to) {
			continue
		}
		filtered = append(filtered, a)
	}
	s.attributed = filtered
	return nil
}

func (s *MemoryStore) ListRawEventsInRange(from, to time.Time) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Event{}
	for _, e := range s.eventsByID {
		if e.OccurredAt.Before(from) || !e.OccurredAt.Before(to) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *MemoryStore) AppendAudit(log AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditLogs = append(s.auditLogs, log)
	return nil
}

func (s *MemoryStore) ListAudit(entityType, entityID string) ([]AuditLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []AuditLog{}
	for _, a := range s.auditLogs {
		if a.EntityType == entityType && a.EntityID == entityID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *MemoryStore) AddGuardrailTrigger(trigger GuardrailTrigger, _ string, incidentKey string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.incidentKeys[incidentKey] {
		return false, nil
	}
	s.incidentKeys[incidentKey] = true
	_ = trigger
	return true, nil
}

func (s *MemoryStore) EnqueueGuardrailJob(job GuardrailJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.guardrailJobs {
		if existing.ExperimentID == job.ExperimentID &&
			existing.WindowBucket == job.WindowBucket &&
			existing.Reason == job.Reason &&
			(existing.Status == GuardrailJobPending || existing.Status == GuardrailJobRunning) {
			return nil
		}
	}
	s.guardrailJobs[job.ID] = job
	return nil
}

func (s *MemoryStore) ClaimGuardrailJobs(workerID string, limit int, now time.Time) ([]GuardrailJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 10
	}
	out := make([]GuardrailJob, 0, limit)
	for id, job := range s.guardrailJobs {
		if len(out) >= limit {
			break
		}
		if job.Status != GuardrailJobPending || job.AvailableAt.After(now) {
			continue
		}
		job.Status = GuardrailJobRunning
		job.Attempts++
		job.LockedBy = workerID
		lockTime := now
		job.LockedAt = &lockTime
		job.UpdatedAt = now
		s.guardrailJobs[id] = job
		out = append(out, job)
	}
	return out, nil
}

func (s *MemoryStore) CompleteGuardrailJob(jobID string, finishedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.guardrailJobs[jobID]
	if !ok {
		return errors.New("guardrail job not found")
	}
	job.Status = GuardrailJobCompleted
	job.UpdatedAt = finishedAt
	s.guardrailJobs[jobID] = job
	return nil
}

func (s *MemoryStore) FailGuardrailJob(jobID string, retryAt time.Time, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.guardrailJobs[jobID]
	if !ok {
		return errors.New("guardrail job not found")
	}
	if job.Attempts >= 5 {
		job.Status = GuardrailJobFailed
	} else {
		job.Status = GuardrailJobPending
		job.AvailableAt = retryAt
	}
	job.LastError = lastError
	job.UpdatedAt = time.Now().UTC()
	s.guardrailJobs[jobID] = job
	return nil
}

func (s *MemoryStore) TryAcquireLeader(lockKey, workerID string, ttl time.Duration, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leaseOwners[lockKey]
	if !ok || current.LeaseUntil.Before(now) || current.WorkerID == workerID {
		s.leaseOwners[lockKey] = GuardrailLease{
			WorkerID:   workerID,
			LeaseUntil: now.Add(ttl),
		}
		return true, nil
	}
	return false, nil
}

func (s *MemoryStore) ReleaseLeader(lockKey, workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.leaseOwners[lockKey]
	if !ok {
		return nil
	}
	if current.WorkerID == workerID {
		delete(s.leaseOwners, lockKey)
	}
	return nil
}
