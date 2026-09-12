// Package capabilities probes what this machine can actually do right now.
//
// The control plane will not call a computer ready on a heartbeat timestamp
// alone: a machine that has just been woken still carries the previous boot's
// timestamp, and a VM that boots to a black screen heartbeats perfectly well
// while being useless. These probes are the evidence that turns "the daemon is
// alive" into "the computer works".
//
// Everything here is deliberately generic. Mola sells a computer, not a
// harness, so nothing in this package may ever check for a particular agent,
// CLI or model provider being installed or logged in.
package capabilities

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Report is the capability set sent with each heartbeat.
type Report struct {
	Shell   bool `json:"shell"`
	Display bool `json:"display"`
	SSHD    bool `json:"sshd"`
}

// Prober collects capability evidence.
type Prober struct {
	// DisplayAddr is where the remote display listens. Configurable because
	// the production Omarchy image uses wayvnc and the development image uses
	// TigerVNC; the daemon must not assume either.
	DisplayAddr string

	// SSHAddr is where sshd listens inside the guest.
	SSHAddr string

	// Timeout bounds each probe. Probes run every heartbeat, so a hung one
	// must never stall the beat that auto-stop and health depend on.
	Timeout time.Duration
}

// NewProber builds a prober from the environment, with defaults that suit a
// standard Mola guest.
func NewProber() *Prober {
	return &Prober{
		DisplayAddr: envOr("MOLA_DISPLAY_ADDR", "127.0.0.1:5900"),
		SSHAddr:     envOr("MOLA_SSH_ADDR", "127.0.0.1:22"),
		Timeout:     2 * time.Second,
	}
}

// Probe reports what currently works.
func (p *Prober) Probe(ctx context.Context) Report {
	return Report{
		Shell:   p.probeShell(ctx),
		Display: p.probeListener(p.DisplayAddr),
		SSHD:    p.probeListener(p.SSHAddr),
	}
}

// probeShell proves a command can actually be executed. A machine whose
// filesystem has gone read-only, or whose process table is exhausted, still
// answers HTTP long after it has stopped being able to run anything.
func (p *Prober) probeShell(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	return exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0").Run() == nil
}

// probeListener checks that something is accepting connections. It does not
// speak the protocol — the display and SSH gateways do that, and a listening
// socket is the part the guest can honestly attest to.
func (p *Prober) probeListener(addr string) bool {
	if addr == "" {
		return false
	}

	conn, err := net.DialTimeout("tcp", addr, p.Timeout)
	if err != nil {
		return false
	}

	_ = conn.Close()

	return true
}

var (
	bootOnce sync.Once
	bootID   string
)

// BootID identifies one boot of this guest OS.
//
// This is what lets the control plane tell "the machine we just woke is up"
// from "the machine we woke *before* is still answering". The kernel's boot id
// changes on every boot and is the natural source; when it is unreadable — a
// container, an unusual kernel — a value generated once per process is a
// reasonable stand-in, because the daemon restarts with the machine.
func BootID() string {
	bootOnce.Do(func() {
		if data, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
			if id := strings.TrimSpace(string(data)); id != "" {
				bootID = id

				return
			}
		}

		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err == nil {
			bootID = "gen-" + hex.EncodeToString(buf)

			return
		}

		bootID = "unknown"
	})

	return bootID
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}

	return fallback
}
