package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"VK_AB_Lotty_task/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.seed(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

func (s *Store) PutFlag(flag domain.Flag) error {
	_, err := s.pool.Exec(context.Background(), `
insert into flags (key, type, default_value, description, owner, created_at, updated_at)
values ($1,$2,$3,$4,$5,$6,$7)
on conflict (key) do update set
  type=excluded.type,
  default_value=excluded.default_value,
  description=excluded.description,
  owner=excluded.owner,
  updated_at=excluded.updated_at
`, flag.Key, flag.Type, flag.DefaultValue, flag.Description, flag.Owner, flag.CreatedAt, flag.UpdatedAt)
	return err
}

func (s *Store) GetFlag(key string) (domain.Flag, bool, error) {
	row := s.pool.QueryRow(context.Background(), `
select key, type, default_value, description, owner, created_at, updated_at
from flags where key=$1
`, key)
	var f domain.Flag
	var t string
	if err := row.Scan(&f.Key, &t, &f.DefaultValue, &f.Description, &f.Owner, &f.CreatedAt, &f.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Flag{}, false, nil
		}
		return domain.Flag{}, false, err
	}
	f.Type = domain.ValueType(t)
	return f, true, nil
}

func (s *Store) ListFlags() ([]domain.Flag, error) {
	rows, err := s.pool.Query(context.Background(), `
select key, type, default_value, description, owner, created_at, updated_at
from flags order by key
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Flag{}
	for rows.Next() {
		var f domain.Flag
		var t string
		if err := rows.Scan(&f.Key, &t, &f.DefaultValue, &f.Description, &f.Owner, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		f.Type = domain.ValueType(t)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) FindFlagByOwnerAndKey(owner, key string) (domain.Flag, bool, error) {
	_ = owner
	return s.GetFlag(key)
}

func (s *Store) PutExperiment(exp *domain.Experiment) error {
	targeting, _ := json.Marshal(exp.Targeting)
	variants, _ := json.Marshal(exp.Variants)
	guardrails, _ := json.Marshal(exp.Guardrails)
	versions, _ := json.Marshal(exp.Versions)
	reviewHistory, _ := json.Marshal(exp.ReviewHistory)
	guardrailLog, _ := json.Marshal(exp.GuardrailLog)
	_, err := s.pool.Exec(context.Background(), `
insert into experiments (
  id, flag_key, name, owner_id, status, version, audience_percent, targeting, variants,
  guardrails, versions, review_history, guardrail_log, decision_comment, created_at, updated_at
)
values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
on conflict (id) do update set
  flag_key=excluded.flag_key,
  name=excluded.name,
  owner_id=excluded.owner_id,
  status=excluded.status,
  version=excluded.version,
  audience_percent=excluded.audience_percent,
  targeting=excluded.targeting,
  variants=excluded.variants,
  guardrails=excluded.guardrails,
  versions=excluded.versions,
  review_history=excluded.review_history,
  guardrail_log=excluded.guardrail_log,
  decision_comment=excluded.decision_comment,
  updated_at=excluded.updated_at
`,
		exp.ID, exp.FlagKey, exp.Name, exp.OwnerID, exp.Status, exp.Version, exp.AudiencePercent,
		targeting, variants, guardrails, versions, reviewHistory, guardrailLog,
		exp.DecisionComment, exp.CreatedAt, exp.UpdatedAt,
	)
	return err
}

func (s *Store) GetExperiment(id string) (*domain.Experiment, bool, error) {
	row := s.pool.QueryRow(context.Background(), `
select id, flag_key, name, owner_id, status, version, audience_percent, targeting, variants,
       guardrails, versions, review_history, guardrail_log, decision_comment, created_at, updated_at
from experiments where id=$1
`, id)
	exp, err := scanExperiment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return exp, true, nil
}

func (s *Store) ListExperiments() ([]*domain.Experiment, error) {
	rows, err := s.pool.Query(context.Background(), `
select id, flag_key, name, owner_id, status, version, audience_percent, targeting, variants,
       guardrails, versions, review_history, guardrail_log, decision_comment, created_at, updated_at
from experiments order by created_at desc
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Experiment{}
	for rows.Next() {
		exp, err := scanExperiment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, exp)
	}
	return out, rows.Err()
}

