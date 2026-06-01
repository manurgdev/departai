package agent

import (
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
