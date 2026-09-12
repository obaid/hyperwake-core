// Package config loads the guest daemon's runtime configuration.
//
// Everything comes from the environment, which is what the per-machine
// bootstrap injects at clone time. Nothing is baked into the image: an image
// that carried a shared secret would let any tenant impersonate any other.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Version is stamped at build time with -ldflags.
var Version = "dev"

// Config is the daemon's full runtime configuration.
type Config struct {
	// Endpoint is the control plane base URL, e.g. http://mola.test.
	Endpoint string

	// RegistrationToken is single-use. It is exchanged for a machine token on
	// first boot and then deleted from disk.
	RegistrationToken string

	// MachineID identifies this machine to the control plane. Generated on
	// first boot if absent so that two clones of one image never share one.
	MachineID string

	// ComputerID is the control plane's own id for this machine, when the
	// bootstrap supplied it. Purely informational before registration — the
	// authoritative value comes back in the registration response.
	ComputerID string

	// Source records where identity came from, for the boot log. A machine
	// that registered off the wrong source is otherwise very hard to diagnose.
	Source string

	// StateDir holds the machine token and machine id between boots. It lives
	// on the persistent volume so a wake does not re-register.
	StateDir string

	// HeartbeatInterval is the starting cadence. The control plane may widen
	// or narrow it in any heartbeat response.
	HeartbeatInterval time.Duration

	// HTTPTimeout bounds every call home.
	HTTPTimeout time.Duration

	// ShutdownCommand is run when the control plane sets desired_state=stopped.
	ShutdownCommand []string

	// Version of the guest daemon.
	Version string

	// OS is a human-readable description of the guest operating system.
	OS string

	// AuthorizedKeysPath is the file the daemon reconciles against the key
	// set the control plane sends on each heartbeat.
	AuthorizedKeysPath string
}

const (
	defaultHeartbeatInterval = 15 * time.Second
	defaultHTTPTimeout       = 15 * time.Second
	defaultStateDir          = "/var/lib/mola"
	legacyStateDir           = "/var/lib/hyperwake"
	defaultAuthorizedKeys    = "/home/dev/.ssh/authorized_keys"
)

// Load reads configuration from the environment, falling back to the SMBIOS
// bootstrap payload for identity fields the environment does not supply.
//
// The two provisioning paths inject identity differently:
//
//   - Docker / development: environment variables, set at container creation.
//   - Proxmox / production: `smbios1: serial=<base64 JSON>` set at clone time,
//     read back from DMI. Cloud-init snippets are not available, because they
//     need filesystem access to Proxmox host storage and the control plane's
//     API token is deliberately scoped to VM lifecycle only.
//
// The environment wins field by field, so a Proxmox guest can still have any
// individual value overridden for debugging without discarding the rest of the
// injected payload.
func Load() (*Config, error) {
	c := &Config{
		Endpoint:           strings.TrimRight(env("ENDPOINT"), "/"),
		RegistrationToken:  env("REGISTRATION_TOKEN"),
		MachineID:          env("MACHINE_ID"),
		ComputerID:         env("COMPUTER_ID"),
		StateDir:           envOr("STATE_DIR", defaultStateDir),
		AuthorizedKeysPath: envOr("AUTHORIZED_KEYS_PATH", defaultAuthorizedKeys),
		HeartbeatInterval:  envDuration("HEARTBEAT_INTERVAL", defaultHeartbeatInterval),
		HTTPTimeout:        envDuration("HTTP_TIMEOUT", defaultHTTPTimeout),
		Version:            Version,
		OS:                 detectOS(),
		Source:             "environment",
	}

	if cmd := env("SHUTDOWN_COMMAND"); cmd != "" {
		c.ShutdownCommand = strings.Fields(cmd)
	} else {
		c.ShutdownCommand = []string{"/sbin/shutdown", "-h", "now"}
	}

	// A machine that first booted under the old name keeps its credential and
	// machine id there. Re-registering would be worse than untidy: the
	// registration token is single-use, so a machine that lost its credential
	// could never register again and would be orphaned for good.
	if c.StateDir == defaultStateDir {
		if _, err := os.Stat(defaultStateDir); errors.Is(err, os.ErrNotExist) {
			if _, err := os.Stat(legacyStateDir); err == nil {
				c.StateDir = legacyStateDir
			}
		}
	}

	if err := c.applyBootstrap(envOr("SMBIOS_PATH", SMBIOSPath)); err != nil {
		return nil, err
	}

	if c.Endpoint == "" {
		return nil, errors.New("no endpoint: set MOLA_ENDPOINT or inject an SMBIOS bootstrap payload")
	}

	if c.HeartbeatInterval < time.Second {
		return nil, fmt.Errorf("heartbeat interval %s is too short", c.HeartbeatInterval)
	}

	return c, nil
}

// applyBootstrap fills in identity fields the environment did not supply from
// the SMBIOS payload.
//
// A malformed payload is only fatal when the environment did not already
// provide what is needed. On the Docker path DMI often holds vendor
// placeholder text, and failing the boot over it would be wrong.
func (c *Config) applyBootstrap(path string) error {
	bootstrap, err := readBootstrap(path)
	if err != nil {
		if c.Endpoint != "" {
			return nil
		}

		return err
	}

	if bootstrap == nil {
		return nil
	}

	used := false

	if c.Endpoint == "" && bootstrap.Endpoint != "" {
		c.Endpoint = strings.TrimRight(bootstrap.Endpoint, "/")
		used = true
	}

	if c.RegistrationToken == "" && bootstrap.RegistrationToken != "" {
		c.RegistrationToken = bootstrap.RegistrationToken
		used = true
	}

	if c.ComputerID == "" && bootstrap.ComputerID != "" {
		c.ComputerID = bootstrap.ComputerID
		used = true
	}

	if used {
		c.Source = "smbios"
	}

	return nil
}

// TokenPath is where the rotating machine credential is persisted.
func (c *Config) TokenPath() string {
	return filepath.Join(c.StateDir, "machine-token")
}

// MachineIDPath is where the generated machine id is persisted.
func (c *Config) MachineIDPath() string {
	return filepath.Join(c.StateDir, "machine-id")
}

// env reads one setting under the current prefix, falling back to the one it
// carried before the project was renamed to Mola.
//
// Both halves of the mismatch are real, and no release order avoids them. A
// host that has been upgraded still launches guest images that have not, since
// an image already on disk is never re-downloaded; and a guest built today can
// be launched by a host nobody has upgraded yet. Reading both names costs one
// extra lookup per setting and removes the ordering problem entirely. The
// hosts write both for the same reason — see runtime/native/host.py.
//
// Safe to delete once no image predating the rename is still in circulation.
func env(name string) string {
	if v := os.Getenv("MOLA_" + name); v != "" {
		return v
	}
	return os.Getenv("HYPERWAKE_" + name)
}

func envOr(name, fallback string) string {
	if v := env(name); v != "" {
		return v
	}
	return fallback
}

func envDuration(name string, fallback time.Duration) time.Duration {
	v := env(name)
	if v == "" {
		return fallback
	}

	// Bare integers are seconds, which is what the control plane sends.
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}

	return d
}

// detectOS reads /etc/os-release so the control plane can track which image
// revision a machine is actually running.
func detectOS() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}

	for _, line := range strings.Split(string(data), "\n") {
		if name, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(name, `"`)
		}
	}

	return "unknown"
}