func (s *Store) FindActiveExperimentByFlag(flagKey string) (*domain.Experiment, bool, error) {
	row := s.pool.QueryRow(context.Background(), `
select id, flag_key, name, owner_id, status, version, audience_percent, targeting, variants,
       guardrails, versions, review_history, guardrail_log, decision_comment, created_at, updated_at
from experiments
where flag_key=$1 and status in ('running','paused')
order by updated_at desc
limit 1
`, flagKey)
	exp, err := scanExperiment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return exp, true, nil
}

func (s *Store) FindExperimentByOwnerAndName(ownerID, name string) (*domain.Experiment, bool, error) {
	row := s.pool.QueryRow(context.Background(), `
select id, flag_key, name, owner_id, status, version, audience_percent, targeting, variants,
       guardrails, versions, review_history, guardrail_log, decision_comment, created_at, updated_at
from experiments where owner_id=$1 and name=$2
order by created_at desc
limit 1
`, ownerID, name)
	exp, err := scanExperiment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return exp, true, nil
}

func (s *Store) AppendExperimentVersion(experimentID string, version domain.ExperimentVersion) error {
	exp, ok, err := s.GetExperiment(experimentID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("experiment not found")
	}
	exp.Version = version.Version
	exp.Versions = append(exp.Versions, version)
	exp.UpdatedAt = version.UpdatedAt
	return s.PutExperiment(exp)
}

func (s *Store) PutUser(user domain.User) error {
	_, err := s.pool.Exec(context.Background(), `
insert into users (id, role) values ($1,$2)
on conflict (id) do update set role=excluded.role
`, user.ID, user.Role)
	return err
}

func (s *Store) GetUser(id string) (domain.User, bool, error) {
	row := s.pool.QueryRow(context.Background(), `select id, role from users where id=$1`, id)
	var u domain.User
	var role string
	if err := row.Scan(&u.ID, &role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, false, nil
		}
		return domain.User{}, false, err
	}
	u.Role = domain.Role(role)
	return u, true, nil
}

