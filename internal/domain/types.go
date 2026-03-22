package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Role string

const (
	RoleAdmin        Role = "admin"
	RoleExperimenter Role = "experimenter"
	RoleApprover     Role = "approver"
	RoleViewer       Role = "viewer"
)

func ParseRole(v string) (Role, error) {
	role := Role(strings.ToLower(strings.TrimSpace(v)))
	switch role {
	case RoleAdmin, RoleExperimenter, RoleApprover, RoleViewer:
		return role, nil
	default:
		return "", fmt.Errorf("unknown role %q", v)
	}
}

type ValueType string

const (
	ValueTypeString ValueType = "string"
	ValueTypeNumber ValueType = "number"
	ValueTypeBool   ValueType = "bool"
)

func (t ValueType) ValidateValue(raw json.RawMessage) error {
	switch t {
	case ValueTypeString:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return errors.New("value must be string")
		}
	case ValueTypeNumber:
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil {
			return errors.New("value must be number")
		}
	case ValueTypeBool:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return errors.New("value must be bool")
		}
	default:
		return fmt.Errorf("unsupported value type %q", t)
	}
	return nil
}

type Flag struct {
	Key          string          `json:"key"`
	Type         ValueType       `json:"type"`
	DefaultValue json.RawMessage `json:"default_value"`
	Description  string          `json:"description,omitempty"`
	Owner        string          `json:"owner,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type ExperimentStatus string

const (
	StatusDraft     ExperimentStatus = "draft"
	StatusInReview  ExperimentStatus = "in_review"
	StatusApproved  ExperimentStatus = "approved"
	StatusRunning   ExperimentStatus = "running"
	StatusPaused    ExperimentStatus = "paused"
	StatusCompleted ExperimentStatus = "completed"
	StatusArchived  ExperimentStatus = "archived"
	StatusRejected  ExperimentStatus = "rejected"
)

type Variant struct {
	Name      string          `json:"name"`
	Value     json.RawMessage `json:"value"`
	Weight    int             `json:"weight"`
	IsControl bool            `json:"is_control"`
}

type RuleNode struct {
	Op       string        `json:"op"`
	Field    string        `json:"field,omitempty"`
	Value    interface{}   `json:"value,omitempty"`
	Values   []interface{} `json:"values,omitempty"`
	Children []RuleNode    `json:"children,omitempty"`
}

type GuardrailAction string

const (
	GuardrailActionPause GuardrailAction = "pause"
)

type GuardrailRule struct {
	MetricKey     string          `json:"metric_key"`
	Threshold     float64         `json:"threshold"`
	WindowSeconds int             `json:"window_seconds"`
	Action        GuardrailAction `json:"action"`
}

type ExperimentVersion struct {
	Version         int       `json:"version"`
	AudiencePercent int       `json:"audience_percent"`
	Variants        []Variant `json:"variants"`
	Targeting       *RuleNode `json:"targeting,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	UpdatedBy       string    `json:"updated_by"`
}

type ReviewDecision string

const (
	ReviewApprove       ReviewDecision = "approve"
	ReviewRequestChange ReviewDecision = "request_changes"
	ReviewReject        ReviewDecision = "reject"
)

type ReviewEntry struct {
	ExperimentID string         `json:"experiment_id"`
	ReviewerID   string         `json:"reviewer_id"`
	Decision     ReviewDecision `json:"decision"`
	Comment      string         `json:"comment"`
	CreatedAt    time.Time      `json:"created_at"`
}

type GuardrailTrigger struct {
	MetricKey     string          `json:"metric_key"`
	Threshold     float64         `json:"threshold"`
	WindowSeconds int             `json:"window_seconds"`
	Action        GuardrailAction `json:"action"`
	ActualValue   float64         `json:"actual_value"`
	TriggeredAt   time.Time       `json:"triggered_at"`
}

