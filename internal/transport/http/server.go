package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"VK_AB_Lotty_task/internal/app"
	"VK_AB_Lotty_task/internal/domain"
)

type Server struct {
	app *app.App
	log *slog.Logger
}

func New(app *app.App, log *slog.Logger) *Server {
	return &Server{app: app, log: log}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)

	mux.HandleFunc("/v1/flags", s.handleFlags)
	mux.HandleFunc("/v1/flags/", s.handleFlagByKey)

	mux.HandleFunc("/v1/experiments", s.handleExperiments)
	mux.HandleFunc("/v1/experiments/", s.handleExperimentAction)

	mux.HandleFunc("/v1/decide", s.handleDecide)
	mux.HandleFunc("/v1/events/batch", s.handleEventsBatch)
	mux.HandleFunc("/v1/reports/", s.handleReportByExperiment)

	mux.HandleFunc("/v1/admin/users", s.handleUsers)
	mux.HandleFunc("/v1/admin/approver-policy", s.handleApproverPolicy)
	mux.HandleFunc("/v1/admin/event-types", s.handleEventTypes)
	mux.HandleFunc("/v1/admin/replay", s.handleReplay)
	mux.HandleFunc("/v1/admin/audit/", s.handleAuditByEntity)
	return s.withJSON(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if !s.app.IsReady() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "starting"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleFlags(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		flags, err := s.app.Store.ListFlags()
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, flags)
	case http.MethodPost:
		actor, err := s.actor(r)
		if err != nil {
			s.writeError(w, r, http.StatusUnauthorized, err)
			return
		}
		var req domain.Flag
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, err)
			return
		}
		idemKey := r.Header.Get("Idempotency-Key")
		flag, err := s.app.Service.CreateFlagIdempotent(actor, req, idemKey)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, flag)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, nil)
	}
}

func (s *Server) handleFlagByKey(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/flags/")
	if strings.HasSuffix(path, "/default") && r.Method == http.MethodPatch {
		key := strings.TrimSuffix(path, "/default")
		actor, err := s.actor(r)
		if err != nil {
			s.writeError(w, r, http.StatusUnauthorized, err)
			return
		}
		var body struct {
			DefaultValue json.RawMessage `json:"default_value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.writeError(w, r, http.StatusBadRequest, err)
			return
		}
		flag, err := s.app.Service.UpdateFlagDefault(actor, key, body.DefaultValue)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, flag)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, nil)
		return
	}
	flag, ok, err := s.app.Store.GetFlag(path)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		s.writeError(w, r, http.StatusNotFound, errors.New("flag not found"))
		return
	}
	writeJSON(w, http.StatusOK, flag)
}

func (s *Server) handleExperiments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		experiments, err := s.app.Store.ListExperiments()
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, experiments)
	case http.MethodPost:
		actor, err := s.actor(r)
		if err != nil {
			s.writeError(w, r, http.StatusUnauthorized, err)
			return
		}
		var req domain.Experiment
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, err)
			return
		}
		idemKey := r.Header.Get("Idempotency-Key")
		exp, err := s.app.Service.CreateExperimentIdempotent(actor, req, idemKey)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, exp)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, nil)
	}
}

func (s *Server) handleExperimentAction(w http.ResponseWriter, r *http.Request) {
	actor, err := s.actor(r)
	if err != nil {
		s.writeError(w, r, http.StatusUnauthorized, err)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/experiments/")
	parts := strings.Split(path, "/")
	if len(parts) == 1 && r.Method == http.MethodGet {
		exp, ok, err := s.app.Store.GetExperiment(parts[0])
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		if !ok {
			s.writeError(w, r, http.StatusNotFound, errors.New("experiment not found"))
			return
		}
		writeJSON(w, http.StatusOK, exp)
		return
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, nil)
		return
	}
	expID, action := parts[0], parts[1]
	switch action {
	case "update":
		var req domain.ExperimentConfigUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, err)
			return
		}
		exp, err := s.app.Service.UpdateExperimentConfig(actor, expID, req)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, exp)
	case "submit-review":
		exp, err := s.app.Service.SubmitForReview(actor, expID)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, exp)
	case "approve":
		exp, err := s.app.Service.Review(actor, expID, domain.ReviewApprove, reqComment(r))
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, exp)
	case "request-changes":
		exp, err := s.app.Service.Review(actor, expID, domain.ReviewRequestChange, reqComment(r))
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, exp)
	case "reject":
		exp, err := s.app.Service.Review(actor, expID, domain.ReviewReject, reqComment(r))
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, exp)
	case "start":
		exp, err := s.app.Service.StartExperiment(actor, expID)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, exp)
	case "pause":
		exp, err := s.app.Service.PauseExperiment(actor, expID)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, exp)
	case "resume":
		exp, err := s.app.Service.ResumeExperiment(actor, expID)
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, exp)
	case "complete":
		exp, err := s.app.Service.CompleteExperiment(actor, expID, reqComment(r))
		if err != nil {
			s.writeDomainError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, exp)
	default:
		s.writeError(w, r, http.StatusNotFound, errors.New("unsupported action"))
	}
}

func (s *Server) handleDecide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, nil)
		return
	}
	var req struct {
		SubjectID  string                 `json:"subject_id"`
		Attributes map[string]interface{} `json:"attributes"`
		FlagKeys   []string               `json:"flag_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)
		return
	}
	decisions, err := s.app.Service.Decide(req.SubjectID, req.Attributes, req.FlagKeys)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"decisions": decisions})
}

