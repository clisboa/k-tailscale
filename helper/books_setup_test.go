package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func enableRAMFixture(t *testing.T, e *Engine) {
	t.Helper()
	root, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(e.paths.Runtime)))
	if err != nil {
		t.Fatal(err)
	}
	e.paths.Books = filepath.Join(root, "public-store")
	e.paths.RAM = filepath.Join(root, "ram-runtime")
	e.paths.USBBlock = filepath.Join(root, "usb-exporting")
	if err := os.MkdirAll(e.paths.Books, 0755); err != nil {
		t.Fatal(err)
	}
	e.ramCheck = func(string) error { return nil }
	e.memory = func() (int64, error) { return 1 << 30, nil }
	e.free = func(path string) (int64, error) {
		if strings.HasPrefix(path, e.paths.RAM) || strings.HasPrefix(path, e.paths.Books) {
			return 1 << 30, nil
		}
		return 35 << 20, nil
	}
}

func TestBooksSetupUsesSmallSystemAndPrivateIdentityOnly(t *testing.T) {
	e, service := setupFixture(t)
	enableRAMFixture(t, e)
	if err := os.WriteFile(e.paths.Version, []byte("a,b,4.38.23852,d,e,376"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	layout, err := e.readLayout()
	if err != nil || layout.Current.Version != "1.104.4" {
		t.Fatal("protected release record missing", err)
	}
	if _, err := os.Stat(e.paths.legacyBinDir()); !os.IsNotExist(err) {
		t.Fatal("binaries still occupy system storage")
	}
	for name, expected := range layout.Current.Files {
		for _, directory := range []string{e.paths.binaryDir(), filepath.Join(e.paths.Books, layout.Current.Version)} {
			data, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil || digest(data) != expected.SHA256 {
				t.Fatal("public/RAM pair differs from protected pin", err)
			}
		}
	}
	rules, err := os.ReadFile(e.paths.Rules)
	if err != nil || !strings.Contains(string(rules), `KERNEL=="eth0"`) || !strings.Contains(string(rules), `KERNEL=="wlan*"`) {
		t.Fatal("Clara wireless boot rule missing", err)
	}
	connect, err := os.ReadFile(filepath.Join(e.paths.Base, "connect.sh"))
	if err != nil || !strings.Contains(string(connect), "--hostname=kobo-clara-hd") {
		t.Fatal("Clara hostname missing", err)
	}
	link, err := os.Readlink(filepath.Join(e.paths.Links, "tailscale"))
	if err != nil || link != filepath.Join(e.paths.binaryDir(), "tailscale") {
		t.Fatal("CLI link does not select RAM", err)
	}
	if err := os.WriteFile(filepath.Join(e.paths.Runtime, "state/identity"), []byte("private identity"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(e.paths.Books, e.paths.Books+"-unmounted"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(e.paths.USBBlock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := e.StartService(context.Background()); err != nil {
		t.Fatal("RAM startup depended on books storage", err)
	}
	if service.starts != 1 || !service.running {
		t.Fatal("RAM daemon did not start")
	}
}

func TestBooksSetupMigratesRecognizedSystemWithoutReplacingIdentity(t *testing.T) {
	e, service := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(e.paths.Runtime, "state/identity")
	if err := os.WriteFile(state, []byte("saved disconnected identity"), 0600); err != nil {
		t.Fatal(err)
	}
	enableRAMFixture(t, e)
	service.running = true
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(state)
	if err != nil || string(data) != "saved disconnected identity" {
		t.Fatal("migration replaced private identity", err)
	}
	if service.stops != 1 || service.starts != 1 || !service.running {
		t.Fatal("migration did not restore the prior daemon state")
	}
	if _, err := os.Stat(e.paths.legacyBinDir()); !os.IsNotExist(err) {
		t.Fatal("successful migration retained system binary copies")
	}
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal("second setup was not idempotent", err)
	}
	if service.stops != 1 || service.starts != 1 {
		t.Fatal("idempotent setup restarted an unchanged daemon")
	}
}

func TestBooksSetupRejectsAlteredPayloadBeforeExecution(t *testing.T) {
	e, service := setupFixture(t)
	enableRAMFixture(t, e)
	path := filepath.Join(e.paths.Payload, "release.tgz")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.Prepare(context.Background()); err == nil {
		t.Fatal("altered payload installed")
	}
	if service.starts != 0 {
		t.Fatal("altered payload executed")
	}
	if _, err := os.Stat(e.paths.layoutPath()); !os.IsNotExist(err) {
		t.Fatal("unverified release was selected")
	}
}

func TestBooksMigrationFailureRetainsSystemPairAndIdentityForRetry(t *testing.T) {
	e, service := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(e.paths.Runtime, "state/identity")
	if err := os.WriteFile(state, []byte("unchanged identity"), 0600); err != nil {
		t.Fatal(err)
	}
	enableRAMFixture(t, e)
	service.running = true
	service.failStart = true
	if err := e.Prepare(context.Background()); err == nil {
		t.Fatal("failed RAM daemon startup was accepted")
	}
	if _, err := os.Stat(filepath.Join(e.paths.legacyBinDir(), "tailscaled")); err != nil {
		t.Fatal("failed migration removed system rollback executable", err)
	}
	data, err := os.ReadFile(state)
	if err != nil || string(data) != "unchanged identity" {
		t.Fatal("failed migration changed identity", err)
	}
	service.failStart = false
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal("migration recovery/retry failed", err)
	}
	if !service.running {
		t.Fatal("retry did not restore prior daemon state")
	}
}

func TestBooksColdStartupAuthenticatesPublicCacheBeforeDaemon(t *testing.T) {
	e, service := setupFixture(t)
	enableRAMFixture(t, e)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	layout, err := e.readLayout()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(e.paths.RAM); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.paths.Books, layout.Current.Version, "tailscaled")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[0] ^= 1
	if err := os.WriteFile(path, data, 0666); err != nil {
		t.Fatal(err)
	}
	if digest(data) == layout.Current.Files["tailscaled"].SHA256 {
		t.Fatal("tampering fixture did not change the pinned bytes")
	}
	if e.ramPairReady(layout.Current) {
		t.Fatal("removed RAM cache was reported ready")
	}
	if err := e.loadRAMPair(context.Background(), layout.Current); err == nil {
		t.Fatal("direct cold cache load accepted tampering")
	}
	if err := e.StartService(context.Background()); err == nil {
		t.Fatal("USB-visible tampering was executed after RAM loss")
	}
	if service.starts != 0 {
		t.Fatal("unverified cache reached the daemon launcher")
	}
}
