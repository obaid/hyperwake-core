package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/obaid/mola-core/guest/mola-guest/internal/client"
	"github.com/obaid/mola-core/guest/mola-guest/internal/config"
	"github.com/obaid/mola-core/guest/mola-guest/internal/sessions"
)

// stubActivity reports fixed activity without touching /proc.
type stubActivity struct {
	counts sessions.Counts
}

func (s stubActivity) Read() sessions.Counts { return s.counts }
func (s stubActivity) LoadAverage() float64  { return 0.5 }
func (s stubActivity) Uptime() int64         { return 42 }

// recordingShutdowner records that a shutdown was requested.
type recordingShutdowner struct {
	mu     sync.Mutex
	called bool
}

func (r *recordingShutdowner) Shutdown(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.called = true

	return nil
}

func (r *recordingShutdowner) wasCalled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.called
}

// controlPlane is a scriptable fake of the Laravel guest endpoints.
type controlPlane struct {
	mu sync.Mutex

	registrationUsed bool
	currentToken     string
	desiredState     string
	rotateOnBeat     int
	beats            int
	registers        int
	shutdownAcks     int
	failBeats        int // fail this many heartbeats before succeeding
	lastAuth         string
	lastSessions     client.Sessions
}

func newControlPlane() *controlPlane {
	return &controlPlane{currentToken: "machine-token-1", desiredState: "running"}
}

func (c *controlPlane) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/guest/register", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.registers++

		var req client.RegisterRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if c.registrationUsed {
			w.WriteHeader(http.StatusConflict)

			return
		}

		if req.MachineID == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)

			return
		}

		c.registrationUsed = true

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.RegisterResponse{
			MachineToken:      c.currentToken,
			ComputerID:        "computer-uuid",
			HeartbeatInterval: 1,
		})
	})

	mux.HandleFunc("/guest/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.beats++
		c.lastAuth = r.Header.Get("Authorization")

		var req client.HeartbeatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		c.lastSessions = req.Sessions

		if c.failBeats > 0 {
			c.failBeats--
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		if c.lastAuth != "Bearer "+c.currentToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		resp := client.HeartbeatResponse{
			DesiredState:      c.desiredState,
			HeartbeatInterval: 1,
		}

		if c.rotateOnBeat > 0 && c.beats == c.rotateOnBeat {
			c.currentToken = "machine-token-rotated"
			resp.MachineToken = c.currentToken
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/guest/shutdown-ack", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.shutdownAcks++
		w.WriteHeader(http.StatusNoContent)
	})

	return mux
}

func (c *controlPlane) snapshot(fn func(*controlPlane)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(c)
}

// harness wires a daemon to a fake control plane with instant sleeps.
type harness struct {
	daemon   *Daemon
	store    *Store
	plane    *controlPlane
	server   *httptest.Server
	shutdown *recordingShutdowner
	slept    []time.Duration
}

func newHarness(t *testing.T, plane *controlPlane) *harness {
	t.Helper()

	server := httptest.NewServer(plane.handler())
	t.Cleanup(server.Close)

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	cfg := &config.Config{
		Endpoint:          server.URL,
		RegistrationToken: "registration-token",
		StateDir:          "",
		HeartbeatInterval: time.Second,
		HTTPTimeout:       2 * time.Second,
		Version:           "test",
		OS:                "test-os",
	}

	shutdown := &recordingShutdowner{}

	h := &harness{
		store:    store,
		plane:    plane,
		server:   server,
		shutdown: shutdown,
	}

	d := New(Options{
		Config:   cfg,
		Client:   client.New(server.URL, cfg.HTTPTimeout),
		Store:    store,
		Activity: stubActivity{counts: sessions.Counts{SSH: 1, TTY: 0}},
		Shutdown: shutdown,
	})

	// Never actually wait in tests; just record what the daemon asked for so
	// backoff behaviour is still assertable.
	d.sleep = func(ctx context.Context, dur time.Duration) {
		h.slept = append(h.slept, dur)
	}
	d.jitter = func() float64 { return 0 }

	h.daemon = d

	return h
}

func TestRegistersOnFirstBootAndPersistsCredential(t *testing.T) {
	h := newHarness(t, newControlPlane())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := h.daemon.ensureRegistered(ctx); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}

	if got := h.store.MachineToken(); got != "machine-token-1" {
		t.Fatalf("machine token = %q, want machine-token-1", got)
	}

	if got := h.store.ComputerID(); got != "computer-uuid" {
		t.Fatalf("computer id = %q, want computer-uuid", got)
	}

	if h.store.read("machine-id") == "" {
		t.Fatal("machine id was not generated and persisted")
	}
}

// Waking a stopped computer must not burn the single-use registration token
// again: the credential is on the persistent volume.
func TestSecondBootReusesStoredCredential(t *testing.T) {
	plane := newControlPlane()
	h := newHarness(t, plane)

	ctx := context.Background()

	if err := h.daemon.ensureRegistered(ctx); err != nil {
		t.Fatalf("first register: %v", err)
	}

	if err := h.daemon.ensureRegistered(ctx); err != nil {
		t.Fatalf("second register: %v", err)
	}

	var registers int
	plane.snapshot(func(c *controlPlane) { registers = c.registers })

	if registers != 1 {
		t.Fatalf("registered %d times, want 1", registers)
	}
}

