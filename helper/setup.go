package helper

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

//go:embed legacy/*.sh
var legacyHelpers embed.FS

type setupRecord struct {
	Schema int               `json:"schema"`
	Files  map[string]string `json:"files"`
}
type configSnapshot struct {
	ID      string `json:"id"`
	Present bool   `json:"present"`
	Data    []byte `json:"data,omitempty"`
	Link    string `json:"link,omitempty"`
	Mode    uint32 `json:"mode"`
}
type setupJournal struct {
	Schema   int              `json:"schema"`
	Previous []configSnapshot `json:"previous"`
	Wanted   setupRecord      `json:"wanted"`
}

func (e *Engine) configPaths() map[string]string {
	return map[string]string{
		"on-boot": filepath.Join(e.paths.Base, "on-boot.sh"), "connect": filepath.Join(e.paths.Base, "connect.sh"),
		"login": filepath.Join(e.paths.Base, "login.sh"), "diagnose": filepath.Join(e.paths.Base, "diagnose.sh"),
		"boot": filepath.Join(e.paths.Base, "boot.sh"), "rule": e.paths.Rules,
		"cli-link": filepath.Join(e.paths.Links, "tailscale"), "daemon-link": filepath.Join(e.paths.Links, "tailscaled"),
		"setup":       filepath.Join(e.paths.Runtime, "setup.json"),
		"core-helper": e.paths.CoreLink,
	}
}

func snapshot(id, filename string) (configSnapshot, error) {
	s := configSnapshot{ID: id}
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.Present = true
	s.Mode = uint32(info.Mode().Perm())
	if info.Mode()&os.ModeSymlink != 0 {
		s.Link, err = os.Readlink(filename)
		return s, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return s, errors.New("unexpected setup file")
	}
	s.Data, err = os.ReadFile(filename)
	return s, err
}

func applySnapshot(filename string, s configSnapshot) error {
	if !s.Present {
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return err
		}
		return syncDirectory(filepath.Dir(filename))
	}
	if s.Link != "" {
		tmp, err := os.CreateTemp(filepath.Dir(filename), ".k-tailscale-link-*")
		if err != nil {
			return err
		}
		name := tmp.Name()
		tmp.Close()
		os.Remove(name)
		defer os.Remove(name)
		if err := os.Symlink(s.Link, name); err != nil {
			return err
		}
		if err := os.Rename(name, filename); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(filename))
	}
	return writeAtomic(filename, s.Data, os.FileMode(s.Mode))
}

func (e *Engine) matchesSetup(record setupRecord) bool {
	if record.Schema != 2 || len(record.Files) == 0 {
		return false
	}
	paths := e.configPaths()
	for id, expected := range record.Files {
		filename, ok := paths[id]
		if !ok || id == "setup" {
			return false
		}
		s, err := snapshot(id, filename)
		if err != nil || !s.Present {
			return false
		}
		value := digest(s.Data)
		if s.Link != "" {
			value = "link:" + s.Link
		}
		if value != expected {
			return false
		}
	}
	return true
}

func (e *Engine) recoverSetup() error {
	filename := filepath.Join(e.paths.Runtime, "setup-journal.json")
	var journal setupJournal
	if err := readJSON(filename, &journal); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if journal.Schema != 1 || len(journal.Previous) > 10 {
		return errors.New("invalid setup recovery journal")
	}
	ready, _ := os.ReadFile(filepath.Join(e.paths.Runtime, "ready"))
	var installed setupRecord
	metadataCommitted := readJSON(filepath.Join(e.paths.Runtime, "setup.json"), &installed) == nil && installed.Schema == 2 && maps.Equal(installed.Files, journal.Wanted.Files)
	if string(ready) == "storage-v1\n" && metadataCommitted && e.matchesSetup(journal.Wanted) {
		if err := os.Remove(filename); err != nil {
			return err
		}
		return syncDirectory(e.paths.Runtime)
	}
	paths := e.configPaths()
	seen := map[string]bool{}
	for _, previous := range journal.Previous {
		path, ok := paths[previous.ID]
		if !ok || seen[previous.ID] {
			return errors.New("invalid setup recovery path")
		}
		seen[previous.ID] = true
		if err := applySnapshot(path, previous); err != nil {
			return fmt.Errorf("setup recovery failed; backups retained: %w", err)
		}
	}
	if err := os.Remove(filename); err != nil {
		return err
	}
	return syncDirectory(e.paths.Runtime)
}

