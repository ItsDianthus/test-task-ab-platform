package main

import (
	"log/slog"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"INFO", slog.LevelInfo},
		{"", slog.LevelInfo},
	}
	for _, tt := range tests {
		if got := parseLogLevel(tt.in); got != tt.want {
			t.Fatalf("parseLogLevel(%q)=%v want %v", tt.in, got, tt.want)
		}
	}
}
