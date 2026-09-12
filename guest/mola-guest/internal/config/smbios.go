package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SMBIOSPath is where the bootstrap payload appears inside a KVM guest.
//
// The Proxmox driver cannot use cloud-init snippets: those require filesystem
// access to Proxmox host storage, and the control plane's API token is scoped
// to VM lifecycle only — deliberately, so a compromised control plane cannot
// write arbitrary files to a hypervisor. Identity is therefore injected as
// `smbios1: serial=<base64 JSON>` at clone time, which the guest reads back
// from DMI.
const SMBIOSPath = "/sys/class/dmi/id/product_serial"

// Bootstrap is the per-machine identity injected at clone time.
//
// It carries no shared secret: the registration token is single-use and unique
// per machine, so reading another machine's DMI would gain an attacker nothing
// they could replay.
type Bootstrap struct {
	ComputerID        string `json:"computer_id"`
	Endpoint          string `json:"endpoint"`
	RegistrationToken string `json:"registration_token"`
}

// readBootstrap loads and decodes the SMBIOS payload at path.
//
// A missing file is not an error: on the Docker development path identity
// arrives through the environment instead, and on bare metal the field is
// simply absent.
func readBootstrap(path string) (*Bootstrap, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("read smbios serial: %w", err)
	}

	encoded := strings.TrimSpace(string(raw))

	// Unprovisioned hardware and most hypervisors leave placeholder values
	// here. Treat them as "no payload" rather than as corrupt input.
	switch encoded {
	case "", "Not Specified", "None", "0", "To be filled by O.E.M.", "Default string":
		return nil, nil
	}

	// Proxmox stores the value as-is; tolerate both standard and URL-safe
	// alphabets, and both padded and unpadded forms, since the encoder on the
	// other side is not ours to pin.
	decoded, err := decodeBase64(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode smbios serial: %w", err)
	}

	var bootstrap Bootstrap
	if err := json.Unmarshal(decoded, &bootstrap); err != nil {
		return nil, fmt.Errorf("parse smbios payload: %w", err)
	}

	return &bootstrap, nil
}

func decodeBase64(encoded string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}

	var lastErr error

	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(encoded)
		if err == nil {
			return decoded, nil
		}

		lastErr = err
	}

	return nil, lastErr
}
