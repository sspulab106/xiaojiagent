package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitDone polls a run until it leaves "running" (or fails the test).
func waitDone(t *testing.T, m *SelfCheckManager, id string) *SelfCheckRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, ok := m.Status(id)
		if !ok {
			t.Fatalf("run %s not found", id)
		}
		if run.Status != "running" {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %s still running after timeout", id)
	return nil
}

func TestSelfCheckStartMissingScript(t *testing.T) {
	m := NewSelfCheckManager(filepath.Join(t.TempDir(), "nope.sh"), t.TempDir(), "http://127.0.0.1:8792", nil)
	if _, err := m.Start(); err == nil {
		t.Fatal("expected error for missing script, got nil")
	}
}

func TestSelfCheckRunSuccess(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "verify-ndp.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\necho hello-world\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewSelfCheckManager(script, dir, "http://127.0.0.1:8792", nil)
	id, err := m.Start()
	if err != nil {
		t.Fatal(err)
	}
	run := waitDone(t, m, id)
	if run.Status != "done" {
		t.Fatalf("status = %s, want done (exit %d)", run.Status, run.ExitCode)
	}
	if run.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", run.ExitCode)
	}
	if run.Output != "hello-world\n" {
		t.Fatalf("output = %q, want %q", run.Output, "hello-world\n")
	}
	if run.FinishedAt.IsZero() {
		t.Fatal("finished_at not set")
	}
}

func TestSelfCheckRunFailure(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "verify-ndp.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\necho boom >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewSelfCheckManager(script, dir, "http://127.0.0.1:8792", nil)
	id, err := m.Start()
	if err != nil {
		t.Fatal(err)
	}
	run := waitDone(t, m, id)
	if run.Status != "failed" {
		t.Fatalf("status = %s, want failed", run.Status)
	}
	if run.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", run.ExitCode)
	}
	if run.Output != "boom\n" {
		t.Fatalf("output = %q, want %q", run.Output, "boom\n")
	}
}

func TestSelfCheckStatusUnknown(t *testing.T) {
	m := NewSelfCheckManager("", t.TempDir(), "http://127.0.0.1:8792", nil)
	if _, ok := m.Status("nope"); ok {
		t.Fatal("expected unknown id to be not found")
	}
}

func TestLocalBaseURL(t *testing.T) {
	cases := map[string]string{
		":8792":          "http://127.0.0.1:8792",
		"0.0.0.0:8792":   "http://127.0.0.1:8792",
		"127.0.0.1:9000": "http://127.0.0.1:9000",
	}
	for in, want := range cases {
		if got := localBaseURL(in); got != want {
			t.Errorf("localBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
