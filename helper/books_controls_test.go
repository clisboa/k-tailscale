package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBooksStatusLogoutAndReportRespectUSBExport(t *testing.T) {
	e, base := setupFixture(t)
	enableRAMFixture(t, e)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	base.running = true
	if err := e.StorageReport(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(e.paths.Books, "storage-report.json"))
	if err != nil || !strings.Contains(string(data), "verified_ram_ready") || strings.Contains(string(data), "identity") {
		t.Fatal("invalid support report", err)
	}
	e.reportSetup(context.Background(), nil)
	publicReport := filepath.Join(e.paths.Books, "migration-status.txt")
	before, err := os.ReadFile(publicReport)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BeginUSB(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.StorageReport(context.Background()); err == nil {
		t.Fatal("storage report accessed exported volume")
	}
	e.reportSetup(context.Background(), os.ErrPermission)
	after, err := os.ReadFile(publicReport)
	if err != nil || string(before) != string(after) {
		t.Fatal("setup report wrote during export", err)
	}
	if err := os.Rename(e.paths.Books, e.paths.Books+"-unmounted"); err != nil {
		t.Fatal(err)
	}
	status, err := e.Status(context.Background())
	if err != nil || status.Current != "1.104.4" {
		t.Fatal("status depends on books or legacy bin", err)
	}
	if err := e.SignOut(context.Background()); err != nil {
		t.Fatal("logout depends on books or legacy bin", err)
	}
}

func TestBooksGeneratedRealConnectionHelpersUseRAM(t *testing.T) {
	e, _ := setupFixture(t)
	enableRAMFixture(t, e)
	for _, name := range []string{"connect-root.sh", "diagnose-root.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(e.paths.Helpers, name), data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"connect.sh", "diagnose.sh"} {
		data, err := os.ReadFile(filepath.Join(e.paths.Base, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "connect.sh" && (!strings.Contains(string(data), " wifi-ready >/dev/null 2>&1") || strings.Contains(string(data), "$1 ~ /^wlan/")) {
			t.Fatal("generated connection command still guesses the wireless interface name")
		}
		if name == "connect.sh" && strings.Contains(string(data), "if ! pidof tailscaled") {
			t.Fatal("connection can race a daemon that has not finished becoming ready")
		}
		if strings.Contains(string(data), "$RUNTIME/bin/tailscale") || strings.Contains(string(data), "/usr/local/tailscale-runtime/bin/tailscale") || !strings.Contains(string(data), e.paths.binaryDir()) {
			t.Fatal("generated helper does not execute RAM pair", name)
		}
	}
}

func TestAddonExactSpacePreflightBeforeCandidateWrites(t *testing.T) {
	e, manifest, archive := addonFixture(t)
	expanded, err := readAddon(archive, "", manifest.Version)
	if err != nil || expanded <= 0 {
		t.Fatal(err)
	}
	e.free = func(string) (int64, error) { return spaceReserve + expanded + 1<<20 - 1, nil }
	if err := e.applyAddon(archive, manifest); err == nil {
		t.Fatal("insufficient exact space accepted")
	}
	if _, err := os.Stat(filepath.Join(e.updateRoot(), "addon-transaction/candidate")); !os.IsNotExist(err) {
		t.Fatal("space failure wrote candidate")
	}
	e.free = func(string) (int64, error) { return spaceReserve + expanded + 1<<20, nil }
	if err := e.applyAddon(archive, manifest); err != nil {
		t.Fatal("exact budget was rejected", err)
	}
}

func TestAtomicLinksRetainImmutableRollbackContents(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "live")
	if err := os.WriteFile(source, []byte("checkpoint"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkAtomic(source, target); err != nil {
		t.Fatal(err)
	}
	a, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(target)
	if err != nil || !os.SameFile(a, b) {
		t.Fatal("atomic replacement duplicated file contents", err)
	}
	if err := writeAtomic(target, []byte("changed live file"), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil || string(data) != "checkpoint" {
		t.Fatal("checkpoint was consumed or modified", err)
	}
}

func TestBooksMigrationRetainsPendingLegacyUpdateRecovery(t *testing.T) {
	e, _, candidate := updateFixture(t, false)
	if err := e.activateTailscale(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	enableRAMFixture(t, e)
	if err := e.Prepare(context.Background()); err == nil || !strings.Contains(err.Error(), "pending system update") {
		t.Fatal("migration bypassed deferred legacy validation", err)
	}
	if _, err := os.Stat(e.updateJournalPath()); err != nil {
		t.Fatal("migration discarded legacy recovery", err)
	}
	if _, err := os.Stat(e.paths.legacyBinDir()); err != nil {
		t.Fatal("migration discarded legacy executables", err)
	}
	if _, err := os.Stat(e.paths.layoutPath()); !os.IsNotExist(err) {
		t.Fatal("migration selected unvalidated release")
	}
}

func TestHelperRetentionRemovesOnlyPromotedOwnedRelease(t *testing.T) {
	e, _ := setupFixture(t)
	base := filepath.Join(filepath.Dir(e.paths.CoreLink), "releases")
	old := filepath.Join(base, "0.1.8/helper")
	for _, name := range []string{"helper", "THIRD_PARTY_NOTICES.txt"} {
		path := filepath.Join(filepath.Dir(old), name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("owned previous release"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(e.paths.CoreLink), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, e.paths.CoreLink); err != nil {
		t.Fatal(err)
	}
	e.retirePreviousHelper(old)
	if _, err := os.Stat(old); err != nil {
		t.Fatal("active predecessor removed", err)
	}
	if err := os.Remove(e.paths.CoreLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.paths.CandidateHelper, e.paths.CoreLink); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(filepath.Dir(old), "custom-file")
	if err := os.WriteFile(extra, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	e.retirePreviousHelper(old)
	if _, err := os.Stat(old); err != nil {
		t.Fatal("custom release directory removed", err)
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	e.retirePreviousHelper(old)
	if _, err := os.Stat(filepath.Dir(old)); !os.IsNotExist(err) {
		t.Fatal("inactive owned predecessor accumulated")
	}
}