type Experiment struct {
	ID              string              `json:"id"`
	FlagKey         string              `json:"flag_key"`
	Name            string              `json:"name"`
	OwnerID         string              `json:"owner_id"`
	Status          ExperimentStatus    `json:"status"`
	Version         int                 `json:"version"`
	AudiencePercent int                 `json:"audience_percent"`
	Variants        []Variant           `json:"variants"`
	Targeting       *RuleNode           `json:"targeting,omitempty"`
	Guardrails      []GuardrailRule     `json:"guardrails,omitempty"`
	Versions        []ExperimentVersion `json:"versions"`
	ReviewHistory   []ReviewEntry       `json:"review_history,omitempty"`
	GuardrailLog    []GuardrailTrigger  `json:"guardrail_log,omitempty"`
	DecisionComment string              `json:"decision_comment,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

func (e *Experiment) Frozen() bool {
	return e.Status == StatusRunning || e.Status == StatusPaused
}

type ExperimentConfigUpdate struct {
	AudiencePercent int             `json:"audience_percent"`
	Variants        []Variant       `json:"variants"`
	Targeting       *RuleNode       `json:"targeting,omitempty"`
	Guardrails      []GuardrailRule `json:"guardrails,omitempty"`
}

type ApproverPolicy struct {
	ExperimenterID string   `json:"experimenter_id"`
	ApproverIDs    []string `json:"approver_ids"`
	MinApprovals   int      `json:"min_approvals"`
}

type User struct {
	ID   string `json:"id"`
	Role Role   `json:"role"`
}

type Decision struct {
	DecisionID    string          `json:"decision_id"`
	SubjectID     string          `json:"subject_id"`
	FlagKey       string          `json:"flag_key"`
	Value         json.RawMessage `json:"value"`
	ExperimentID  string          `json:"experiment_id,omitempty"`
	VariantName   string          `json:"variant_name,omitempty"`
	ConfigVersion int             `json:"config_version,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	Exposed       bool            `json:"-"`
}

type EventType struct {
	Key              string `json:"key"`
	Description      string `json:"description"`
	RequiresExposure bool   `json:"requires_exposure"`
	Archived         bool   `json:"archived"`
}

type Event struct {
	EventID     string                 `json:"event_id"`
	Type        string                 `json:"type"`
	SubjectID   string                 `json:"subject_id"`
	DecisionID  string                 `json:"decision_id,omitempty"`
	OccurredAt  time.Time              `json:"occurred_at"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
	ReceivedAt  time.Time              `json:"received_at"`
	Attributed  bool                   `json:"attributed"`
	Rejected    bool                   `json:"rejected"`
	RejectError string                 `json:"reject_error,omitempty"`
}

type AttributedEvent struct {
	EventID      string                 `json:"event_id"`
	ExperimentID string                 `json:"experiment_id"`
	VariantName  string                 `json:"variant_name"`
	Type         string                 `json:"type"`
	OccurredAt   time.Time              `json:"occurred_at"`
	Properties   map[string]interface{} `json:"properties,omitempty"`
}

type MetricDefinition struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type GuardrailJobStatus string

const (
	GuardrailJobPending   GuardrailJobStatus = "pending"
	GuardrailJobRunning   GuardrailJobStatus = "running"
	GuardrailJobCompleted GuardrailJobStatus = "completed"
	GuardrailJobFailed    GuardrailJobStatus = "failed"
)

type GuardrailJob struct {
	ID           string             `json:"id"`
	ExperimentID string             `json:"experiment_id"`
	WindowFrom   time.Time          `json:"window_from"`
	WindowTo     time.Time          `json:"window_to"`
	WindowBucket int64              `json:"window_bucket"`
	Reason       string             `json:"reason"`
	Status       GuardrailJobStatus `json:"status"`
	Attempts     int                `json:"attempts"`
	AvailableAt  time.Time          `json:"available_at"`
	LockedAt     *time.Time         `json:"locked_at,omitempty"`
	LockedBy     string             `json:"locked_by,omitempty"`
	LastError    string             `json:"last_error,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type AuditLog struct {
	ID         string                 `json:"id"`
	EntityType string                 `json:"entity_type"`
	EntityID   string                 `json:"entity_id"`
	Action     string                 `json:"action"`
	ActorID    string                 `json:"actor_id"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}
