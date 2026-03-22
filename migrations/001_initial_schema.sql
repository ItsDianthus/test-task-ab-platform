-- Full schema for PostgreSQL runtime.
-- This migration is executed manually via Makefile (`make migrate`).

create table if not exists flags (
  key text primary key,
  type text not null,
  default_value jsonb not null,
  description text,
  owner text,
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create table if not exists users (
  id text primary key,
  role text not null
);

create table if not exists approver_policies (
  experimenter_id text primary key references users(id),
  approver_ids jsonb not null,
  min_approvals int not null check (min_approvals > 0)
);

create table if not exists event_types (
  key text primary key,
  description text not null,
  requires_exposure boolean not null default false,
  archived boolean not null default false
);

create table if not exists experiments (
  id text primary key,
  flag_key text not null references flags(key),
  name text not null,
  owner_id text not null,
  status text not null,
  version int not null,
  audience_percent int not null,
  targeting jsonb,
  variants jsonb not null,
  guardrails jsonb not null default '[]'::jsonb,
  versions jsonb not null default '[]'::jsonb,
  review_history jsonb not null default '[]'::jsonb,
  guardrail_log jsonb not null default '[]'::jsonb,
  decision_comment text,
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create unique index if not exists uq_experiments_owner_name
  on experiments(owner_id, name);

-- Enforce at most one running/paused experiment per flag.
create unique index if not exists uq_experiments_active_flag
  on experiments(flag_key)
  where status in ('running', 'paused');

create table if not exists decisions (
  decision_id text primary key,
  subject_id text not null,
  flag_key text not null,
  value jsonb not null,
  experiment_id text,
  variant_name text,
  config_version int,
  created_at timestamptz not null,
  exposed bool not null default false
);

create table if not exists events (
  event_id text primary key,
  type text not null,
  subject_id text not null,
  decision_id text,
  occurred_at timestamptz not null,
  received_at timestamptz not null,
  properties jsonb
);

create table if not exists pending_attribution (
  decision_id text not null,
  event_id text primary key,
  payload jsonb not null,
  created_at timestamptz not null
);

create table if not exists attributed_events (
  id bigserial primary key,
  event_id text not null unique,
  experiment_id text not null references experiments(id),
  variant_name text not null,
  type text not null,
  occurred_at timestamptz not null,
  properties jsonb
);

create table if not exists guardrail_triggers (
  id bigserial primary key,
  experiment_id text not null references experiments(id),
  incident_key text not null unique,
  metric_key text not null,
  threshold double precision not null,
  window_seconds int not null,
  action text not null,
  actual_value double precision not null,
  triggered_at timestamptz not null
);

alter table if exists guardrail_triggers
  add column if not exists incident_key text;

update guardrail_triggers
set incident_key = concat(
  experiment_id, '|', metric_key, '|', action, '|', extract(epoch from triggered_at)::bigint
)
where incident_key is null;

create unique index if not exists uq_guardrail_triggers_incident_key
  on guardrail_triggers(incident_key);

create table if not exists guardrail_jobs (
  id text primary key,
  experiment_id text not null references experiments(id),
  window_from timestamptz not null,
  window_to timestamptz not null,
  window_bucket bigint not null,
  reason text not null,
  status text not null,
  attempts int not null default 0,
  available_at timestamptz not null,
  locked_at timestamptz,
  locked_by text,
  last_error text,
  created_at timestamptz not null,
  updated_at timestamptz not null
);

create unique index if not exists uq_guardrail_jobs_idempotent
  on guardrail_jobs(experiment_id, window_bucket, reason)
  where status in ('pending', 'running');

create index if not exists idx_guardrail_jobs_claim
  on guardrail_jobs(status, available_at);

create index if not exists idx_guardrail_jobs_experiment_time
  on guardrail_jobs(experiment_id, created_at);

create table if not exists worker_leases (
  lock_key text primary key,
  worker_id text not null,
  lease_until timestamptz not null,
  updated_at timestamptz not null
);

create table if not exists audit_logs (
  id text primary key,
  entity_type text not null,
  entity_id text not null,
  action text not null,
  actor_id text not null,
  payload jsonb,
  created_at timestamptz not null
);

create index if not exists idx_attributed_events_exp_time
  on attributed_events(experiment_id, occurred_at);

create index if not exists idx_audit_entity_time
  on audit_logs(entity_type, entity_id, created_at);
