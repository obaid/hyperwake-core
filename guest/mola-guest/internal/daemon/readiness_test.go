package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/obaid/mola-core/guest/mola-guest/internal/capabilities"
	"github.com/obaid/mola-core/guest/mola-guest/internal/client"
	"github.com/obaid/mola-core/guest/mola-guest/internal/config"
	"github.com/obaid/mola-core/guest/mola-guest/internal/sessions"
)

// stubCapabilities reports a fixed capability set.
type stubCapabilities struct {
	report capabilities.Report
}

func (s stubCapabilities) Probe(context.Context) capabilities.Report { return s.report }

// P0-10: the heartbeat must carry evidence of *which* boot is answering, not
// merely that something answered.
func TestHeartbeatCarriesReadinessEvidence(t *testing.T) {
	var got client.HeartbeatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)

		_ = json.NewEncoder(w).Encode(client.HeartbeatResponse{
			DesiredState:      "running",
			RuntimeGeneration: 7,
			Challenge:         "nonce-abc",
		})
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{report: capabilities.Report{
		Shell: true, Display: true, SSHD: true,
	}})

	if _, err := d.beat(context.Background()); err != nil {
		t.Fatalf("beat: %v", err)
	}

	if got.BootID == "" {
		t.Error("heartbeat must carry a boot id; without it the control plane cannot tell this boot from the previous one")
	}

	if !got.Capabilities.Shell || !got.Capabilities.Display || !got.Capabilities.SSHD {
		t.Errorf("capabilities not reported: %+v", got.Capabilities)
	}
}

// The challenge from one beat must be answered on the next, which is what
// proves the daemon is responding now rather than having responded once.
func TestDaemonEchoesChallengeOnNextBeat(t *testing.T) {
	var seen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req client.HeartbeatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		seen = append(seen, req.ChallengeResponse)

		_ = json.NewEncoder(w).Encode(client.HeartbeatResponse{
			DesiredState:      "running",
			RuntimeGeneration: 3,
			Challenge:         "nonce-2",
		})
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{})

	if _, err := d.beat(context.Background()); err != nil {
		t.Fatalf("first beat: %v", err)
	}
	if _, err := d.beat(context.Background()); err != nil {
		t.Fatalf("second beat: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("expected two beats, got %d", len(seen))
	}
	if seen[0] != "" {
		t.Errorf("first beat had nothing to answer, sent %q", seen[0])
	}
	if seen[1] != "nonce-2" {
		t.Errorf("second beat must echo the nonce, sent %q", seen[1])
	}
}

// The daemon echoes the generation it was told, so a daemon left over from a
// previous generation is visible as stale rather than silently accepted.
func TestDaemonEchoesRuntimeGeneration(t *testing.T) {
	var second client.HeartbeatRequest
	call := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		if call == 2 {
			_ = json.NewDecoder(r.Body).Decode(&second)
		}

		_ = json.NewEncoder(w).Encode(client.HeartbeatResponse{
			DesiredState:      "running",
			RuntimeGeneration: 11,
		})
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{})

	_, _ = d.beat(context.Background())
	_, _ = d.beat(context.Background())

	if second.RuntimeGeneration != 11 {
		t.Errorf("expected generation 11 echoed, got %d", second.RuntimeGeneration)
	}
}

// P0-13: a lost success response must not strand the machine. The retry comes
// back as a success flagged Recovered, and the daemon must store the new
// credential rather than treating it as a fresh enrolment or an error.
func TestRegistrationRecoversFromLostResponse(t *testing.T) {
	attempts := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++

		// First attempt: the server succeeds but the response is lost.
		if attempts == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("cannot simulate a dropped response")

				return
			}
			conn, _, err := hj.Hijack()
			if err == nil {
				_ = conn.Close()
			}

			return
		}

		_ = json.NewEncoder(w).Encode(client.RegisterResponse{
			MachineToken:      "recovered-token",
			ComputerID:        "computer-1",
			HeartbeatInterval: 15,
			Recovered:         true,
		})
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{})
	d.cfg.RegistrationToken = "enrol-token"
	d.sleep = func(context.Context, time.Duration) {}

	// Start unregistered: this is a first-boot enrolment.
	if err := d.store.Clear(); err != nil {
		t.Fatalf("clear seeded credential: %v", err)
	}

	if err := d.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("daemon must recover from a lost registration response, got: %v", err)
	}

	if d.store.MachineToken() != "recovered-token" {
		t.Errorf("recovered credential not stored, got %q", d.store.MachineToken())
	}
}

// The machine id must be established before the first registration request:
// it is the identity the control plane matches on to tell a legitimate retry
// from a clone replaying the same token.
func TestMachineIDPersistsBeforeRegistration(t *testing.T) {
	var sent []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req client.RegisterRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		sent = append(sent, req.MachineID)

		if len(sent) < 2 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_ = json.NewEncoder(w).Encode(client.RegisterResponse{
			MachineToken: "t", ComputerID: "c", Recovered: true,
		})
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{})
	d.cfg.RegistrationToken = "enrol-token"
	d.sleep = func(context.Context, time.Duration) {}

	// Start unregistered: this is a first-boot enrolment.
	if err := d.store.Clear(); err != nil {
		t.Fatalf("clear seeded credential: %v", err)
	}

	if err := d.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}

	if len(sent) < 2 {
		t.Fatalf("expected a retry, got %d attempt(s)", len(sent))
	}

	if sent[0] == "" {
		t.Error("machine id must be established before the first request")
	}

	if sent[0] != sent[1] {
		t.Errorf("machine id must be stable across attempts: %q then %q", sent[0], sent[1])
	}
}

func newTestDaemon(t *testing.T, endpoint string, caps Capabilities) *Daemon {
	t.Helper()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	if err := store.SetMachineToken("machine-token"); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	return New(Options{
		Config: &config.Config{
			Endpoint:          endpoint,
			HeartbeatInterval: time.Second,
			HTTPTimeout:       2 * time.Second,
			Version:           "test",
		},
		Client:       client.New(endpoint, 2*time.Second),
		Store:        store,
		Activity:     stubActivity{counts: sessions.Counts{}},
		Capabilities: caps,
	})
}