func (s *Server) handleEventsBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, nil)
		return
	}
	var req struct {
		Events []domain.Event `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)
		return
	}
	out := s.app.Service.IngestEvents(req.Events)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleReportByExperiment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, nil)
		return
	}
	expID := strings.TrimPrefix(r.URL.Path, "/v1/reports/")
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	if qs := r.URL.Query().Get("from"); qs != "" {
		if v, err := time.Parse(time.RFC3339, qs); err == nil {
			from = v
		}
	}
	if qs := r.URL.Query().Get("to"); qs != "" {
		if v, err := time.Parse(time.RFC3339, qs); err == nil {
			to = v
		}
	}
	report, err := s.app.Service.BuildReport(expID, from, to)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, nil)
		return
	}
	actor, err := s.actor(r)
	if err != nil {
		s.writeError(w, r, http.StatusUnauthorized, err)
		return
	}
	if actor.Role != domain.RoleAdmin {
		s.writeError(w, r, http.StatusForbidden, errors.New("forbidden"))
		return
	}
	var req struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)
		return
	}
	role, err := domain.ParseRole(req.Role)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)
		return
	}
	u := domain.User{ID: req.ID, Role: role}
	if err := s.app.Store.PutUser(u); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleApproverPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, nil)
		return
	}
	actor, err := s.actor(r)
	if err != nil {
		s.writeError(w, r, http.StatusUnauthorized, err)
		return
	}
	if actor.Role != domain.RoleAdmin {
		s.writeError(w, r, http.StatusForbidden, errors.New("forbidden"))
		return
	}
	var req domain.ApproverPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)
		return
	}
	if req.MinApprovals <= 0 {
		req.MinApprovals = 1
	}
	if err := s.app.Store.PutApproverPolicy(req); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

func (s *Server) handleEventTypes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		eventTypes, err := s.app.Store.ListEventTypes()
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, eventTypes)
	case http.MethodPost:
		actor, err := s.actor(r)
		if err != nil {
			s.writeError(w, r, http.StatusUnauthorized, err)
			return
		}
		if actor.Role != domain.RoleAdmin {
			s.writeError(w, r, http.StatusForbidden, errors.New("forbidden"))
			return
		}
		var req domain.EventType
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, err)
			return
		}
		if req.Key == "" {
			s.writeError(w, r, http.StatusBadRequest, errors.New("key is required"))
			return
		}
		if err := s.app.Store.PutEventType(req); err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, req)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, nil)
	}
}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, nil)
		return
	}
	actor, err := s.actor(r)
	if err != nil {
		s.writeError(w, r, http.StatusUnauthorized, err)
		return
	}
	if actor.Role != domain.RoleAdmin {
		s.writeError(w, r, http.StatusForbidden, errors.New("forbidden"))
		return
	}
	var req struct {
		ExperimentID string `json:"experiment_id"`
		From         string `json:"from"`
		To           string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err)
		return
	}
	from, err := time.Parse(time.RFC3339, req.From)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, errors.New("invalid from"))
		return
	}
	to, err := time.Parse(time.RFC3339, req.To)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, errors.New("invalid to"))
		return
	}
	rebuilt, err := s.app.Service.ReplayAttribution(req.ExperimentID, from, to)
	if err != nil {
		s.writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"rebuilt": rebuilt})
}

func (s *Server) handleAuditByEntity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, nil)
		return
	}
	actor, err := s.actor(r)
	if err != nil {
		s.writeError(w, r, http.StatusUnauthorized, err)
		return
	}
	if actor.Role != domain.RoleAdmin && actor.Role != domain.RoleViewer {
		s.writeError(w, r, http.StatusForbidden, errors.New("forbidden"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/admin/audit/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		s.writeError(w, r, http.StatusBadRequest, errors.New("expected /v1/admin/audit/{entityType}/{entityID}"))
		return
	}
	logs, err := s.app.Store.ListAudit(parts[0], parts[1])
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) actor(r *http.Request) (domain.User, error) {
	id := r.Header.Get("X-User-ID")
	if id == "" {
		id = "viewer"
	}
	roleHeader := r.Header.Get("X-Role")
	if roleHeader != "" {
		role, err := domain.ParseRole(roleHeader)
		if err != nil {
			return domain.User{}, err
		}
		return domain.User{ID: id, Role: role}, nil
	}
	u, ok, err := s.app.Store.GetUser(id)
	if err != nil {
		return domain.User{}, err
	}
	if !ok {
		return domain.User{}, errors.New("unknown user")
	}
	return u, nil
}

func (s *Server) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	if strings.Contains(err.Error(), "forbidden") {
		s.writeError(w, r, http.StatusForbidden, err)
		return
	}
	if strings.Contains(err.Error(), "not found") {
		s.writeError(w, r, http.StatusNotFound, err)
		return
	}
	s.writeError(w, r, http.StatusBadRequest, err)
}

func reqComment(r *http.Request) string {
	var body struct {
		Comment string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	return body.Comment
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, err error) {
	if status >= 500 {
		s.log.Error("request error response",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"error", err.Error(),
		)
	} else {
		s.log.Warn("request error response",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"error", err.Error(),
		)
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
		)
		next.ServeHTTP(w, r)
	})
}