func (e *Engine) recognizedRuntime() (bool, error) {
	info, err := os.Lstat(e.paths.Runtime)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	owner, _ := os.ReadFile(filepath.Join(e.paths.Runtime, "owner"))
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || string(owner) != "storage-v1\n" {
		return false, errors.New("unknown system runtime; it was not changed")
	}
	if err := ensureDirectory(e.paths.Runtime, 0700); err != nil {
		return false, err
	}
	if err := e.recoverSetup(); err != nil {
		return false, err
	}
	ready, err := os.ReadFile(filepath.Join(e.paths.Runtime, "ready"))
	if os.IsNotExist(err) {
		if entries, stateErr := os.ReadDir(filepath.Join(e.paths.Runtime, "state")); (stateErr == nil && len(entries) > 0) || (stateErr != nil && !os.IsNotExist(stateErr)) {
			return false, errors.New("uncommitted runtime contains a saved identity; it was not replaced")
		}
		return false, nil
	}
	if err != nil || string(ready) != "storage-v1\n" {
		return false, errors.New("unrecognized system runtime marker")
	}
	var record setupRecord
	err = readJSON(filepath.Join(e.paths.Runtime, "setup.json"), &record)
	if err == nil {
		if !e.matchesSetup(record) {
			return false, errors.New("installed setup files changed; custom configuration was retained")
		}
		return true, nil
	}
	if !os.IsNotExist(err) {
		return false, err
	}
	for _, name := range []string{"on-boot", "connect", "login", "diagnose"} {
		expected, _ := legacyHelpers.ReadFile("legacy/" + name + ".sh")
		actual, err := os.ReadFile(filepath.Join(e.paths.Base, name+".sh"))
		known := err == nil && string(actual) == string(expected)
		if !known && err == nil && name == "connect" {
			for _, generation := range []string{"connect-v2.sh", "connect-v3.sh"} {
				current, _ := legacyHelpers.ReadFile("legacy/" + generation)
				known = known || string(actual) == string(current)
			}
		}
		if !known {
			return false, errors.New("unrecognized previous setup; custom helpers were retained")
		}
	}
	return true, nil
}

func (e *Engine) validateRuntimeDirectories() error {
	names := []string{"bin", "state"}
	if e.paths.ramMode() {
		names = []string{"state"}
		layout, err := e.readLayout()
		if err != nil {
			return err
		}
		if !e.ramPairReady(layout.Current) {
			return errors.New("verified RAM executable pair is unavailable")
		}
	}
	for _, name := range names {
		filename := filepath.Join(e.paths.Runtime, name)
		info, err := os.Lstat(filename)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("runtime directories must remain on system storage")
		}
		if err := ensureDirectory(filename, 0700); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) supportedDevice() error {
	_, err := e.deviceProfile()
	return err
}

