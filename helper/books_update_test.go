package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func booksUpdateFixture(t *testing.T, running bool) (*Engine, *updateService, releasePin, string) {
	t.Helper()
	e, base := setupFixture(t)
	enableRAMFixture(t, e)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := &updateService{fixtureService: base, runtime: e.paths.Runtime}
	s.running = running
	e.service = s
	if err := os.WriteFile(filepath.Join(e.paths.Runtime, "state/identity"), []byte("saved disconnected identity"), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := os.MkdirTemp(e.paths.RAM, "test-candidate-")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"tailscale": "new executable", "tailscaled": "new daemon"} {
		if err := os.WriteFile(filepath.Join(stage, name), []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	pin, err := pinDirectory(stage, "1.104.6")
	if err != nil {
		t.Fatal(err)
	}
	return e, s, pin, stage
}

func activateBooksFixture(e *Engine, ctx context.Context, pin releasePin, stage string) error {
	return e.withPublicIO(ctx, func(ctx context.Context) error {
		if err := e.persistPublicPair(ctx, stage, pin); err != nil {
			return err
		}
		return e.activateBooksRelease(ctx, pin, stage)
	})
}

func assertBooksIdentity(t *testing.T, e *Engine) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.paths.Runtime, "state/identity"))
	if err != nil || string(data) != "saved disconnected identity" {
		t.Fatal("identity not preserved", err)
	}
}

func TestBooksUpdatePreservesIdentityAndRunningPreference(t *testing.T) {
	for _, running := range []bool{true, false} {
		t.Run(map[bool]string{true: "running", false: "stopped"}[running], func(t *testing.T) {
			e, s, pin, stage := booksUpdateFixture(t, running)
			if err := activateBooksFixture(e, context.Background(), pin, stage); err != nil {
				t.Fatal(err)
			}
			assertBooksIdentity(t, e)
			if s.running != running {
				t.Fatal("update changed daemon preference")
			}
			if !running {
				if _, err := os.Stat(e.ramUpdateJournalPath()); err != nil {
					t.Fatal("stopped update lost deferred recovery", err)
				}
				if err := e.StartService(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			layout, err := e.readLayout()
			if err != nil || layout.Current.Version != pin.Version {
				t.Fatal("wrong selected version", err)
			}
			if _, err := os.Stat(e.ramUpdateJournalPath()); !os.IsNotExist(err) {
				t.Fatal("successful update not committed")
			}
			if _, err := os.Stat(e.paths.legacyBinDir()); !os.IsNotExist(err) {
				t.Fatal("binaries placed on system storage")
			}
		})
	}
}

func TestBooksUpdateFailureRestoresImmutableIdentityAndRAMWithoutBooks(t *testing.T) {
	e, s, pin, stage := booksUpdateFixture(t, true)
	s.failNew = true
	if err := activateBooksFixture(e, context.Background(), pin, stage); err == nil {
		t.Fatal("failed candidate accepted")
	}
	assertBooksIdentity(t, e)
	layout, err := e.readLayout()
	if err != nil || layout.Current.Version != "1.104.4" || !s.running {
		t.Fatal("old release/running state not restored", err)
	}
	if err := os.Rename(e.paths.Books, e.paths.Books+"-unmounted"); err != nil {
		t.Fatal(err)
	}
	if err := e.StartService(context.Background()); err != nil {
		t.Fatal("rollback depended on books", err)
	}
}

func TestBooksUpdateRecoversAfterPowerLossAndRepeatedRollback(t *testing.T) {
	e, s, pin, stage := booksUpdateFixture(t, false)
	if err := activateBooksFixture(e, context.Background(), pin, stage); err != nil {
		t.Fatal(err)
	}
	var journal ramUpdateJournal
	if err := readJSON(e.ramUpdateJournalPath(), &journal); err != nil {
		t.Fatal(err)
	}
	journal.Phase, journal.WasRunning = "activating", true
	if err := writeJSON(e.ramUpdateJournalPath(), journal); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(e.paths.RAM); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.paths.Runtime, "state/identity"), []byte("interrupted mutation"), 0600); err != nil {
		t.Fatal(err)
	}
	s.failStart = true
	if err := e.StartService(context.Background()); err == nil {
		t.Fatal("failed rollback startup accepted")
	}
	assertBooksIdentity(t, e)
	if _, err := os.Stat(e.ramUpdateJournalPath()); err != nil {
		t.Fatal("failed recovery discarded journal", err)
	}
	s.failStart = false
	if err := e.StartService(context.Background()); err != nil {
		t.Fatal("repeated recovery failed", err)
	}
	assertBooksIdentity(t, e)
	layout, err := e.readLayout()
	if err != nil || layout.Current.Version != "1.104.4" || !s.running {
		t.Fatal("power-loss recovery selected candidate", err)
	}
}

type cancelBooksHealth struct {
	*updateService
	cancel context.CancelFunc
	paths  Paths
}

func (s *cancelBooksHealth) Healthy(ctx context.Context, v string) error {
	if v == "1.104.6" {
		s.cancel()
		if err := os.Mkdir(s.paths.usbBlockPath(), 0700); err != nil {
			return err
		}
		if err := os.Rename(s.paths.Books, s.paths.Books+"-unmounted"); err != nil {
			return err
		}
		return context.Canceled
	}
	return s.updateService.Healthy(ctx, v)
}

func TestBooksUpdateUSBCancellationRollsBackFromRAM(t *testing.T) {
	e, s, pin, stage := booksUpdateFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.service = &cancelBooksHealth{updateService: s, cancel: cancel, paths: e.paths}
	if err := activateBooksFixture(e, ctx, pin, stage); !errors.Is(err, context.Canceled) {
		t.Fatal("missing cancellation", err)
	}
	assertBooksIdentity(t, e)
	layout, err := e.readLayout()
	if err != nil || layout.Current.Version != "1.104.4" || !s.running {
		t.Fatal("USB cancellation lost running old pair", err)
	}
	if _, err := os.Stat(e.ramUpdateJournalPath()); !os.IsNotExist(err) {
		t.Fatal("RAM-only rollback did not finish")
	}
}

func TestBooksUpdateSpaceFailureLeavesRunningPair(t *testing.T) {
	e, s, pin, stage := booksUpdateFixture(t, true)
	e.free = func(string) (int64, error) { return 1, nil }
	if err := e.activateBooksRelease(context.Background(), pin, stage); err == nil {
		t.Fatal("low space accepted")
	}
	if s.stops != 0 || !s.running {
		t.Fatal("preflight stopped daemon")
	}
}
