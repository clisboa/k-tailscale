package helper

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fixtureService struct {
	version       string
	running       bool
	starts, stops int
	failStart     bool
}

func (s *fixtureService) Version(_ context.Context, filename string) (string, error) {
	if _, err := os.Stat(filename); err != nil {
		return "", err
	}
	return s.version, nil
}
func (s *fixtureService) Running(context.Context) (bool, error) { return s.running, nil }
func (s *fixtureService) Start(context.Context) error {
	if s.failStart {
		return errors.New("fixture startup failure")
	}
	s.running = true
	s.starts++
	return nil
}
func (s *fixtureService) Stop(context.Context) error { s.running = false; s.stops++; return nil }
func (s *fixtureService) Healthy(context.Context, string) error {
	if !s.running {
		return errors.New("fixture is stopped")
	}
	return nil
}
func (s *fixtureService) DisableNativeUpdates(context.Context) error { return nil }
func (s *fixtureService) Logout(context.Context) error               { return nil }

func setupFixture(t *testing.T) (*Engine, *fixtureService) {
	t.Helper()
	root := t.TempDir()
	p := Paths{Runtime: filepath.Join(root, "system/runtime"), Payload: filepath.Join(root, "onboard/.adds/k-tailscale/install"), Helpers: filepath.Join(root, "system/ui"), Base: filepath.Join(root, "system/tailscale"), Rules: filepath.Join(root, "etc/99-k-tailscale.rules"), Lock: filepath.Join(root, "connect.lock"), Version: filepath.Join(root, "onboard/.kobo/version"), ServiceLock: filepath.Join(root, "service.lock")}
	p.Links = filepath.Join(root, "usrbin")
	p.CoreLink = filepath.Join(root, "system/core/helper")
	p.CandidateHelper = filepath.Join(root, "system/core/releases/0.1.0/helper")
	if err := os.MkdirAll(filepath.Dir(p.CandidateHelper), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.CandidateHelper, []byte("fixture-helper"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{p.Payload, p.Helpers, filepath.Dir(p.Rules), filepath.Dir(p.Version)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(p.Version, []byte("fixture,fixture,4.46.23852,fixture,fixture,00000000-0000-0000-0000-000000000390"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runtime-boot.sh", "connect-root.sh", "login-root.sh", "diagnose-root.sh"} {
		data := "#!/bin/sh\n# " + name + "\n"
		if name == "connect-root.sh" {
			template, err := os.ReadFile("../scripts/connect-root.sh")
			if err != nil {
				t.Fatal(err)
			}
			data = string(template)
		}
		if err := os.WriteFile(filepath.Join(p.Helpers, name), []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	u, _ := signedUpstream(t)
	artifact, err := u.Download(context.Background(), "1.104.4", p.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(artifact.Path, filepath.Join(p.Payload, "release.tgz")); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(artifact)
	if err := os.WriteFile(filepath.Join(p.Payload, "release.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	service := &fixtureService{version: "1.104.4"}
	return &Engine{paths: p, upstream: u, service: service, free: func(string) (int64, error) { return 1 << 30, nil }}, service
}

func TestPrepareFreshWithoutIdentity(t *testing.T) {
	e, s := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bin/tailscale", "bin/tailscaled", "ready", "setup.json"} {
		if _, err := os.Stat(filepath.Join(e.paths.Runtime, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(e.paths.Runtime, "state/tailscaled.state")); !os.IsNotExist(err) {
		t.Fatal("fresh installer created a saved login")
	}
	if s.starts != 0 || s.stops != 0 {
		t.Fatal("setup changed daemon state")
	}
	if _, err := os.Stat(filepath.Join(e.paths.Payload, "release.tgz")); !os.IsNotExist(err) {
		t.Fatal("initial payload was not closed and removed")
	}
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatalf("repeat setup: %v", err)
	}
}

func TestPreparePreservesCurrentIdentity(t *testing.T) {
	e, s := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := []byte(`{"identity":"fixture","WantRunning":false}`)
	filename := filepath.Join(e.paths.Runtime, "state/tailscaled.state")
	if err := os.WriteFile(filename, state, 0600); err != nil {
		t.Fatal(err)
	}
	s.running = true
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filename)
	if err != nil || string(got) != string(state) {
		t.Fatal("current saved identity/preferences changed")
	}
	if s.stops != 0 || s.starts != 0 {
		t.Fatal("repeat setup restarted daemon")
	}
}

func TestPrepareSpaceBusyAndUnknownLayouts(t *testing.T) {
	for _, kind := range []string{"space", "busy", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			e, _ := setupFixture(t)
			switch kind {
			case "space":
				e.free = func(string) (int64, error) { return 1, nil }
			case "busy":
				if err := os.Mkdir(e.paths.Lock, 0700); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				if err := os.MkdirAll(e.paths.Runtime, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(e.paths.Runtime, "custom"), []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.Prepare(context.Background()); err == nil {
				t.Fatal("unsafe setup succeeded")
			}
			if _, err := os.Stat(filepath.Join(e.paths.Runtime, "ready")); !os.IsNotExist(err) {
				t.Fatal("failed setup became active")
			}
		})
	}
}

func TestPrepareDoesNotAdoptCustomBoot(t *testing.T) {
	e, _ := setupFixture(t)
	if err := os.MkdirAll(e.paths.Base, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.paths.Base, "boot.sh"), []byte("custom boot"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := e.Prepare(context.Background()); err == nil {
		t.Fatal("custom boot was silently adopted")
	}
}

func TestPrepareRejectsMissingCurrentBinary(t *testing.T) {
	e, _ := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.paths.Runtime, "bin/tailscaled")); err != nil {
		t.Fatal(err)
	}
	if err := e.Prepare(context.Background()); err == nil {
		t.Fatal("broken runtime was accepted")
	}
}

func TestSetupRecoveryBeforeMetadataCommit(t *testing.T) {
	e, _ := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(e.paths.Base, "on-boot.sh")
	previous, err := snapshot("on-boot", filename)
	if err != nil {
		t.Fatal(err)
	}
	oldMetadata, err := snapshot("setup", filepath.Join(e.paths.Runtime, "setup.json"))
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("#!/bin/sh\n# new interrupted helper\n")
	journal := setupJournal{Schema: 1, Previous: []configSnapshot{previous, oldMetadata}, Wanted: setupRecord{Schema: 2, Files: map[string]string{"on-boot": digest(changed)}}}
	if err := writeJSON(filepath.Join(e.paths.Runtime, "setup-journal.json"), journal); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filename, changed, 0755); err != nil {
		t.Fatal(err)
	}
	if err := e.recoverSetup(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filename)
	if err != nil || string(got) != string(previous.Data) {
		t.Fatal("uncommitted helper was not rolled back")
	}
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatalf("retry after recovery: %v", err)
	}
}

func TestPrepareAdoptsRecognizedStorageV1(t *testing.T) {
	for _, generation := range []string{"connect", "connect-v2", "connect-v3"} {
		t.Run(generation, func(t *testing.T) {
			e, _ := setupFixture(t)
			if err := e.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"on-boot", "connect", "login", "diagnose"} {
				source := name
				if name == "connect" {
					source = generation
				}
				data, err := legacyHelpers.ReadFile("legacy/" + source + ".sh")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(e.paths.Base, name+".sh"), data, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(filepath.Join(e.paths.Runtime, "setup.json")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(e.paths.CoreLink); err != nil {
				t.Fatal(err)
			}
			state := []byte(`{"identity":"newer-root-node","WantRunning":false}`)
			filename := filepath.Join(e.paths.Runtime, "state/tailscaled.state")
			if err := os.WriteFile(filename, state, 0600); err != nil {
				t.Fatal(err)
			}
			if err := e.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filename)
			if err != nil || string(got) != string(state) {
				t.Fatal("storage-v1 identity changed")
			}
		})
	}
}

func TestNewRuntimeBootDoesNotNeedBooks(t *testing.T) {
	e, s := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	onboard := filepath.Dir(filepath.Dir(e.paths.Version))
	if err := os.Rename(onboard, onboard+"-usb"); err != nil {
		t.Fatal(err)
	}
	if err := e.StartService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.starts != 1 {
		t.Fatal("fresh service was not started without a saved identity")
	}
}

func TestBootRejectsUnknownRuntimeOwner(t *testing.T) {
	e, s := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.paths.Runtime, "owner"), []byte("another-project\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.StartService(context.Background()); err == nil {
		t.Fatal("unknown runtime was started")
	}
	if s.starts != 0 {
		t.Fatal("unknown runtime had a side effect")
	}
}

func TestRuntimeDirectoryLinksAreRefused(t *testing.T) {
	for _, name := range []string{"bin", "state"} {
		t.Run(name, func(t *testing.T) {
			e, _ := setupFixture(t)
			if err := e.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(e.paths.Runtime, name)
			outside := filepath.Join(filepath.Dir(e.paths.Runtime), "outside-"+name)
			if err := os.Rename(original, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, original); err != nil {
				t.Fatal(err)
			}
			if err := e.Prepare(context.Background()); err == nil {
				t.Fatal("runtime link was accepted by setup")
			}
			if err := e.StartService(context.Background()); err == nil {
				t.Fatal("runtime link was accepted by startup")
			}
		})
	}
}

func TestPrepareRejectsInvalidOfflineProofAndDevice(t *testing.T) {
	for _, kind := range []string{"signature", "firmware", "model", "version"} {
		t.Run(kind, func(t *testing.T) {
			e, s := setupFixture(t)
			switch kind {
			case "signature":
				var artifact Artifact
				if err := readJSON(filepath.Join(e.paths.Payload, "release.json"), &artifact); err != nil {
					t.Fatal(err)
				}
				artifact.Proof.PackageSignature = []byte("bad")
				if err := writeJSON(filepath.Join(e.paths.Payload, "release.json"), artifact); err != nil {
					t.Fatal(err)
				}
			case "firmware":
				if err := os.WriteFile(e.paths.Version, []byte("a,b,4.99.0,d,e,390"), 0600); err != nil {
					t.Fatal(err)
				}
			case "model":
				if err := os.WriteFile(e.paths.Version, []byte("a,b,4.46.23852,d,e,393"), 0600); err != nil {
					t.Fatal(err)
				}
			case "version":
				s.version = "1.106.0"
			}
			if err := e.Prepare(context.Background()); err == nil {
				t.Fatal("invalid installation accepted")
			}
			if _, err := os.Stat(filepath.Join(e.paths.Runtime, "ready")); !os.IsNotExist(err) {
				t.Fatal("failed setup became active")
			}
		})
	}
}
