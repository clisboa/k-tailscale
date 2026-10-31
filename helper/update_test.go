package helper

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type updateService struct {
	*fixtureService
	runtime string
	failNew bool
}

func (s *updateService) Version(_ context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if strings.Contains(string(data), "new") {
		return "1.104.6", nil
	}
	return "1.104.4", nil
}
func (s *updateService) Healthy(ctx context.Context, v string) error {
	if v == "1.104.6" && s.failNew {
		_ = os.WriteFile(filepath.Join(s.runtime, "state/identity"), []byte("changed by failed new daemon"), 0600)
		return errors.New("new daemon failed")
	}
	return s.fixtureService.Healthy(ctx, v)
}
func updateFixture(t *testing.T, running bool) (*Engine, *updateService, Artifact) {
	t.Helper()
	e, base := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := &updateService{fixtureService: base, runtime: e.paths.Runtime}
	s.running = running
	e.service = s
	for _, name := range []string{"tailscale", "tailscaled"} {
		if err := os.WriteFile(filepath.Join(e.paths.Runtime, "bin", name), []byte("old executable"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.paths.Runtime, "state/identity"), []byte("saved disconnected identity"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "new.tgz")
	data := releaseArchive(t, "1.104.6", map[string][]byte{"tailscale": []byte("new executable"), "tailscaled": []byte("new daemon")})
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	return e, s, Artifact{Path: p, Version: "1.104.6", Expanded: 24, Files: map[string]int64{"tailscale": 14, "tailscaled": 10}}
}
func TestUpdatePreservesIdentityAndRunningPreference(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "running"}[running], func(t *testing.T) {
			e, s, a := updateFixture(t, running)
			if err := e.activateTailscale(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(filepath.Join(e.paths.Runtime, "state/identity"))
			if string(data) != "saved disconnected identity" {
				t.Fatal("identity changed")
			}
			if s.running != running {
				t.Fatal("running state changed")
			}
			if !running {
				if err := e.StartService(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(e.updateJournalPath()); !os.IsNotExist(err) {
				t.Fatal("transaction not committed")
			}
		})
	}
}
func TestUpdateFailureRestoresImmutableStateAndPair(t *testing.T) {
	e, s, a := updateFixture(t, true)
	s.failNew = true
	if err := e.activateTailscale(context.Background(), a); err == nil {
		t.Fatal("failed health accepted")
	}
	for _, name := range []string{"tailscale", "tailscaled"} {
		v, err := s.Version(context.Background(), filepath.Join(e.paths.Runtime, "bin", name))
		if err != nil || v != "1.104.4" {
			t.Fatal("old pair not restored", err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(e.paths.Runtime, "state/identity"))
	if string(data) != "saved disconnected identity" || !s.running {
		t.Fatal("saved state not restored")
	}
}
func TestUpdateSpaceFailureNeverStopsService(t *testing.T) {
	e, s, a := updateFixture(t, true)
	e.free = func(string) (int64, error) { return 1, nil }
	if err := e.activateTailscale(context.Background(), a); err == nil {
		t.Fatal("insufficient space accepted")
	}
	if s.stops != 0 || !s.running {
		t.Fatal("space failure stopped daemon")
	}
}

func releaseArchive(t *testing.T, v string, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	w := tar.NewWriter(gz)
	for n, data := range files {
		if err := w.WriteHeader(&tar.Header{Name: "tailscale_" + v + "_arm/" + n, Mode: 0755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestRecoveryAtLiveDirectoryRenameWindows(t *testing.T) {
	for _, window := range []string{"before-first-rename", "between-renames", "after-renames", "interrupted-bin-restore", "interrupted-state-restore"} {
		t.Run(window, func(t *testing.T) {
			e, s, a := updateFixture(t, false)
			if err := e.activateTailscale(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			var j updateJournal
			if err := readJSON(e.updateJournalPath(), &j); err != nil {
				t.Fatal(err)
			}
			j.Phase = "activating"
			j.WasRunning = true
			txn := filepath.Join(e.updateRoot(), "transaction")
			live := filepath.Join(e.paths.Runtime, "bin")
			prev := filepath.Join(txn, "previous")
			switch window {
			case "before-first-rename":
				_ = os.RemoveAll(live)
				if err := os.Rename(prev, live); err != nil {
					t.Fatal(err)
				}
			case "between-renames":
				if err := os.RemoveAll(live); err != nil {
					t.Fatal(err)
				}
			case "interrupted-bin-restore":
				j.Phase = "rollback"
				if err := os.Rename(live, filepath.Join(txn, "discard-bin")); err != nil {
					t.Fatal(err)
				}
			case "interrupted-state-restore":
				j.Phase = "rollback"
				if err := os.Rename(filepath.Join(e.paths.Runtime, "state"), filepath.Join(txn, "discard-state")); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeJSON(e.updateJournalPath(), j); err != nil {
				t.Fatal(err)
			}
			if err := e.StartService(context.Background()); err != nil {
				t.Fatal(err)
			}
			v, err := s.Version(context.Background(), live+"/tailscale")
			if err != nil || v != "1.104.4" {
				t.Fatal("incomplete old pair", err)
			}
			data, _ := os.ReadFile(filepath.Join(e.paths.Runtime, "state/identity"))
			if string(data) != "saved disconnected identity" {
				t.Fatal("checkpoint lost")
			}
			if err := e.StartService(context.Background()); err != nil {
				t.Fatal("recovery was not idempotent", err)
			}
		})
	}
}
func TestDeferredCandidateFailureOnNextStartup(t *testing.T) {
	e, s, a := updateFixture(t, false)
	if err := e.activateTailscale(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	s.failNew = true
	if err := e.StartService(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Version(context.Background(), filepath.Join(e.paths.Runtime, "bin/tailscale"))
	if v != "1.104.4" || !s.running {
		t.Fatal("previous daemon not usable after rollback")
	}
}
func TestChecksAreMetadataOnlyAndPersistThrottle(t *testing.T) {
	e, s, _ := updateFixture(t, true)
	if err := e.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	var r checkRecord
	if err := readJSON(filepath.Join(e.updateRoot(), "check.json"), &r); err != nil {
		t.Fatal(err)
	}
	if r.Next-r.Checked != 21600 || r.Latest != "1.104.4" {
		t.Fatal("wrong interval", r)
	}
	e.upstream.baseURL = "http://127.0.0.1:1"
	if err := e.Check(context.Background(), true); err != nil {
		t.Fatal("auto throttle failed", err)
	}
	if s.stops != 0 || s.starts != 0 {
		t.Fatal("metadata check changed service")
	}
	if err := e.Check(context.Background(), false); err == nil {
		t.Fatal("manual check did not bypass throttle")
	}
	if err := readJSON(filepath.Join(e.updateRoot(), "check.json"), &r); err != nil {
		t.Fatal(err)
	}
	if r.Next-r.Checked != 1800 || r.Error == "" {
		t.Fatal("failure backoff missing")
	}
}
func TestInstallRefusesBusyAndLowBatteryBeforeDownload(t *testing.T) {
	e, s, _ := updateFixture(t, true)
	if err := os.Mkdir(e.paths.Lock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := e.InstallTailscale(context.Background()); err == nil {
		t.Fatal("busy action accepted")
	}
	_ = os.Remove(e.paths.Lock)
	e.eligibility = func() error { return errors.New("battery low") }
	if err := e.InstallTailscale(context.Background()); err == nil {
		t.Fatal("low battery accepted")
	}
	if s.stops != 0 {
		t.Fatal("refusal stopped daemon")
	}
}

func TestBackgroundCheckSwitchPersistsAndManualCheckStillWorks(t *testing.T) {
	e, _, _ := updateFixture(t, true)
	if err := e.SetChecks(false); err != nil {
		t.Fatal(err)
	}
	if enabled, err := e.checksEnabled(); err != nil || enabled {
		t.Fatal("switch not persisted", err)
	}
	if err := e.Check(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.updateRoot(), "check.json")); !os.IsNotExist(err) {
		t.Fatal("disabled automatic check contacted upstream")
	}
	if err := e.Check(context.Background(), false); err != nil {
		t.Fatal("manual check disabled", err)
	}
	if err := e.SetChecks(true); err != nil {
		t.Fatal(err)
	}
	if enabled, err := e.checksEnabled(); err != nil || !enabled {
		t.Fatal("switch not reenabled", err)
	}
}

func TestRecoveryBeforeConnectionClearsInterruptedRuntimeTransaction(t *testing.T) {
	e, s, a := updateFixture(t, false)
	if err := e.activateTailscale(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	var j updateJournal
	if err := readJSON(e.updateJournalPath(), &j); err != nil {
		t.Fatal(err)
	}
	j.Phase = "activating"
	j.WasRunning = true
	if err := writeJSON(e.updateJournalPath(), j); err != nil {
		t.Fatal(err)
	}
	if err := e.RecoverFiles(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Version(context.Background(), filepath.Join(e.paths.Runtime, "bin/tailscale"))
	if v != "1.104.4" {
		t.Fatal("a new connection could use an uncommitted executable pair")
	}
	if _, err := os.Stat(e.updateJournalPath()); !os.IsNotExist(err) {
		t.Fatal("connection recovery left an active rollback journal")
	}
}

func TestCheckpointPersistenceFailureRetainsOriginalRuntime(t *testing.T) {
	e, s, a := updateFixture(t, true)
	e.persist = func(path string) error { return errors.New("filesystem could not persist checkpoint parent") }
	if err := e.activateTailscale(context.Background(), a); err == nil {
		t.Fatal("update activated despite an unpersisted checkpoint parent")
	}
	if _, err := os.Stat(e.updateJournalPath()); !os.IsNotExist(err) {
		t.Fatal("activation journal was written before checkpoint hierarchy became durable")
	}
	v, _ := s.Version(context.Background(), filepath.Join(e.paths.Runtime, "bin/tailscale"))
	if v != "1.104.4" || !s.running {
		t.Fatal("original runtime unavailable after persistence failure")
	}
}
