package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckCLIFound(t *testing.T) {
	// Drop a fake executable into a temp dir and prepend it to PATH.
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakecli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing fake binary: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := CheckCLI("fakecli", "should not be shown"); err != nil {
		t.Errorf("expected nil for an installed binary, got %v", err)
	}
}

func TestCheckCLIMissing(t *testing.T) {
	err := CheckCLI("departai-nonexistent-binary-xyz", "Install it: do the thing")
	if err == nil {
		t.Fatal("expected an error for a missing binary, got nil")
	}
	if !strings.Contains(err.Error(), "Install it: do the thing") {
		t.Errorf("error should include the install hint, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error should mention it's not installed, got: %v", err)
	}
}

func TestStreamBufferMB(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv(streamBufferEnvVar, "")
		if got := StreamBufferMB(); got != defaultMaxStreamLineMB {
			t.Errorf("StreamBufferMB() = %d, want default %d", got, defaultMaxStreamLineMB)
		}
	})
	t.Run("env override", func(t *testing.T) {
		t.Setenv(streamBufferEnvVar, "64")
		if got := StreamBufferMB(); got != 64 {
			t.Errorf("StreamBufferMB() = %d, want 64", got)
		}
		if got := StreamBufferBytes(); got != 64*1024*1024 {
			t.Errorf("StreamBufferBytes() = %d, want %d", got, 64*1024*1024)
		}
	})
	t.Run("invalid env falls back to default", func(t *testing.T) {
		for _, v := range []string{"abc", "0", "-5"} {
			t.Setenv(streamBufferEnvVar, v)
			if got := StreamBufferMB(); got != defaultMaxStreamLineMB {
				t.Errorf("StreamBufferMB() with %q = %d, want default", v, got)
			}
		}
	})
}

func TestStreamReadError(t *testing.T) {
	if err := StreamReadError("Agent Alpha", nil); err != nil {
		t.Errorf("nil error should map to nil, got %v", err)
	}

	tooLong := StreamReadError("Agent Alpha", bufio.ErrTooLong)
	if tooLong == nil || !strings.Contains(tooLong.Error(), streamBufferEnvVar) {
		t.Errorf("ErrTooLong should produce an actionable hint, got %v", tooLong)
	}
	if !strings.Contains(tooLong.Error(), "Agent Alpha") {
		t.Errorf("error should name the agent, got %v", tooLong)
	}

	other := StreamReadError("Agent Beta", os.ErrClosed)
	if other == nil || !strings.Contains(other.Error(), "reading stream output") {
		t.Errorf("generic error should be wrapped, got %v", other)
	}
}
