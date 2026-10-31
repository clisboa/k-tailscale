package helper

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAddonSignatureAndVersionBoundary(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	s := addonSource{key: pub, base: projectReleaseBase}
	m := addonManifest{Schema: 1, Version: "0.2.0", MinHelper: "0.1.0", Size: 123, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", URL: "https://github.com/clisboa/k-tailscale/releases/download/v0.2.0/k-tailscale-update.tgz"}
	data, _ := json.Marshal(m)
	sig := ed25519.Sign(key, data)
	if _, err := s.verifyManifest(data, sig); err != nil {
		t.Fatal(err)
	}
	sig[0] ^= 1
	if _, err := s.verifyManifest(data, sig); err == nil {
		t.Fatal("bad signature accepted")
	}
	for _, url := range []string{"http://evil.invalid/update.tgz", "https://github.com/noesiscarloslisboa/k-tailscale/releases/download/v0.2.0/k-tailscale-update.tgz", projectReleaseBase + "download/v0.1.0/k-tailscale-update.tgz"} {
		m.URL = url
		data, _ = json.Marshal(m)
		if _, err := s.verifyManifest(data, ed25519.Sign(key, data)); err == nil {
			t.Fatal("unbound URL accepted")
		}
	}
	if !newerVersion("0.2.0", "0.1.0") || newerVersion("0.1.0", "0.2.0") || stableVersion("0.2.0") {
		t.Fatal("addon and upstream versions conflated")
	}
}
func addonFixture(t *testing.T) (*Engine, addonManifest, string) {
	t.Helper()
	e, _, _ := updateFixture(t, true)
	e.paths.SystemRoot = t.TempDir()
	m := addonManifest{Version: "0.2.0"}
	files := map[string][]byte{}
	for name := range addonScope(m.Version) {
		files[name] = []byte("new addon file")
	}
	files["usr/local/tailscale-ui/release-version"] = []byte(m.Version + "\n")
	files["usr/local/k-tailscale/releases/0.2.0/helper"] = []byte("#!/bin/sh\nprintf '%s\\n' '{\"version\":\"0.2.0\"}'\n")
	// Reuse the tar fixture with exact paths and modes, bypassing only transport.
	archive := filepath.Join(t.TempDir(), "addon.tgz")
	writeAddonArchive(t, archive, files, m.Version)
	for name, mode := range addonScope(m.Version) {
		if name == "usr/local/k-tailscale/releases/0.2.0/helper" || name == "usr/local/k-tailscale/releases/0.2.0/THIRD_PARTY_NOTICES.txt" {
			continue
		}
		target := e.addonPath(name)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("old addon file"), mode); err != nil {
			t.Fatal(err)
		}
	}
	return e, m, archive
}
func TestAddonAppliesOnlyOwnedFilesAndKeepsTailscale(t *testing.T) {
	e, m, a := addonFixture(t)
	if err := e.applyAddon(a, m); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(e.addonPath("usr/local/tailscale-ui/control.sh"))
	if string(data) != "new addon file" {
		t.Fatal("addon not applied")
	}
	identity, _ := os.ReadFile(filepath.Join(e.paths.Runtime, "state/identity"))
	if string(identity) != "saved disconnected identity" {
		t.Fatal("addon changed identity")
	}
	if _, err := os.Stat(filepath.Join(e.updateRoot(), "restart-required")); err != nil {
		t.Fatal(err)
	}
}
func TestAddonRecoveryRetainsImmutableBackups(t *testing.T) {
	e, m, a := addonFixture(t)
	if err := e.applyAddon(a, m); err != nil {
		t.Fatal(err)
	}
	txn := filepath.Join(e.updateRoot(), "addon-transaction")
	j := addonJournal{Schema: 1, Phase: "applying", Version: m.Version}
	for name, mode := range addonScope(m.Version) {
		backup := filepath.Join(txn, "backup", name)
		if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(backup, []byte("before interruption"), mode); err != nil {
			t.Fatal(err)
		}
		j.Files = append(j.Files, addonFile{Name: name, Present: true, Mode: uint32(mode)})
	}
	if err := writeJSON(e.addonJournalPath(), j); err != nil {
		t.Fatal(err)
	}
	if err := e.recoverAddon(); err != nil {
		t.Fatal(err)
	}
	for name := range addonScope(m.Version) {
		data, _ := os.ReadFile(e.addonPath(name))
		if string(data) != "before interruption" {
			t.Fatal("backup not restored", name)
		}
	}
	if err := e.recoverAddon(); err != nil {
		t.Fatal(err)
	}
}
func TestAddonRejectsTraversalAndWrongScope(t *testing.T) {
	e, m, a := addonFixture(t)
	writeAddonArchive(t, a, map[string][]byte{"../../etc/evil": []byte("x")}, m.Version)
	if err := e.applyAddon(a, m); err == nil {
		t.Fatal("unsafe archive accepted")
	}
}

func writeAddonArchive(t *testing.T, path string, files map[string][]byte, v string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	w := tar.NewWriter(gz)
	for name, data := range files {
		mode := addonScope(v)[name]
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: int64(mode), Size: int64(len(data))}); err != nil {
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
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAddonRollbackCleanupAfterBackupsAreGone(t *testing.T) {
	e, m, a := addonFixture(t)
	if err := e.applyAddon(a, m); err != nil {
		t.Fatal(err)
	}
	j := addonJournal{Schema: 1, Phase: "rolled-back", Version: m.Version}
	for name, mode := range addonScope(m.Version) {
		j.Files = append(j.Files, addonFile{Name: name, Present: true, Mode: uint32(mode)})
	}
	if err := writeJSON(e.addonJournalPath(), j); err != nil {
		t.Fatal(err)
	}
	if err := e.recoverAddon(); err != nil {
		t.Fatal("completed rollback still depends on deleted backups", err)
	}
	if _, err := os.Stat(e.addonJournalPath()); !os.IsNotExist(err) {
		t.Fatal("cleanup journal retained")
	}
}
