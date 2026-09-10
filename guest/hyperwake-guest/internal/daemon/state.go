package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store persists the machine identity and rotating credential across boots.
//
// The state directory lives on the persistent volume, so waking a stopped
// computer reuses the existing credential instead of burning the single-use
// registration token again.
type Store struct {
	dir string
}

// NewStore builds a store rooted at dir, creating it if needed.
func NewStore(dir string) (*Store, error) {
	// 0700: the credential must not be readable by other users inside the
	// guest. The tenant is root in their own machine, so this is defence
	// against their own unprivileged processes, not against the tenant.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	return &Store{dir: dir}, nil
}

// MachineToken returns the stored credential, or "" when unregistered.
func (s *Store) MachineToken() string {
	return s.read("machine-token")
}

// SetMachineToken writes the credential atomically so a crash mid-write
// cannot leave a truncated token behind.
func (s *Store) SetMachineToken(token string) error {
	return s.writeAtomic("machine-token", token)
}

// MachineID returns the stored machine id, generating and persisting one on
// first call. Generating per-machine rather than baking into the image is what
// stops two clones sharing an identity.
func (s *Store) MachineID(injected string) (string, error) {
	if existing := s.read("machine-id"); existing != "" {
		return existing, nil
	}

	id := injected
	if id == "" {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate machine id: %w", err)
		}
		id = hex.EncodeToString(buf)
	}

	if err := s.writeAtomic("machine-id", id); err != nil {
		return "", err
	}

	return id, nil
}

// EnrollmentKey returns this machine's enrolment signing key, generating and
// persisting one on first call.
//
// The key must exist *before* the first registration request, because it is
// what proves — later, if our success response is lost — that we are the
// machine that enrolled rather than someone who watched it happen. The private
// half never leaves the guest.
func (s *Store) EnrollmentKey() (ed25519.PrivateKey, error) {
	if existing := s.read("enrollment-key"); existing != "" {
		raw, err := base64.StdEncoding.DecodeString(existing)
		if err == nil && len(raw) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(raw), nil
		}
		// A corrupt key is replaced rather than fatal: a machine that cannot
		// read its own key has not yet enrolled with it in any useful sense.
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate enrollment key: %w", err)
	}

	if err := s.writeAtomic("enrollment-key", base64.StdEncoding.EncodeToString(priv)); err != nil {
		return nil, err
	}

	return priv, nil
}

// ComputerID returns the control plane's id for this machine, if known.
func (s *Store) ComputerID() string {
	return s.read("computer-id")
}

// SetComputerID records the control plane's id for this machine.
func (s *Store) SetComputerID(id string) error {
	return s.writeAtomic("computer-id", id)
}

// ClearIdentity discards everything tying this state directory to a previous
// machine: the credential, the computer id, the machine id and the enrolment
// key. Used when a restored or forked disk carries another machine's identity,
// where reusing any of it would mean impersonating the source computer.
func (s *Store) ClearIdentity() error {
	for _, name := range []string{"machine-token", "computer-id", "machine-id", "enrollment-key"} {
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("clear %s: %w", name, err)
		}
	}

	return nil
}

// Clear removes the stored credential. Used when the control plane rejects it,
// so the daemon does not keep replaying a dead token.
func (s *Store) Clear() error {
	err := os.Remove(filepath.Join(s.dir, "machine-token"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func (s *Store) read(name string) string {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func (s *Store) writeAtomic(name, value string) error {
	final := filepath.Join(s.dir, name)
	tmp := final + ".tmp"

	if err := os.WriteFile(tmp, []byte(value+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}

	return nil
}