func (e *Engine) desiredSetup() (map[string]configSnapshot, error) {
	profile, err := e.deviceProfile()
	if err != nil {
		return nil, err
	}
	desired := map[string]configSnapshot{}
	for name, source := range map[string]string{"on-boot": "runtime-boot.sh", "connect": "connect-root.sh", "login": "login-root.sh", "diagnose": "diagnose-root.sh"} {
		data, err := os.ReadFile(filepath.Join(e.paths.Helpers, source))
		if err != nil {
			return nil, err
		}
		if name == "connect" {
			// Specialize the known template during setup. Runtime connection
			// actions must not read the version file on books storage.
			const argument = "--hostname=kobo-libra-colour"
			if strings.Count(string(data), argument) != 1 {
				return nil, errors.New("connection template has an invalid hostname argument")
			}
			modelArgs := "--hostname=" + profile.hostname
			if profile.netfilterMode != "" {
				modelArgs += " --netfilter-mode=" + profile.netfilterMode
			}
			data = []byte(strings.Replace(string(data), argument, modelArgs, 1))
			if e.paths.ramMode() {
				const oldStartup = "if ! pidof tailscaled >/dev/null 2>&1; then\n    /bin/sh /usr/local/tailscale/boot.sh 3>&- || exit 1\nfi"
				if strings.Count(string(data), oldStartup) != 1 {
					return nil, errors.New("connection template has an invalid service readiness check")
				}
				// A process may exist while LocalAPI/configuration is still
				// starting. The serialized helper waits for actual readiness.
				data = []byte(strings.Replace(string(data), oldStartup, "/bin/sh /usr/local/tailscale/boot.sh 3>&- || exit 1", 1))
				const oldWiFiLoop = `while ! awk '$1 ~ /^wlan/ && $2 == "00000000" && $3 != "00000000" { ready=1 } END { exit !ready }' /proc/net/route; do`
				if strings.Count(string(data), oldWiFiLoop) != 1 {
					return nil, errors.New("connection template has an invalid Wi-Fi readiness loop")
				}
				data = []byte(strings.Replace(string(data), oldWiFiLoop, "while ! "+shellQuote(e.paths.CoreLink)+" wifi-ready >/dev/null 2>&1; do", 1))
				data = []byte(strings.ReplaceAll(string(data), "\"$RUNTIME/bin/tailscale\"",
					shellQuote(filepath.Join(e.paths.binaryDir(), "tailscale"))))
			}
		}
		if name == "diagnose" && e.paths.ramMode() {
			data = []byte(strings.ReplaceAll(string(data), "/usr/local/tailscale-runtime/bin/tailscale", filepath.Join(e.paths.binaryDir(), "tailscale")))
			data = []byte(strings.ReplaceAll(string(data), "System storage is active.", "Verified RAM execution is active; private state uses system storage."))
		}
		desired[name] = configSnapshot{ID: name, Present: true, Data: data, Mode: 0755}
	}
	if _, err := os.Lstat(filepath.Join(e.paths.Base, "boot.sh")); os.IsNotExist(err) {
		desired["boot"] = configSnapshot{ID: "boot", Present: true, Data: []byte("#!/bin/sh\nexec /bin/sh " + filepath.Join(e.paths.Base, "on-boot.sh") + "\n"), Mode: 0755}
	} else if err != nil {
		return nil, err
	}
	var rules strings.Builder
	for _, name := range profile.wifiInterfaces {
		fmt.Fprintf(&rules, "ACTION==\"add\", SUBSYSTEM==\"net\", KERNEL==\"%s\", RUN+=\"/bin/sh %s\"\n", name, filepath.Join(e.paths.Base, "on-boot.sh"))
	}
	desired["rule"] = configSnapshot{ID: "rule", Present: true, Mode: 0644, Data: []byte(rules.String())}
	if existing, err := snapshot("rule", e.paths.Rules); err != nil {
		return nil, err
	} else if existing.Present && (existing.Link != "" || string(existing.Data) != string(desired["rule"].Data)) {
		var owned setupRecord
		if readJSON(filepath.Join(e.paths.Runtime, "setup.json"), &owned) != nil || owned.Files["rule"] != digest(existing.Data) {
			return nil, errors.New("an unrecognized startup rule was retained")
		}
	}
	for id, name := range map[string]string{"cli-link": "tailscale", "daemon-link": "tailscaled"} {
		target := filepath.Join(e.paths.binaryDir(), name)
		existing, err := snapshot(id, filepath.Join(e.paths.Links, name))
		if err != nil {
			return nil, err
		}
		if existing.Present && existing.Link != target {
			var owned setupRecord
			knownOld := e.paths.ramMode() && existing.Link == filepath.Join(e.paths.legacyBinDir(), name) &&
				readJSON(filepath.Join(e.paths.Runtime, "setup.json"), &owned) == nil &&
				owned.Files[id] == "link:"+existing.Link
			if !knownOld {
				return nil, errors.New("an unrecognized Tailscale binary path was retained")
			}
		}
		desired[id] = configSnapshot{ID: id, Present: true, Link: target}
	}
	existing, err := snapshot("core-helper", e.paths.CoreLink)
	if err != nil {
		return nil, err
	}
	if existing.Present && existing.Link != e.paths.CandidateHelper {
		var owned setupRecord
		if existing.Link == "" || readJSON(filepath.Join(e.paths.Runtime, "setup.json"), &owned) != nil || owned.Files["core-helper"] != "link:"+existing.Link {
			return nil, errors.New("unrecognized core helper path was retained")
		}
	}
	desired["core-helper"] = configSnapshot{ID: "core-helper", Present: true, Link: e.paths.CandidateHelper}
	return desired, nil
}