func (s *Store) ListUsersByRoles(roles ...domain.Role) ([]domain.User, error) {
	roleStrings := make([]string, 0, len(roles))
	for _, r := range roles {
		roleStrings = append(roleStrings, string(r))
	}
	rows, err := s.pool.Query(context.Background(), `select id, role from users where role = any($1)`, roleStrings)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.User{}
	for rows.Next() {
		var u domain.User
		var role string
		if err := rows.Scan(&u.ID, &role); err != nil {
			return nil, err
		}
		u.Role = domain.Role(role)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) PutApproverPolicy(policy domain.ApproverPolicy) error {
	ids, _ := json.Marshal(policy.ApproverIDs)
	_, err := s.pool.Exec(context.Background(), `
insert into approver_policies (experimenter_id, approver_ids, min_approvals)
values ($1,$2,$3)
on conflict (experimenter_id) do update set
  approver_ids=excluded.approver_ids,
  min_approvals=excluded.min_approvals
`, policy.ExperimenterID, ids, policy.MinApprovals)
	return err
}

func (s *Store) ResolveApproverPolicy(experimenterID string) (domain.ApproverPolicy, error) {
	row := s.pool.QueryRow(context.Background(), `
select approver_ids, min_approvals from approver_policies where experimenter_id=$1
`, experimenterID)
	var idsRaw []byte
	var minApprovals int
	if err := row.Scan(&idsRaw, &minApprovals); err == nil && minApprovals > 0 {
		var ids []string
		_ = json.Unmarshal(idsRaw, &ids)
		return domain.ApproverPolicy{
			ExperimenterID: experimenterID,
			ApproverIDs:    ids,
			MinApprovals:   minApprovals,
		}, nil
	}
	users, err := s.ListUsersByRoles(domain.RoleAdmin, domain.RoleApprover)
	if err != nil {
		return domain.ApproverPolicy{}, err
	}
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	return domain.ApproverPolicy{
		ExperimenterID: experimenterID,
		ApproverIDs:    ids,
		MinApprovals:   1,
	}, nil
}

func (s *Store) PutDecision(d domain.Decision) error {
	_, err := s.pool.Exec(context.Background(), `
insert into decisions (decision_id, subject_id, flag_key, value, experiment_id, variant_name, config_version, created_at, exposed)
values ($1,$2,$3,$4,$5,$6,$7,$8,$9)
on conflict (decision_id) do update set
  value=excluded.value,
  experiment_id=excluded.experiment_id,
  variant_name=excluded.variant_name,
  config_version=excluded.config_version,
  exposed=excluded.exposed
`, d.DecisionID, d.SubjectID, d.FlagKey, d.Value, d.ExperimentID, d.VariantName, d.ConfigVersion, d.CreatedAt, d.Exposed)
	return err
}

func (s *Store) GetDecision(id string) (domain.Decision, bool, error) {
	row := s.pool.QueryRow(context.Background(), `
select decision_id, subject_id, flag_key, value, experiment_id, variant_name, config_version, created_at, exposed
from decisions where decision_id=$1
`, id)
	var d domain.Decision
	if err := row.Scan(&d.DecisionID, &d.SubjectID, &d.FlagKey, &d.Value, &d.ExperimentID, &d.VariantName, &d.ConfigVersion, &d.CreatedAt, &d.Exposed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Decision{}, false, nil
		}
		return domain.Decision{}, false, err
	}
	return d, true, nil
}

func (s *Store) MarkExposed(decisionID string) error {
	_, err := s.pool.Exec(context.Background(), `update decisions set exposed=true where decision_id=$1`, decisionID)
	return err
}

func (s *Store) PutEventType(t domain.EventType) error {
	_, err := s.pool.Exec(context.Background(), `
insert into event_types (key, description, requires_exposure, archived)
values ($1,$2,$3,$4)
on conflict (key) do update set
  description=excluded.description,
  requires_exposure=excluded.requires_exposure,
  archived=excluded.archived
`, t.Key, t.Description, t.RequiresExposure, t.Archived)
	return err
}

func (s *Store) GetEventType(key string) (domain.EventType, bool, error) {
	row := s.pool.QueryRow(context.Background(), `
select key, description, requires_exposure, archived from event_types where key=$1
`, key)
	var t domain.EventType
	if err := row.Scan(&t.Key, &t.Description, &t.RequiresExposure, &t.Archived); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.EventType{}, false, nil
		}
		return domain.EventType{}, false, err
	}
	return t, true, nil
}

