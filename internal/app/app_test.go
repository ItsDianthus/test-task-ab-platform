package app

import (
	"os"
	"testing"
)

func TestEnvIntSeconds(t *testing.T) {
	t.Setenv("TEST_SEC", "15")
	if got := envIntSeconds("TEST_SEC", 5); got != 15 {
		t.Fatalf("expected 15, got %d", got)
	}
	t.Setenv("TEST_SEC", "bad")
	if got := envIntSeconds("TEST_SEC", 5); got != 5 {
		t.Fatalf("expected fallback 5, got %d", got)
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("TEST_BOOL", "true")
	if !envBool("TEST_BOOL", false) {
		t.Fatalf("expected true from envBool")
	}
	t.Setenv("TEST_BOOL", "0")
	if envBool("TEST_BOOL", true) {
		t.Fatalf("expected false from envBool")
	}
}

func TestEnvStringAndInt(t *testing.T) {
	t.Setenv("TEST_STR", " value ")
	if got := envString("TEST_STR", "fallback"); got != "value" {
		t.Fatalf("expected trimmed value, got %q", got)
	}
	t.Setenv("TEST_INT", "9")
	if got := envInt("TEST_INT", 3); got != 9 {
		t.Fatalf("expected 9, got %d", got)
	}
}

func TestEnvIntMillisFallbackOnMissing(t *testing.T) {
	_ = os.Unsetenv("TEST_MS")
	if got := envIntMillis("TEST_MS", 250); got != 250 {
		t.Fatalf("expected fallback 250, got %d", got)
	}
}
