package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type signOutService struct {
	*fixtureService
	calls int
	err   error
}

func (s *signOutService) Logout(context.Context) error { s.calls++; return s.err }

func signOut(t *testing.T, e *Engine, ctx context.Context) error {
	t.Helper()
	operation, ok := interface{}(e).(interface{ SignOut(context.Context) error })
	if !ok {
		t.Fatal("Engine has no serialized sign-out operation")
	}
	return operation.SignOut(ctx)
}

func TestSignOutPreservesInstallationAndSerializes(t *testing.T) {
	for _, name := range []string{"success", "action busy", "service busy", "daemon stopped", "cli failure", "missing cli"} {
		t.Run(name, func(t *testing.T) {
			e, fixture := setupFixture(t)
			if err := e.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			fixture.running = true
			s := &signOutService{fixtureService: fixture}
			e.service = s
			statePath := filepath.Join(e.paths.Runtime, "state/tailscaled.state")
			const state = `{"identity":"kept-for-the-daemon-to-manage"}`
			if err := os.WriteFile(statePath, []byte(state), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			switch name {
			case "action busy":
				if err := os.Mkdir(e.paths.Lock, 0700); err != nil {
					t.Fatal(err)
				}
			case "service busy":
				release, err := acquireServiceLock(context.Background(), e.paths.ServiceLock)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
			case "daemon stopped":
				fixture.running = false
			case "cli failure":
				s.err = errors.New("fixture logout denied")
			case "missing cli":
				if err := os.Remove(filepath.Join(e.paths.Runtime, "bin/tailscale")); err != nil {
					t.Fatal(err)
				}
			}
			err := signOut(t, e, ctx)
			if (err == nil) != (name == "success") {
				t.Fatalf("unexpected result: %v", err)
			}
			wantCalls := 0
			if name == "success" || name == "cli failure" {
				wantCalls = 1
			}
			if s.calls != wantCalls {
				t.Fatalf("logout calls=%d, want %d", s.calls, wantCalls)
			}
			if name == "cli failure" && !strings.Contains(err.Error(), "logout denied") {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(statePath); err != nil || string(got) != state {
				t.Fatal("helper altered saved credentials", err)
			}
			if fixture.starts != 0 || fixture.stops != 0 {
				t.Fatal("sign out restarted or killed daemon")
			}
			if name != "action busy" {
				if _, err := os.Stat(e.paths.Lock); !os.IsNotExist(err) {
					t.Fatal("action lock leaked")
				}
			}
		})
	}
}