func (s *Store) ListEventTypes() ([]domain.EventType, error) {
	rows, err := s.pool.Query(context.Background(), `select key, description, requires_exposure, archived from event_types order by key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.EventType{}
	for rows.Next() {
		var t domain.EventType
		if err := rows.Scan(&t.Key, &t.Description, &t.RequiresExposure, &t.Archived); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SaveEvent(e domain.Event) (bool, error) {
	props, _ := json.Marshal(e.Properties)
	_, err := s.pool.Exec(context.Background(), `
insert into events (event_id, type, subject_id, decision_id, occurred_at, received_at, properties)
values ($1,$2,$3,$4,$5,$6,$7)
`, e.EventID, e.Type, e.SubjectID, e.DecisionID, e.OccurredAt, e.ReceivedAt, props)
	if err != nil {
		if isUniqueViolation(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func (s *Store) GetEventByID(eventID string) (domain.Event, bool, error) {
	row := s.pool.QueryRow(context.Background(), `
select event_id, type, subject_id, decision_id, occurred_at, received_at, properties
from events where event_id=$1
`, eventID)
	var e domain.Event
	var props []byte
	if err := row.Scan(&e.EventID, &e.Type, &e.SubjectID, &e.DecisionID, &e.OccurredAt, &e.ReceivedAt, &props); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Event{}, false, nil
		}
		return domain.Event{}, false, err
	}
	if len(props) > 0 {
		_ = json.Unmarshal(props, &e.Properties)
	}
	return e, true, nil
}

func (s *Store) PutPending(decisionID string, e domain.Event) error {
	payload, _ := json.Marshal(e)
	_, err := s.pool.Exec(context.Background(), `
insert into pending_attribution (decision_id, event_id, payload, created_at)
values ($1,$2,$3,$4)
on conflict (event_id) do nothing
`, decisionID, e.EventID, payload, time.Now().UTC())
	return err
}

func (s *Store) DrainPending(decisionID string) ([]domain.Event, error) {
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	rows, err := tx.Query(context.Background(), `
select payload from pending_attribution where decision_id=$1
`, decisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Event{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var e domain.Event
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if _, err := tx.Exec(context.Background(), `delete from pending_attribution where decision_id=$1`, decisionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) PutAttributed(a domain.AttributedEvent) error {
	props, _ := json.Marshal(a.Properties)
	_, err := s.pool.Exec(context.Background(), `
insert into attributed_events (event_id, experiment_id, variant_name, type, occurred_at, properties)
values ($1,$2,$3,$4,$5,$6)
on conflict (event_id) do nothing
`, a.EventID, a.ExperimentID, a.VariantName, a.Type, a.OccurredAt, props)
	return err
}

func (s *Store) ListAttributed() ([]domain.AttributedEvent, error) {
	rows, err := s.pool.Query(context.Background(), `
select event_id, experiment_id, variant_name, type, occurred_at, properties
from attributed_events
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AttributedEvent{}
	for rows.Next() {
		var e domain.AttributedEvent
		var props []byte
		if err := rows.Scan(&e.EventID, &e.ExperimentID, &e.VariantName, &e.Type, &e.OccurredAt, &props); err != nil {
			return nil, err
		}
		if len(props) > 0 {
			_ = json.Unmarshal(props, &e.Properties)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAttributedForExperimentInRange(experimentID string, from, to time.Time) error {
	_, err := s.pool.Exec(context.Background(), `
delete from attributed_events
where experiment_id=$1 and occurred_at >= $2 and occurred_at < $3
`, experimentID, from, to)
	return err
}

func (s *Store) ListRawEventsInRange(from, to time.Time) ([]domain.Event, error) {
	rows, err := s.pool.Query(context.Background(), `
select event_id, type, subject_id, decision_id, occurred_at, received_at, properties
from events
where occurred_at >= $1 and occurred_at < $2
order by occurred_at asc
`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Event{}
	for rows.Next() {
		var e domain.Event
		var props []byte
		if err := rows.Scan(&e.EventID, &e.Type, &e.SubjectID, &e.DecisionID, &e.OccurredAt, &e.ReceivedAt, &props); err != nil {
			return nil, err
		}
		if len(props) > 0 {
			_ = json.Unmarshal(props, &e.Properties)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AppendAudit(log domain.AuditLog) error {
	payload, _ := json.Marshal(log.Payload)
	_, err := s.pool.Exec(context.Background(), `
insert into audit_logs (id, entity_type, entity_id, action, actor_id, payload, created_at)
values ($1,$2,$3,$4,$5,$6,$7)
`, log.ID, log.EntityType, log.EntityID, log.Action, log.ActorID, payload, log.CreatedAt)
	return err
}

func (s *Store) ListAudit(entityType, entityID string) ([]domain.AuditLog, error) {
	rows, err := s.pool.Query(context.Background(), `
select id, entity_type, entity_id, action, actor_id, payload, created_at
from audit_logs
where entity_type=$1 and entity_id=$2
order by created_at asc
`, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AuditLog{}
	for rows.Next() {
		var a domain.AuditLog
		var payload []byte
		if err := rows.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.Action, &a.ActorID, &payload, &a.CreatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &a.Payload)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AddGuardrailTrigger(trigger domain.GuardrailTrigger, experimentID string, incidentKey string) (bool, error) {
	_, err := s.pool.Exec(context.Background(), `
insert into guardrail_triggers (
  experiment_id, incident_key, metric_key, threshold, window_seconds, action, actual_value, triggered_at
)
values ($1,$2,$3,$4,$5,$6,$7,$8)
`, experimentID, incidentKey, trigger.MetricKey, trigger.Threshold, trigger.WindowSeconds, trigger.Action, trigger.ActualValue, trigger.TriggeredAt)
	if err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Store) EnqueueGuardrailJob(job domain.GuardrailJob) error {
	_, err := s.pool.Exec(context.Background(), `
insert into guardrail_jobs (
  id, experiment_id, window_from, window_to, window_bucket, reason, status, attempts,
  available_at, locked_at, locked_by, last_error, created_at, updated_at
)
values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
`,
		job.ID, job.ExperimentID, job.WindowFrom, job.WindowTo, job.WindowBucket, job.Reason,
		job.Status, job.Attempts, job.AvailableAt, job.LockedAt, job.LockedBy, job.LastError,
		job.CreatedAt, job.UpdatedAt,
	)
	if err != nil && isUniqueViolation(err) {
		return nil
	}
	return err
}

func (s *Store) ClaimGuardrailJobs(workerID string, limit int, now time.Time) ([]domain.GuardrailJob, error) {
	if limit <= 0 {
		limit = 10
	}
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	rows, err := tx.Query(context.Background(), `
select id
from guardrail_jobs
where status='pending' and available_at <= $1
order by created_at
for update skip locked
limit $2
`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		if err := tx.Commit(context.Background()); err != nil {
			return nil, err
		}
		return []domain.GuardrailJob{}, nil
	}

	_, err = tx.Exec(context.Background(), `
update guardrail_jobs
set status='running',
    attempts=attempts+1,
    locked_at=$1,
    locked_by=$2,
    updated_at=$1
where id = any($3)
`, now, workerID, ids)
	if err != nil {
		return nil, err
	}

	claimedRows, err := tx.Query(context.Background(), `
select id, experiment_id, window_from, window_to, window_bucket, reason, status, attempts, available_at,
       locked_at, locked_by, coalesce(last_error,''), created_at, updated_at
from guardrail_jobs
where id = any($1)
order by created_at
`, ids)
	if err != nil {
		return nil, err
	}
	defer claimedRows.Close()

	out := []domain.GuardrailJob{}
	for claimedRows.Next() {
		job, err := scanGuardrailJob(claimedRows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	if err := tx.Commit(context.Background()); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) CompleteGuardrailJob(jobID string, finishedAt time.Time) error {
	_, err := s.pool.Exec(context.Background(), `
update guardrail_jobs
set status='completed',
    updated_at=$2
where id=$1
`, jobID, finishedAt)
	return err
}

func (s *Store) FailGuardrailJob(jobID string, retryAt time.Time, lastError string) error {
	_, err := s.pool.Exec(context.Background(), `
update guardrail_jobs
set status=case when attempts >= 5 then 'failed' else 'pending' end,
    available_at=case when attempts >= 5 then available_at else $2 end,
    last_error=$3,
    updated_at=$4
where id=$1
`, jobID, retryAt, lastError, time.Now().UTC())
	return err
}

func (s *Store) TryAcquireLeader(lockKey, workerID string, ttl time.Duration, now time.Time) (bool, error) {
	result, err := s.pool.Exec(context.Background(), `
insert into worker_leases (lock_key, worker_id, lease_until, updated_at)
values ($1,$2,$3,$4)
on conflict (lock_key) do update
set worker_id = excluded.worker_id,
    lease_until = excluded.lease_until,
    updated_at = excluded.updated_at
where worker_leases.lease_until < $4 or worker_leases.worker_id = $2
`, lockKey, workerID, now.Add(ttl), now)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (s *Store) ReleaseLeader(lockKey, workerID string) error {
	_, err := s.pool.Exec(context.Background(), `
delete from worker_leases where lock_key=$1 and worker_id=$2
`, lockKey, workerID)
	return err
}

func (s *Store) seed(ctx context.Context) error {
	seeds := []string{
		`insert into users (id, role) values ('admin','admin') on conflict (id) do nothing`,
		`insert into users (id, role) values ('exp','experimenter') on conflict (id) do nothing`,
		`insert into users (id, role) values ('approver','approver') on conflict (id) do nothing`,
		`insert into users (id, role) values ('viewer','viewer') on conflict (id) do nothing`,
		`insert into approver_policies (experimenter_id, approver_ids, min_approvals)
		 values ('exp','["approver"]',1) on conflict (experimenter_id) do nothing`,
		`insert into event_types (key, description, requires_exposure, archived)
		 values ('exposure','Fact of shown variant',false,false) on conflict (key) do nothing`,
		`insert into event_types (key, description, requires_exposure, archived)
		 values ('conversion','Target action',true,false) on conflict (key) do nothing`,
		`insert into event_types (key, description, requires_exposure, archived)
		 values ('error','Error happened',true,false) on conflict (key) do nothing`,
		`insert into event_types (key, description, requires_exposure, archived)
		 values ('latency','Latency sample',true,false) on conflict (key) do nothing`,
	}
	for _, q := range seeds {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("seed failed: %w", err)
		}
	}
	return nil
}

func scanExperiment(row interface {
	Scan(dest ...interface{}) error
}) (*domain.Experiment, error) {
	var exp domain.Experiment
	var status string
	var targeting, variants, guardrails, versions, reviewHistory, guardrailLog []byte
	if err := row.Scan(
		&exp.ID, &exp.FlagKey, &exp.Name, &exp.OwnerID, &status, &exp.Version, &exp.AudiencePercent,
		&targeting, &variants, &guardrails, &versions, &reviewHistory, &guardrailLog,
		&exp.DecisionComment, &exp.CreatedAt, &exp.UpdatedAt,
	); err != nil {
		return nil, err
	}
	exp.Status = domain.ExperimentStatus(status)
	if len(targeting) > 0 {
		var rule domain.RuleNode
		if err := json.Unmarshal(targeting, &rule); err == nil {
			exp.Targeting = &rule
		}
	}
	_ = json.Unmarshal(variants, &exp.Variants)
	_ = json.Unmarshal(guardrails, &exp.Guardrails)
	_ = json.Unmarshal(versions, &exp.Versions)
	_ = json.Unmarshal(reviewHistory, &exp.ReviewHistory)
	_ = json.Unmarshal(guardrailLog, &exp.GuardrailLog)
	return &exp, nil
}

func scanGuardrailJob(row interface {
	Scan(dest ...interface{}) error
}) (domain.GuardrailJob, error) {
	var job domain.GuardrailJob
	var status string
	var lockedAt *time.Time
	if err := row.Scan(
		&job.ID, &job.ExperimentID, &job.WindowFrom, &job.WindowTo, &job.WindowBucket,
		&job.Reason, &status, &job.Attempts, &job.AvailableAt, &lockedAt, &job.LockedBy,
		&job.LastError, &job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		return domain.GuardrailJob{}, err
	}
	job.Status = domain.GuardrailJobStatus(status)
	job.LockedAt = lockedAt
	return job, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
