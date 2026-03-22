package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"VK_AB_Lotty_task/internal/domain"
	"VK_AB_Lotty_task/internal/infrastructure/db/postgres"
)

type App struct {
	Service              *domain.Service
	Store                domain.Store
	Logger               *slog.Logger
	GuardrailTick        time.Duration
	WorkerEnabled        bool
	WorkerPoll           time.Duration
	WorkerBatchSize      int
	LeaderLockKey        string
	LeaderLeaseTTL       time.Duration
	FallbackScanInterval time.Duration
}

func New(logger *slog.Logger) (*App, error) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "postgres://lotty:lotty@localhost:5433/lotty?sslmode=disable"
	}
	store, err := postgres.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	service := domain.NewService(store, "lotty-salt-v1", 2)
	guardrailTick := envIntSeconds("GUARDRAIL_TICK_SECONDS", 10)
	workerPollMS := envIntMillis("GUARDRAIL_WORKER_POLL_MS", 1000)
	fallbackScanSeconds := envIntSeconds("GUARDRAIL_FALLBACK_SCAN_SECONDS", 300)
	return &App{
		Service:              service,
		Store:                store,
		Logger:               logger,
		GuardrailTick:        time.Duration(guardrailTick) * time.Second,
		WorkerEnabled:        envBool("GUARDRAIL_WORKER_ENABLED", true),
		WorkerPoll:           time.Duration(workerPollMS) * time.Millisecond,
		WorkerBatchSize:      envInt("GUARDRAIL_WORKER_BATCH_SIZE", 50),
		LeaderLockKey:        envString("GUARDRAIL_LEADER_LOCK_KEY", "guardrail_worker"),
		LeaderLeaseTTL:       time.Duration(envIntSeconds("GUARDRAIL_LEADER_TTL_SECONDS", 15)) * time.Second,
		FallbackScanInterval: time.Duration(fallbackScanSeconds) * time.Second,
	}, nil
}

func (a *App) IsReady() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return a.Store.Ping(ctx) == nil
}

func (a *App) StartBackground(ctx context.Context) {
	if !a.WorkerEnabled {
		ticker := time.NewTicker(a.GuardrailTick)
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					_ = a.Store.Close()
					return
				case now := <-ticker.C:
					a.Service.EvaluateGuardrails(now.UTC())
				}
			}
		}()
		return
	}

	pollTicker := time.NewTicker(a.WorkerPoll)
	fallbackTicker := time.NewTicker(a.FallbackScanInterval)
	workerID := fmt.Sprintf("worker-%d", time.Now().UnixNano())
	go func() {
		defer pollTicker.Stop()
		defer fallbackTicker.Stop()
		leader := false
		for {
			select {
			case <-ctx.Done():
				_ = a.Store.ReleaseLeader(a.LeaderLockKey, workerID)
				_ = a.Store.Close()
				return
			case now := <-pollTicker.C:
				acquired, err := a.Store.TryAcquireLeader(a.LeaderLockKey, workerID, a.LeaderLeaseTTL, now.UTC())
				if err != nil || !acquired {
					leader = false
					continue
				}
				leader = true
				_, _ = a.Service.ProcessGuardrailJobs(workerID, a.WorkerBatchSize, now.UTC())
			case now := <-fallbackTicker.C:
				if leader {
					a.Service.EvaluateGuardrails(now.UTC())
				}
			}
		}
	}()
}

func envIntSeconds(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func envIntMillis(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func envBool(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func envString(key, fallback string) string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	return raw
}
