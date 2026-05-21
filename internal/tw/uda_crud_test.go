package tw

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateUDA_CallsCorrectArgv(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "argv")
	argvRecorder(t, logFile)

	c := NewClient()
	if err := c.CreateUDA(context.Background(), "priority", "string", "Priority", "high,medium,low"); err != nil {
		t.Fatalf("CreateUDA: %v", err)
	}

	invocations := readAllInvocations(t, logFile)
	if len(invocations) == 0 {
		t.Fatal("no invocations recorded")
	}

	checkContains := func(key, value string) {
		t.Helper()
		for _, args := range invocations {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, key) && strings.Contains(joined, value) {
				return
			}
		}
		t.Errorf("no invocation with key %q and value %q in: %v", key, value, invocations)
	}
	checkContains("uda.priority.type", "string")
	checkContains("uda.priority.label", "Priority")
	checkContains("uda.priority.values", "high,medium,low")
}

func TestCreateUDA_RejectsBadName(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	for _, bad := range []string{"", "1bad", "a b", "a;b", "+evil", "../etc", "rc.foo=bar"} {
		if err := c.CreateUDA(ctx, bad, "string", "Label", ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("name %q: expected ErrInvalid, got %v", bad, err)
		}
	}
}

func TestCreateUDA_RejectsBadType(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	for _, bad := range []string{"", "text", "int", "boolean", "STRING"} {
		if err := c.CreateUDA(ctx, "myuda", bad, "Label", ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("type %q: expected ErrInvalid, got %v", bad, err)
		}
	}
}

func TestCreateUDA_RejectsEmptyLabel(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	if err := c.CreateUDA(ctx, "myuda", "string", "", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for empty label, got %v", err)
	}
	if err := c.CreateUDA(ctx, "myuda", "string", "   ", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for whitespace-only label, got %v", err)
	}
}

func TestCreateUDA_RejectsEmptyValueEntry(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	if err := c.CreateUDA(ctx, "myuda", "string", "Label", "high,,low"); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for empty value entry, got %v", err)
	}
}

func TestCreateUDA_AcceptsValidTypes(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "argv")
	argvRecorder(t, logFile)

	c := NewClient()
	ctx := context.Background()
	for _, typ := range []string{"string", "numeric", "date", "duration"} {
		if err := c.CreateUDA(ctx, "myuda", typ, "My Label", ""); err != nil {
			t.Errorf("type %q: unexpected error %v", typ, err)
		}
	}
}

func TestDeleteUDA_CallsCorrectArgv(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "argv")
	argvRecorder(t, logFile)

	c := NewClient()
	if err := c.DeleteUDA(context.Background(), "priority"); err != nil {
		t.Fatalf("DeleteUDA: %v", err)
	}

	invocations := readAllInvocations(t, logFile)
	checkKey := func(key string) {
		t.Helper()
		for _, args := range invocations {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "config") && strings.Contains(joined, key) {
				return
			}
		}
		t.Errorf("no 'config %s' invocation found in: %v", key, invocations)
	}
	checkKey("uda.priority.type")
	checkKey("uda.priority.label")
	checkKey("uda.priority.values")
}

func TestDeleteUDA_RejectsBadName(t *testing.T) {
	c := NewClient()
	ctx := context.Background()
	for _, bad := range []string{"", "a b", "+evil", "a;b", "1nope"} {
		if err := c.DeleteUDA(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("name %q: expected ErrInvalid, got %v", bad, err)
		}
	}
}

func TestUpdateUDA_CallsCorrectArgv(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "argv")
	argvRecorder(t, logFile)

	c := NewClient()
	if err := c.UpdateUDA(context.Background(), "effort", "numeric", "Effort", ""); err != nil {
		t.Fatalf("UpdateUDA: %v", err)
	}

	invocations := readAllInvocations(t, logFile)
	checkContains := func(key, value string) {
		t.Helper()
		for _, args := range invocations {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, key) && strings.Contains(joined, value) {
				return
			}
		}
		t.Errorf("no invocation with key %q and value %q in: %v", key, value, invocations)
	}
	checkContains("uda.effort.type", "numeric")
	checkContains("uda.effort.label", "Effort")
}

func TestCreateUDA_InvalidatesCache(t *testing.T) {
	callsFile := filepath.Join(t.TempDir(), "calls")
	body := `#!/bin/sh
echo X >> "` + callsFile + `"
case "$*" in
  *"_udas"*) printf 'priority\n'; exit 0;;
  *"_get"*"type"*) printf 'string\n'; exit 0;;
  *"_get"*"label"*) printf 'Priority\n'; exit 0;;
  *"_get"*"values"*) printf ''; exit 0;;
esac
exit 0
`
	scriptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(scriptDir, "task"), []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := NewClient()
	ctx := context.Background()

	_ = c.UDAsCached(ctx)

	if err := c.CreateUDA(ctx, "effort", "numeric", "Effort", ""); err != nil {
		t.Fatalf("CreateUDA: %v", err)
	}

	callsBefore, _ := os.ReadFile(callsFile)
	countBefore := strings.Count(string(callsBefore), "X")

	_ = c.UDAsCached(ctx)

	callsAfter, _ := os.ReadFile(callsFile)
	countAfter := strings.Count(string(callsAfter), "X")

	if countAfter <= countBefore {
		t.Errorf("cache was not invalidated: calls before=%d after=%d", countBefore, countAfter)
	}
}
