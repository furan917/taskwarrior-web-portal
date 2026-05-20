package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

// makeExe writes a minimal executable shell script to path and returns path.
func makeExe(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	return path
}

// makeNonExe writes a file at path with no execute bit.
func makeNonExe(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

// isolatedEnv clears BUGWARRIOR_BIN, sets PATH to an empty temp dir (so PATH
// lookup fails), and prevents the probe list from matching anything in HOME.
// Returns a restore function.
func isolatedEnv(t *testing.T) {
	t.Helper()
	empty := t.TempDir()
	t.Setenv("BUGWARRIOR_BIN", "")
	t.Setenv("PATH", empty)
	t.Setenv("HOME", empty)
}

// TestFindBugwarrior_EnvVar_Valid confirms BUGWARRIOR_BIN takes precedence
// when it points to a valid executable.
func TestFindBugwarrior_EnvVar_Valid(t *testing.T) {
	isolatedEnv(t)
	bin := makeExe(t, filepath.Join(t.TempDir(), "my-bugwarrior"))
	t.Setenv("BUGWARRIOR_BIN", bin)

	got := findBugwarrior()
	if got != bin {
		t.Errorf("got %q want %q", got, bin)
	}
}

// TestFindBugwarrior_EnvVar_NotExecutable confirms a non-executable
// BUGWARRIOR_BIN is ignored and the search falls through to PATH.
func TestFindBugwarrior_EnvVar_NotExecutable(t *testing.T) {
	isolatedEnv(t)
	bad := filepath.Join(t.TempDir(), "bugwarrior")
	makeNonExe(t, bad)
	t.Setenv("BUGWARRIOR_BIN", bad)

	// PATH is empty dir, nothing else will match either.
	got := findBugwarrior()
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestFindBugwarrior_EnvVar_Missing confirms a non-existent path is ignored.
func TestFindBugwarrior_EnvVar_Missing(t *testing.T) {
	isolatedEnv(t)
	t.Setenv("BUGWARRIOR_BIN", "/does/not/exist/bugwarrior")

	got := findBugwarrior()
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestFindBugwarrior_PathLookup confirms PATH lookup works when BUGWARRIOR_BIN
// is unset.
func TestFindBugwarrior_PathLookup(t *testing.T) {
	isolatedEnv(t)
	dir := t.TempDir()
	makeExe(t, filepath.Join(dir, "bugwarrior"))
	t.Setenv("PATH", dir)
	t.Setenv("BUGWARRIOR_BIN", "")

	got := findBugwarrior()
	if got == "" {
		t.Error("expected PATH hit, got empty")
	}
}

// TestFindBugwarrior_ProbeFallback confirms the probe list is checked when
// BUGWARRIOR_BIN is unset and PATH misses. We fake ~/.local/bin by pointing
// HOME at a temp dir.
func TestFindBugwarrior_ProbeFallback(t *testing.T) {
	isolatedEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := makeExe(t, filepath.Join(home, ".local/bin/bugwarrior"))

	got := findBugwarrior()
	if got != bin {
		t.Errorf("got %q want %q", got, bin)
	}
}

// TestFindBugwarrior_NoneFound confirms empty string is returned when nothing
// matches.
func TestFindBugwarrior_NoneFound(t *testing.T) {
	isolatedEnv(t)
	got := findBugwarrior()
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestNewBugwarrior_Available mirrors findBugwarrior via the public API.
func TestNewBugwarrior_Available(t *testing.T) {
	isolatedEnv(t)
	dir := t.TempDir()
	makeExe(t, filepath.Join(dir, "bugwarrior"))
	t.Setenv("PATH", dir)

	bw := NewBugwarrior(nil)
	if !bw.Available() {
		t.Error("Available() = false, want true")
	}
}

// TestNewBugwarrior_NotAvailable confirms Available is false when nothing found.
func TestNewBugwarrior_NotAvailable(t *testing.T) {
	isolatedEnv(t)
	bw := NewBugwarrior(nil)
	if bw.Available() {
		t.Error("Available() = true, want false")
	}
}

// TestNewBugwarrior_EnvVarPrecedence confirms BUGWARRIOR_BIN beats PATH.
func TestNewBugwarrior_EnvVarPrecedence(t *testing.T) {
	isolatedEnv(t)

	// Put a different binary named bugwarrior in PATH.
	pathDir := t.TempDir()
	makeExe(t, filepath.Join(pathDir, "bugwarrior"))
	t.Setenv("PATH", pathDir)

	// Point BUGWARRIOR_BIN at a distinct location.
	envBin := makeExe(t, filepath.Join(t.TempDir(), "custom-bw"))
	t.Setenv("BUGWARRIOR_BIN", envBin)

	bw := NewBugwarrior(nil)
	if !bw.Available() {
		t.Fatal("Available() = false, want true")
	}
	if bw.bin != envBin {
		t.Errorf("bin = %q, want %q (env var should win)", bw.bin, envBin)
	}
}
