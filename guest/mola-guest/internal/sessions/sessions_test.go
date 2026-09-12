package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeProc builds a minimal /proc tree so session counting is testable
// without a real machine.
func fakeProc(t *testing.T, procs map[string]string, extra map[string]string) string {
	t.Helper()

	root := t.TempDir() + "/"

	for pid, cmdline := range procs {
		dir := filepath.Join(root, "proc", pid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		// /proc cmdline arguments are NUL-separated.
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644); err != nil {
			t.Fatal(err)
		}

		// tty_nr 0 = no controlling terminal.
		stat := "1 (proc) S 1 1 1 0 -1 0"
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for name, contents := range extra {
		path := filepath.Join(root, "proc", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

func TestCountsSSHSessionsNotTheListener(t *testing.T) {
	root := fakeProc(t, map[string]string{
		// The listener has no pts and must not count as a session, or every
		// idle machine would look busy and never auto-stop.
		"100": "/usr/sbin/sshd\x00-D\x00",
		"200": "sshd: dev@pts/0\x00",
		"300": "sshd: dev@pts/1\x00",
	}, nil)

	got := NewReader(root).Read()

	if got.SSH != 2 {
		t.Fatalf("ssh sessions = %d, want 2", got.SSH)
	}
}

func TestIgnoresNonNumericProcEntries(t *testing.T) {
	root := fakeProc(t, map[string]string{"200": "sshd: dev@pts/0\x00"}, nil)

	if err := os.MkdirAll(filepath.Join(root, "proc", "self"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := NewReader(root).Read()

	if got.SSH != 1 {
		t.Fatalf("ssh sessions = %d, want 1", got.SSH)
	}
}

func TestMissingProcIsNotAnError(t *testing.T) {
	got := NewReader(t.TempDir() + "/").Read()

	if got.SSH != 0 || got.TTY != 0 {
		t.Fatalf("expected zero counts on a missing /proc, got %+v", got)
	}
}

func TestReadsLoadAndUptime(t *testing.T) {
	root := fakeProc(t, nil, map[string]string{
		"loadavg": "0.42 0.30 0.20 1/200 1234",
		"uptime":  "1234.56 9876.54",
	})

	r := NewReader(root)

	if load := r.LoadAverage(); load != 0.42 {
		t.Fatalf("load = %v, want 0.42", load)
	}

	if uptime := r.Uptime(); uptime != 1234 {
		t.Fatalf("uptime = %d, want 1234", uptime)
	}
}

func TestMalformedProcFilesDegradeToZero(t *testing.T) {
	root := fakeProc(t, nil, map[string]string{
		"loadavg": "not-a-number",
		"uptime":  "",
	})

	r := NewReader(root)

	if load := r.LoadAverage(); load != 0 {
		t.Fatalf("load = %v, want 0 for malformed input", load)
	}

	if uptime := r.Uptime(); uptime != 0 {
		t.Fatalf("uptime = %d, want 0 for malformed input", uptime)
	}
}
