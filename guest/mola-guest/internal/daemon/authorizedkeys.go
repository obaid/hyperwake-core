package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// syncAuthorizedKeys rewrites the guest's authorized_keys so it matches what
// the control plane says it should be.
//
// Keys are injected once at machine creation, which means a key added — or
// revoked — afterwards would never reach a machine that already exists. The
// dashboard would show a key removed while the holder could still log in.
// The heartbeat carries the authoritative set, so the file is reconciled here.
//
// A nil slice means the control plane said nothing this beat; the file is left
// alone. An empty (non-nil) slice means "no keys", and the file is truncated.
func syncAuthorizedKeys(path string, keys []string) (bool, error) {
	if keys == nil {
		return false, nil
	}

	desired := normaliseKeys(keys)

	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read authorized_keys: %w", err)
	}

	if string(current) == desired {
		return false, nil
	}

	sshDir := filepath.Dir(path)

	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return false, fmt.Errorf("create ssh dir: %w", err)
	}

	// The daemon runs as root. sshd refuses to read an authorized_keys file
	// that the logging-in user does not own, so a root-owned rewrite would
	// lock every key out — including the ones that already worked. Inherit
	// ownership from the .ssh directory, which the image created as the user.
	uid, gid, haveOwner := ownerOf(sshDir)

	// Write via a temporary file in the same directory so a crash mid-write
	// cannot leave a truncated key file and lock the owner out.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".authorized_keys-*")
	if err != nil {
		return false, fmt.Errorf("stage authorized_keys: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(desired); err != nil {
		tmp.Close()

		return false, fmt.Errorf("write authorized_keys: %w", err)
	}

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()

		return false, fmt.Errorf("chmod authorized_keys: %w", err)
	}

	if haveOwner {
		if err := tmp.Chown(uid, gid); err != nil {
			tmp.Close()

			return false, fmt.Errorf("chown authorized_keys: %w", err)
		}
	}

	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("close authorized_keys: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, fmt.Errorf("install authorized_keys: %w", err)
	}

	return true, nil
}

// normaliseKeys produces a stable, deduplicated file body so an unchanged key
// set never looks like a change and never triggers a rewrite.
func normaliseKeys(keys []string) string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))

	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsAny(key, "\n\r") {
			continue
		}

		if _, dup := seen[key]; dup {
			continue
		}

		seen[key] = struct{}{}

		out = append(out, key)
	}

	sort.Strings(out)

	if len(out) == 0 {
		return ""
	}

	return strings.Join(out, "\n") + "\n"
}

// ownerOf reports the uid/gid owning a path, when the platform exposes it.
func ownerOf(path string) (int, int, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, false
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}

	return int(stat.Uid), int(stat.Gid), true
}
