package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeSerial writes a fixture standing in for
// /sys/class/dmi/id/product_serial, so the SMBIOS path is testable without
// real hardware or a hypervisor.
func writeSerial(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "product_serial")

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func encodeBootstrap(t *testing.T, b Bootstrap) string {
	t.Helper()

	payload, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}

	return base64.StdEncoding.EncodeToString(payload)
}

func TestReadsBootstrapFromSMBIOS(t *testing.T) {
	path := writeSerial(t, encodeBootstrap(t, Bootstrap{
		ComputerID:        "01a0844a-88a0-71cd-969e-3339b4565b7a",
		Endpoint:          "https://hyperwake.ai",
		RegistrationToken: "single-use-token",
	}))

	got, err := readBootstrap(path)
	if err != nil {
		t.Fatalf("readBootstrap: %v", err)
	}

	if got == nil {
		t.Fatal("expected a bootstrap payload")
	}

	if got.ComputerID != "01a0844a-88a0-71cd-969e-3339b4565b7a" {
		t.Fatalf("computer id = %q", got.ComputerID)
	}

	if got.Endpoint != "https://hyperwake.ai" {
		t.Fatalf("endpoint = %q", got.Endpoint)
	}

	if got.RegistrationToken != "single-use-token" {
		t.Fatalf("registration token = %q", got.RegistrationToken)
	}
}

// DMI values often arrive with a trailing newline from sysfs.
func TestTolerantOfSurroundingWhitespace(t *testing.T) {
	path := writeSerial(t, "\n  "+encodeBootstrap(t, Bootstrap{
		Endpoint:          "https://hyperwake.ai",
		RegistrationToken: "tok",
	})+"  \n")

	got, err := readBootstrap(path)
	if err != nil {
		t.Fatalf("readBootstrap: %v", err)
	}

	if got == nil || got.Endpoint != "https://hyperwake.ai" {
		t.Fatalf("payload not decoded from padded input: %+v", got)
	}
}

// The encoder on the Proxmox side is not ours to pin, so accept the common
// base64 variants rather than failing a boot over padding.
func TestAcceptsBase64Variants(t *testing.T) {
	payload, err := json.Marshal(Bootstrap{Endpoint: "https://hyperwake.ai", RegistrationToken: "t"})
	if err != nil {
		t.Fatal(err)
	}

	for name, encoded := range map[string]string{
		"std":    base64.StdEncoding.EncodeToString(payload),
		"rawStd": base64.RawStdEncoding.EncodeToString(payload),
		"url":    base64.URLEncoding.EncodeToString(payload),
		"rawURL": base64.RawURLEncoding.EncodeToString(payload),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readBootstrap(writeSerial(t, encoded))
			if err != nil {
				t.Fatalf("readBootstrap: %v", err)
			}

			if got == nil || got.Endpoint != "https://hyperwake.ai" {
				t.Fatalf("payload not decoded: %+v", got)
			}
		})
	}
}

// A missing DMI file is the normal case on the Docker path and on bare metal.
func TestMissingSerialIsNotAnError(t *testing.T) {
	got, err := readBootstrap(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("expected no error for a missing file, got %v", err)
	}

	if got != nil {
		t.Fatalf("expected no payload, got %+v", got)
	}
}

// Unprovisioned hardware leaves placeholder text here; it is not corrupt input.
func TestPlaceholderSerialsAreTreatedAsAbsent(t *testing.T) {
	for _, placeholder := range []string{
		"", "Not Specified", "None", "0", "To be filled by O.E.M.", "Default string",
	} {
		got, err := readBootstrap(writeSerial(t, placeholder))
		if err != nil {
			t.Fatalf("placeholder %q returned error %v", placeholder, err)
		}

		if got != nil {
			t.Fatalf("placeholder %q produced a payload: %+v", placeholder, got)
		}
	}
}

func TestUndecodableSerialIsAnError(t *testing.T) {
	if _, err := readBootstrap(writeSerial(t, "!!!not base64!!!")); err == nil {
		t.Fatal("expected an error for an undecodable serial")
	}
}

func TestNonJSONPayloadIsAnError(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("this is not json"))

	if _, err := readBootstrap(writeSerial(t, encoded)); err == nil {
		t.Fatal("expected an error for a non-JSON payload")
	}
}

// --- Load() integration --------------------------------------------------