func (e *Engine) Prepare(ctx context.Context) error {
	if e.paths.ramMode() {
		return e.prepareBooks(ctx)
	}
	return e.prepareSystem(ctx)
}

func (e *Engine) prepareSystem(ctx context.Context) error {
	unlock, err := acquireActionLock(e.paths.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	unlockService, err := acquireServiceLock(ctx, e.paths.ServiceLock)
	if err != nil {
		return err
	}
	defer unlockService()
	current, err := e.recognizedRuntime()
	if err != nil {
		return err
	}
	if !current {
		if err := e.supportedDevice(); err != nil {
			return err
		}
		if entries, err := os.ReadDir(e.paths.Base); err == nil && len(entries) > 0 {
			return errors.New("existing boot setup was retained; layout is not recognized")
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		if err := e.recoverAddon(); err != nil {
			return err
		}
		if err := e.recoverUpdate(ctx, false); err != nil {
			return err
		}
		if err := e.validateRuntimeDirectories(); err != nil {
			return err
		}
		cli, err := e.service.Version(ctx, filepath.Join(e.paths.Runtime, "bin/tailscale"))
		if err != nil {
			return err
		}
		daemon, err := e.service.Version(ctx, filepath.Join(e.paths.Runtime, "bin/tailscaled"))
		if err != nil {
			return err
		}
		if !stableVersion(cli) || cli != daemon {
			return errors.New("the active executable pair is incomplete or mismatched")
		}
	}
	for _, dir := range []string{e.paths.Base, filepath.Dir(e.paths.CoreLink)} {
		if err := ensureDirectory(dir, 0755); err != nil {
			return err
		}
	}
	profile, err := e.deviceProfile()
	if err != nil {
		return err
	}
	if err := ensureSystemDirectory(e.paths.Links, profile.linksMode); err != nil {
		return err
	}
	if err := ensureSystemDirectory(filepath.Dir(e.paths.Rules), 0755); err != nil {
		return err
	}
	desired, err := e.desiredSetup()
	if err != nil {
		return err
	}
	if !current {
		for id := range desired {
			if id == "cli-link" || id == "daemon-link" || id == "core-helper" {
				continue
			}
			old, err := snapshot(id, e.configPaths()[id])
			if err != nil {
				return err
			}
			if old.Present {
				return errors.New("existing setup files were retained; layout is not recognized")
			}
		}
		var payload Artifact
		if err := readJSON(filepath.Join(e.paths.Payload, "release.json"), &payload); err != nil {
			return err
		}
		artifact, err := e.upstream.VerifyLocal(filepath.Join(e.paths.Payload, "release.tgz"), payload.Version, payload.Proof)
		if err != nil {
			return err
		}
		info, err := os.Stat(artifact.Path)
		if err != nil {
			return err
		}
		// Discard only owned failed fresh-install files, after proving no state exists.
		if _, err := os.Stat(filepath.Join(e.paths.Runtime, "owner")); err == nil {
			for _, name := range []string{"bin", "bin.new", "install.tgz"} {
				if err := os.RemoveAll(filepath.Join(e.paths.Runtime, name)); err != nil {
					return err
				}
			}
		}
		available, err := e.free(filepath.Dir(e.paths.Runtime))
		if err != nil {
			return err
		}
		if available < artifact.Expanded+info.Size()+spaceReserve {
			return errors.New("not enough system space for the full Tailscale installation")
		}
		if err := ensureDirectory(e.paths.Runtime, 0700); err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(e.paths.Runtime, "owner"), []byte("storage-v1\n"), 0600); err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(e.paths.Runtime, "installer-owner"), []byte("k-tailscale-v2\n"), 0600); err != nil {
			return err
		}
		cache := filepath.Join(e.paths.Runtime, "install.tgz")
		if err := os.Remove(cache); err != nil && !os.IsNotExist(err) {
			return err
		}
		defer os.Remove(cache)
		if err := copyPrivate(artifact.Path, cache, maxDownload); err != nil {
			return err
		}
		defer os.Remove(cache)
		artifact, err = e.upstream.VerifyLocal(cache, artifact.Version, artifact.Proof)
		if err != nil {
			return err
		}
		stage := filepath.Join(e.paths.Runtime, "bin.new")
		if err := os.RemoveAll(stage); err != nil {
			return err
		}
		if err := extractArtifact(ctx, artifact, stage); err != nil {
			return err
		}
		for _, name := range []string{"tailscale", "tailscaled"} {
			version, err := e.service.Version(ctx, filepath.Join(stage, name))
			if err != nil {
				return err
			}
			if version != artifact.Version {
				return errors.New("staged executable version mismatch")
			}
		}
		if err := os.RemoveAll(filepath.Join(e.paths.Runtime, "bin")); err != nil {
			return err
		}
		if err := os.Rename(stage, filepath.Join(e.paths.Runtime, "bin")); err != nil {
			return err
		}
		if err := ensureDirectory(filepath.Join(e.paths.Runtime, "state"), 0700); err != nil {
			return err
		}
	}
	if current {
		if running, err := e.service.Running(ctx); err != nil {
			return err
		} else if running {
			if err := e.service.DisableNativeUpdates(ctx); err != nil {
				return fmt.Errorf("cannot enforce manual updates: %w", err)
			}
		}
	}
	return e.commitSetup(ctx, desired, true)
}

