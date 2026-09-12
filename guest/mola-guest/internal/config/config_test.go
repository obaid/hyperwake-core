package config

import (
	"testing"
	"time"
)

func TestLoadRequiresEndpoint(t *testing.T) {
	t.Setenv("MOLA_ENDPOINT", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error when MOLA_ENDPOINT is unset")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("MOLA_ENDPOINT", "http://mola.test/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// A trailing slash would produce "http://host//guest/register".
	if cfg.Endpoint != "http://mola.test" {
		t.Fatalf("endpoint = %q, want the trailing slash trimmed", cfg.Endpoint)
	}

	if cfg.HeartbeatInterval != defaultHeartbeatInterval {
		t.Fatalf("heartbeat interval = %s, want %s", cfg.HeartbeatInterval, defaultHeartbeatInterval)
	}

	if cfg.StateDir != defaultStateDir {
		t.Fatalf("state dir = %q, want %q", cfg.StateDir, defaultStateDir)
	}

	if len(cfg.ShutdownCommand) == 0 {
		t.Fatal("no default shutdown command")
	}
}

// The control plane sends bare integers meaning seconds.
func TestHeartbeatIntervalAcceptsBareSeconds(t *testing.T) {
	t.Setenv("MOLA_ENDPOINT", "http://mola.test")
	t.Setenv("MOLA_HEARTBEAT_INTERVAL", "30")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.HeartbeatInterval != 30*time.Second {
		t.Fatalf("interval = %s, want 30s", cfg.HeartbeatInterval)
	}
}

func TestHeartbeatIntervalAcceptsDurationStrings(t *testing.T) {
	t.Setenv("MOLA_ENDPOINT", "http://mola.test")
	t.Setenv("MOLA_HEARTBEAT_INTERVAL", "2m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.HeartbeatInterval != 2*time.Minute {
		t.Fatalf("interval = %s, want 2m", cfg.HeartbeatInterval)
	}
}

func TestRejectsAbsurdlyShortInterval(t *testing.T) {
	t.Setenv("MOLA_ENDPOINT", "http://mola.test")
	t.Setenv("MOLA_HEARTBEAT_INTERVAL", "10ms")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a sub-second heartbeat interval")
	}
}

func TestTokenPathsLiveUnderStateDir(t *testing.T) {
	t.Setenv("MOLA_ENDPOINT", "http://mola.test")
	t.Setenv("MOLA_STATE_DIR", "/tmp/hw-state")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.TokenPath() != "/tmp/hw-state/machine-token" {
		t.Fatalf("token path = %q", cfg.TokenPath())
	}

	if cfg.MachineIDPath() != "/tmp/hw-state/machine-id" {
		t.Fatalf("machine id path = %q", cfg.MachineIDPath())
	}
}

// A guest built after the rename still has to boot under a host built before
// it. Nothing about release order fixes this: a host is upgraded by npm, and
// the guest image it launches is whatever is already cached on disk, so the
// two versions drift apart in both directions.
func TestLoadAcceptsTheOldEnvironmentPrefix(t *testing.T) {
	t.Setenv("MOLA_ENDPOINT", "")
	t.Setenv("HYPERWAKE_ENDPOINT", "http://old-host.test/")
	t.Setenv("HYPERWAKE_HEARTBEAT_INTERVAL", "30")
	t.Setenv("HYPERWAKE_STATE_DIR", "/tmp/old-state")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Endpoint != "http://old-host.test" {
		t.Fatalf("endpoint = %q, want the value from the old prefix", cfg.Endpoint)
	}

	if cfg.HeartbeatInterval != 30*time.Second {
		t.Fatalf("heartbeat interval = %s, want 30s from the old prefix", cfg.HeartbeatInterval)
	}

	if cfg.StateDir != "/tmp/old-state" {
		t.Fatalf("state dir = %q, want the value from the old prefix", cfg.StateDir)
	}
}

// A host that writes both prefixes must not be ambiguous about which wins.
func TestCurrentPrefixBeatsTheOldOne(t *testing.T) {
	t.Setenv("MOLA_ENDPOINT", "http://new-host.test")
	t.Setenv("HYPERWAKE_ENDPOINT", "http://old-host.test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Endpoint != "http://new-host.test" {
		t.Fatalf("endpoint = %q, want the current prefix to win", cfg.Endpoint)
	}
}

// An empty value under the current prefix is not a value. Hosts that write
// both will sometimes write an empty one, and falling through to the old name
// is the whole point.
func TestAnEmptyCurrentValueFallsThrough(t *testing.T) {
	t.Setenv("MOLA_ENDPOINT", "")
	t.Setenv("HYPERWAKE_ENDPOINT", "http://old-host.test")
	t.Setenv("MOLA_STATE_DIR", "")
	t.Setenv("HYPERWAKE_STATE_DIR", "/tmp/old-state")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.StateDir != "/tmp/old-state" {
		t.Fatalf("state dir = %q, want the old prefix when the new one is empty", cfg.StateDir)
	}
}
