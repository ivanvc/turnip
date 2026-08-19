package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecCommand_Success(t *testing.T) {
	stdout, _, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo hello"})
	if err != nil {
		t.Fatalf("execCommand returned unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
	if got := strings.TrimSpace(string(stdout)); got != "hello" {
		t.Errorf("stdout = %q, want %q", got, "hello")
	}
}

func TestExecCommand_NonZeroExit(t *testing.T) {
	stdout, stderr, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo out; echo err >&2; exit 3"})
	if err != nil {
		t.Fatalf("execCommand returned unexpected error for a completed-but-failed command: %v", err)
	}
	if exitCode != 3 {
		t.Errorf("exitCode = %d, want 3", exitCode)
	}
	if got := strings.TrimSpace(string(stdout)); got != "out" {
		t.Errorf("stdout = %q, want %q", got, "out")
	}
	if got := strings.TrimSpace(string(stderr)); got != "err" {
		t.Errorf("stderr = %q, want %q", got, "err")
	}
}

func TestExecCommand_BinaryNotFound(t *testing.T) {
	_, _, exitCode, err := execCommand(context.Background(), "", "turnip-nonexistent-binary-xyz", nil)
	if err == nil {
		t.Fatal("execCommand returned nil error for a nonexistent binary")
	}
	if exitCode != -1 {
		t.Errorf("exitCode = %d, want -1", exitCode)
	}
}

func TestExecCommand_RespectsWorkingDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to write marker file: %v", err)
	}

	stdout, _, exitCode, err := execCommand(context.Background(), dir, "ls", nil)
	if err != nil {
		t.Fatalf("execCommand returned unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
	if !strings.Contains(string(stdout), "marker.txt") {
		t.Errorf("stdout = %q, want it to contain marker.txt (dir not respected)", stdout)
	}
}
