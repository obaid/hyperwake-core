package config

import (
	"testing"
	"time"
)

func TestLoadRequiresEndpoint(t *testing.T) {
	t.Setenv("HYPERWAKE_ENDPOINT", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error when HYPERWAKE_ENDPOINT is unset")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("HYPERWAKE_ENDPOINT", "http://hyperwake.test/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// A trailing slash would produce "http://host//guest/register".
	if cfg.Endpoint != "http://hyperwake.test" {
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
	t.Setenv("HYPERWAKE_ENDPOINT", "http://hyperwake.test")
	t.Setenv("HYPERWAKE_HEARTBEAT_INTERVAL", "30")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.HeartbeatInterval != 30*time.Second {
		t.Fatalf("interval = %s, want 30s", cfg.HeartbeatInterval)
	}
}

func TestHeartbeatIntervalAcceptsDurationStrings(t *testing.T) {
	t.Setenv("HYPERWAKE_ENDPOINT", "http://hyperwake.test")
	t.Setenv("HYPERWAKE_HEARTBEAT_INTERVAL", "2m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.HeartbeatInterval != 2*time.Minute {
		t.Fatalf("interval = %s, want 2m", cfg.HeartbeatInterval)
	}
}

func TestRejectsAbsurdlyShortInterval(t *testing.T) {
	t.Setenv("HYPERWAKE_ENDPOINT", "http://hyperwake.test")
	t.Setenv("HYPERWAKE_HEARTBEAT_INTERVAL", "10ms")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a sub-second heartbeat interval")
	}
}

func TestTokenPathsLiveUnderStateDir(t *testing.T) {
	t.Setenv("HYPERWAKE_ENDPOINT", "http://hyperwake.test")
	t.Setenv("HYPERWAKE_STATE_DIR", "/tmp/hw-state")

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
