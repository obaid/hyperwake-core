package daemon

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperwake/hyperwake-guest/internal/client"
)

// P0-13: the enrolment key must exist before the first registration request.
// It is what later distinguishes this machine from anyone who observed that
// request, so generating it on demand during recovery would prove nothing.
func TestEnrollmentKeyPersistsBeforeFirstRequest(t *testing.T) {
	var sentKeys []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req client.RegisterRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		sentKeys = append(sentKeys, req.EnrollmentPublicKey)

		if len(sentKeys) < 2 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_ = json.NewEncoder(w).Encode(client.RegisterResponse{
			MachineToken: "t", ComputerID: "c",
		})
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{})
	d.cfg.RegistrationToken = "enrol"
	d.sleep = func(context.Context, time.Duration) {}

	if err := d.store.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}

	if err := d.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}

	if sentKeys[0] == "" {
		t.Fatal("first request carried no enrolment public key")
	}

	if sentKeys[0] != sentKeys[1] {
		t.Errorf("enrolment key must be stable across attempts: %q then %q", sentKeys[0], sentKeys[1])
	}

	raw, err := base64.StdEncoding.DecodeString(sentKeys[0])
	if err != nil || len(raw) != ed25519.PublicKeySize {
		t.Errorf("public key is not a base64 ed25519 key: %v (len %d)", err, len(raw))
	}
}

// The daemon must answer a recovery challenge with a signature the control
// plane can verify against the key it recorded at enrolment.
func TestDaemonSignsRecoveryChallenge(t *testing.T) {
	var enrolledKey string
	var proof client.RegisterRequest
	attempt := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++

		var req client.RegisterRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		switch attempt {
		case 1:
			// Enrolment succeeds server-side; the response is lost.
			enrolledKey = req.EnrollmentPublicKey
			hj, _ := w.(http.Hijacker)
			conn, _, err := hj.Hijack()
			if err == nil {
				_ = conn.Close()
			}
		case 2:
			// The retry is challenged rather than answered.
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(client.RegisterResponse{
				RecoveryChallenge: "nonce-to-sign",
			})
		default:
			proof = req
			_ = json.NewEncoder(w).Encode(client.RegisterResponse{
				MachineToken: "recovered", ComputerID: "c", Recovered: true,
			})
		}
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{})
	d.cfg.RegistrationToken = "enrol"
	d.sleep = func(context.Context, time.Duration) {}

	if err := d.store.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}

	if err := d.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("recovery must succeed: %v", err)
	}

	if proof.RecoveryChallenge != "nonce-to-sign" {
		t.Fatalf("challenge not echoed, got %q", proof.RecoveryChallenge)
	}

	pub, err := base64.StdEncoding.DecodeString(enrolledKey)
	if err != nil {
		t.Fatalf("decode enrolled key: %v", err)
	}

	sig, err := base64.StdEncoding.DecodeString(proof.RecoverySignature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}

	// The signature must verify against the key sent at enrolment — that is
	// exactly the check the control plane performs.
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte("nonce-to-sign"), sig) {
		t.Error("signature does not verify against the enrolled public key")
	}

	if d.store.MachineToken() != "recovered" {
		t.Errorf("recovered credential not stored, got %q", d.store.MachineToken())
	}
}

// A control plane that keeps challenging must not spin the daemon forever.
func TestRecoveryChallengeLoopIsBounded(t *testing.T) {
	attempts := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(client.RegisterResponse{RecoveryChallenge: "again"})
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{})
	d.cfg.RegistrationToken = "enrol"
	d.sleep = func(context.Context, time.Duration) {}

	if err := d.store.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- d.ensureRegistered(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected an error once recovery attempts are exhausted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon span the challenge loop instead of giving up")
	}
}

// A forked or restored disk carries the source machine's identity. Using it
// would authenticate this machine as the one it was copied from.
func TestForkedMachineDiscardsCarriedIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.RegisterResponse{
			MachineToken: "fresh-token", ComputerID: "computer-new",
		})
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{})
	d.cfg.RegistrationToken = "enrol"
	d.cfg.ComputerID = "computer-new"
	d.sleep = func(context.Context, time.Duration) {}

	// The disk we booted from belonged to a different computer.
	if err := d.store.SetComputerID("computer-original"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.store.SetMachineToken("credential-of-original"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := d.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}

	if got := d.store.MachineToken(); got == "credential-of-original" {
		t.Error("forked machine kept the source machine's credential")
	}

	if got := d.store.ComputerID(); got != "computer-new" {
		t.Errorf("computer id not updated, got %q", got)
	}
}

// The ordinary wake path must be untouched: the credential survives on the
// persistent volume so waking does not burn the single-use enrolment token.
func TestWakeReusesStoredCredential(t *testing.T) {
	called := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := newTestDaemon(t, srv.URL, stubCapabilities{})
	d.cfg.RegistrationToken = "enrol"
	d.cfg.ComputerID = "computer-1"

	if err := d.store.SetComputerID("computer-1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := d.ensureRegistered(context.Background()); err != nil {
		t.Fatalf("wake must not re-enrol: %v", err)
	}

	if called {
		t.Error("daemon contacted the control plane instead of reusing its credential")
	}
}