func (e *Engine) commitSetup(ctx context.Context, desired map[string]configSnapshot, clearPayload bool) error {
	paths := e.configPaths()
	if err := ctx.Err(); err != nil {
		return err
	}
	journal := setupJournal{Schema: 1, Wanted: setupRecord{Schema: 2, Files: map[string]string{}}}
	for id, entry := range desired {
		old, err := snapshot(id, paths[id])
		if err != nil {
			return err
		}
		journal.Previous = append(journal.Previous, old)
		value := digest(entry.Data)
		if entry.Link != "" {
			value = "link:" + entry.Link
		}
		journal.Wanted.Files[id] = value
	}
	previous, err := snapshot("setup", paths["setup"])
	if err != nil {
		return err
	}
	journal.Previous = append(journal.Previous, previous)
	if err := writeJSON(filepath.Join(e.paths.Runtime, "setup-journal.json"), journal); err != nil {
		return err
	}
	for id, entry := range desired {
		if err := applySnapshot(paths[id], entry); err != nil {
			recoveryErr := e.recoverSetup()
			return errors.Join(err, recoveryErr)
		}
	}
	if err := writeJSON(paths["setup"], journal.Wanted); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(e.paths.Runtime, "installer-owner"), []byte("k-tailscale-v2\n"), 0600); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(e.paths.Runtime, "ready"), []byte("storage-v1\n"), 0600); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(e.paths.Runtime, "setup-journal.json")); err != nil {
		return err
	}
	if err := syncDirectory(e.paths.Runtime); err != nil {
		return err
	}
	if !clearPayload {
		return nil
	}
	for _, name := range []string{"release.json", "release.tgz"} {
		if err := os.Remove(filepath.Join(e.paths.Payload, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