// Two clones of the same image must never end up with the same machine id.
func TestMachineIDIsUniquePerStore(t *testing.T) {
	first, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	second, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	a, err := first.MachineID("")
	if err != nil {
		t.Fatal(err)
	}

	b, err := second.MachineID("")
	if err != nil {
		t.Fatal(err)
	}

	if a == b {
		t.Fatalf("two machines generated the same id %q", a)
	}

	// And it must be stable across calls on one machine.
	again, err := first.MachineID("")
	if err != nil {
		t.Fatal(err)
	}

	if again != a {
		t.Fatalf("machine id changed between calls: %q then %q", a, again)
	}
}

// A replayed registration token is terminal — retrying can never succeed.
func TestRegistrationReplayIsFatal(t *testing.T) {
	plane := newControlPlane()
	plane.registrationUsed = true

	h := newHarness(t, plane)

	err := h.daemon.ensureRegistered(context.Background())
	if err == nil {
		t.Fatal("expected an error when the registration token was already used")
	}

	if !errors.Is(err, client.ErrRegistrationUsed) {
		t.Fatalf("error = %v, want ErrRegistrationUsed", err)
	}
}

func TestHeartbeatPersistsRotatedCredential(t *testing.T) {
	plane := newControlPlane()
	plane.rotateOnBeat = 1

	h := newHarness(t, plane)
	ctx := context.Background()

	if err := h.daemon.ensureRegistered(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := h.daemon.beat(ctx); err != nil {
		t.Fatalf("beat: %v", err)
	}

	if got := h.store.MachineToken(); got != "machine-token-rotated" {
		t.Fatalf("stored token = %q, want machine-token-rotated", got)
	}

	// The next beat must use the new credential, or the control plane will
	// reject it.
	if _, err := h.daemon.beat(ctx); err != nil {
		t.Fatalf("beat after rotation: %v", err)
	}

	var auth string
	plane.snapshot(func(c *controlPlane) { auth = c.lastAuth })

	if auth != "Bearer machine-token-rotated" {
		t.Fatalf("auth header = %q, want the rotated token", auth)
	}
}

func TestHeartbeatReportsSessionHints(t *testing.T) {
	plane := newControlPlane()
	h := newHarness(t, plane)
	ctx := context.Background()

	if err := h.daemon.ensureRegistered(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := h.daemon.beat(ctx); err != nil {
		t.Fatal(err)
	}

	var got client.Sessions
	plane.snapshot(func(c *controlPlane) { got = c.lastSessions })

	if got.SSH != 1 {
		t.Fatalf("reported ssh sessions = %d, want 1", got.SSH)
	}
}

// The control plane going away must not kill the daemon; it backs off and
// recovers when the endpoint returns.
func TestHeartbeatBacksOffAndRecovers(t *testing.T) {
	plane := newControlPlane()
	plane.failBeats = 3

	h := newHarness(t, plane)
	ctx := context.Background()

	if err := h.daemon.ensureRegistered(ctx); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if _, err := h.daemon.beat(ctx); err == nil {
			t.Fatalf("beat %d: expected failure", i)
		}
		h.daemon.failures++
	}

	// Backoff must grow and stay bounded.
	if backoffFor(1) >= backoffFor(3) {
		t.Fatal("backoff is not increasing")
	}

	if backoffFor(50) > maxBackoff {
		t.Fatalf("backoff %s exceeds the %s ceiling", backoffFor(50), maxBackoff)
	}

	if _, err := h.daemon.beat(ctx); err != nil {
		t.Fatalf("beat after recovery: %v", err)
	}
}

// A rejected credential must stop the daemon reusing it, not loop forever.
func TestRejectedCredentialIsClearedAndFatal(t *testing.T) {
	plane := newControlPlane()
	h := newHarness(t, plane)
	ctx := context.Background()

	if err := h.daemon.ensureRegistered(ctx); err != nil {
		t.Fatal(err)
	}

	// The control plane rotates its expectation without telling the guest.
	plane.snapshot(func(c *controlPlane) { c.currentToken = "some-other-token" })

	err := h.daemon.Run(ctx)
	if !errors.Is(err, client.ErrUnauthorized) {
		t.Fatalf("Run returned %v, want ErrUnauthorized", err)
	}

	if got := h.store.MachineToken(); got != "" {
		t.Fatalf("credential %q was not cleared after rejection", got)
	}
}

func TestObeysStopDesiredState(t *testing.T) {
	plane := newControlPlane()
	plane.desiredState = "stopped"

	h := newHarness(t, plane)

	if err := h.daemon.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !h.shutdown.wasCalled() {
		t.Fatal("daemon did not shut the guest down when asked")
	}

	var acks int
	plane.snapshot(func(c *controlPlane) { acks = c.shutdownAcks })

	if acks != 1 {
		t.Fatalf("shutdown acks = %d, want 1", acks)
	}
}

// A cancelled context must end the loop cleanly rather than hanging.
func TestRunStopsOnContextCancel(t *testing.T) {
	plane := newControlPlane()
	h := newHarness(t, plane)

	ctx, cancel := context.WithCancel(context.Background())

	if err := h.daemon.ensureRegistered(ctx); err != nil {
		t.Fatal(err)
	}

	cancel()

	done := make(chan error, 1)
	go func() { done <- h.daemon.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on cancel", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestJitterStaysWithinBounds(t *testing.T) {
	h := newHarness(t, newControlPlane())
	h.daemon.interval = 10 * time.Second

	h.daemon.jitter = func() float64 { return 0 }
	if got := h.daemon.nextInterval(); got != 10*time.Second {
		t.Fatalf("interval with no jitter = %s, want 10s", got)
	}

	h.daemon.jitter = func() float64 { return 0.999 }
	got := h.daemon.nextInterval()

	if got <= 10*time.Second || got > 12*time.Second {
		t.Fatalf("interval with max jitter = %s, want between 10s and 12s", got)
	}
}
