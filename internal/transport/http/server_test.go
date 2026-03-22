package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"VK_AB_Lotty_task/internal/app"
	"VK_AB_Lotty_task/internal/domain"
)

func newTestHTTPServer(t *testing.T) http.Handler {
	t.Helper()
	store := domain.NewMemoryStore()
	svc := domain.NewService(store, "test-salt", 2)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &app.App{
		Service: svc,
		Store:   store,
		Logger:  logger,
	}
	return New(a, logger).Handler()
}

func TestHealthEndpoint(t *testing.T) {
	h := newTestHTTPServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestReadyEndpoint(t *testing.T) {
	h := newTestHTTPServer(t)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAdminUsersForbiddenForNonAdmin(t *testing.T) {
	h := newTestHTTPServer(t)
	body := bytes.NewBufferString(`{"id":"u-new","role":"viewer"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/users", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "viewer")
	req.Header.Set("X-Role", "viewer")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestAdminUsersCreateSuccess(t *testing.T) {
	h := newTestHTTPServer(t)
	body := bytes.NewBufferString(`{"id":"u-new","role":"viewer"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/users", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "admin")
	req.Header.Set("X-Role", "admin")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got domain.User
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != "u-new" {
		t.Fatalf("expected created user id u-new, got %s", got.ID)
	}
}

func TestAuditEndpointRequiresAdminOrViewer(t *testing.T) {
	h := newTestHTTPServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/audit/experiment/exp1", nil)
	req.Header.Set("X-User-ID", "exp")
	req.Header.Set("X-Role", "experimenter")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}