func TestLoadFallsBackToSMBIOSWhenEnvIsEmpty(t *testing.T) {
	path := writeSerial(t, encodeBootstrap(t, Bootstrap{
		ComputerID:        "computer-uuid",
		Endpoint:          "https://hyperwake.ai/",
		RegistrationToken: "smbios-token",
	}))

	t.Setenv("HYPERWAKE_ENDPOINT", "")
	t.Setenv("HYPERWAKE_REGISTRATION_TOKEN", "")
	t.Setenv("HYPERWAKE_COMPUTER_ID", "")
	t.Setenv("HYPERWAKE_SMBIOS_PATH", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Endpoint != "https://hyperwake.ai" {
		t.Fatalf("endpoint = %q, want the trailing slash trimmed", cfg.Endpoint)
	}

	if cfg.RegistrationToken != "smbios-token" {
		t.Fatalf("registration token = %q", cfg.RegistrationToken)
	}

	if cfg.ComputerID != "computer-uuid" {
		t.Fatalf("computer id = %q", cfg.ComputerID)
	}

	if cfg.Source != "smbios" {
		t.Fatalf("source = %q, want smbios", cfg.Source)
	}
}

// The Docker path must keep working unchanged even on a guest whose DMI
// happens to carry a payload.
func TestEnvironmentWinsOverSMBIOS(t *testing.T) {
	path := writeSerial(t, encodeBootstrap(t, Bootstrap{
		ComputerID:        "smbios-computer",
		Endpoint:          "https://smbios.example",
		RegistrationToken: "smbios-token",
	}))

	t.Setenv("HYPERWAKE_ENDPOINT", "http://hyperwake.test")
	t.Setenv("HYPERWAKE_REGISTRATION_TOKEN", "env-token")
	t.Setenv("HYPERWAKE_COMPUTER_ID", "env-computer")
	t.Setenv("HYPERWAKE_SMBIOS_PATH", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Endpoint != "http://hyperwake.test" {
		t.Fatalf("endpoint = %q, want the environment value", cfg.Endpoint)
	}

	if cfg.RegistrationToken != "env-token" {
		t.Fatalf("registration token = %q, want the environment value", cfg.RegistrationToken)
	}

	if cfg.ComputerID != "env-computer" {
		t.Fatalf("computer id = %q, want the environment value", cfg.ComputerID)
	}

	if cfg.Source != "environment" {
		t.Fatalf("source = %q, want environment", cfg.Source)
	}
}

// Precedence is per field, so one overridden value does not discard the rest
// of an injected payload.
func TestEnvironmentAndSMBIOSMergePerField(t *testing.T) {
	path := writeSerial(t, encodeBootstrap(t, Bootstrap{
		ComputerID:        "smbios-computer",
		Endpoint:          "https://smbios.example",
		RegistrationToken: "smbios-token",
	}))

	t.Setenv("HYPERWAKE_ENDPOINT", "http://override.test")
	t.Setenv("HYPERWAKE_REGISTRATION_TOKEN", "")
	t.Setenv("HYPERWAKE_COMPUTER_ID", "")
	t.Setenv("HYPERWAKE_SMBIOS_PATH", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Endpoint != "http://override.test" {
		t.Fatalf("endpoint = %q, want the environment override", cfg.Endpoint)
	}

	if cfg.RegistrationToken != "smbios-token" {
		t.Fatalf("registration token = %q, want the SMBIOS value", cfg.RegistrationToken)
	}
}

// A corrupt DMI value must not break the Docker path, where identity comes
// from the environment and DMI is irrelevant.
func TestCorruptSMBIOSIsIgnoredWhenEnvIsSufficient(t *testing.T) {
	t.Setenv("HYPERWAKE_ENDPOINT", "http://hyperwake.test")
	t.Setenv("HYPERWAKE_REGISTRATION_TOKEN", "env-token")
	t.Setenv("HYPERWAKE_COMPUTER_ID", "")
	t.Setenv("HYPERWAKE_SMBIOS_PATH", writeSerial(t, "!!!garbage!!!"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load must not fail when the environment is sufficient: %v", err)
	}

	if cfg.Endpoint != "http://hyperwake.test" {
		t.Fatalf("endpoint = %q", cfg.Endpoint)
	}
}

// But a corrupt value with nothing else to fall back on is a real failure and
// must be surfaced, not silently swallowed into "endpoint is required".
func TestCorruptSMBIOSIsFatalWhenItIsTheOnlySource(t *testing.T) {
	t.Setenv("HYPERWAKE_ENDPOINT", "")
	t.Setenv("HYPERWAKE_REGISTRATION_TOKEN", "")
	t.Setenv("HYPERWAKE_COMPUTER_ID", "")
	t.Setenv("HYPERWAKE_SMBIOS_PATH", writeSerial(t, "!!!garbage!!!"))

	if _, err := Load(); err == nil {
		t.Fatal("expected an error when the only identity source is corrupt")
	}
}
