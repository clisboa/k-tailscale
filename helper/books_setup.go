package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type ramSetupJournal struct {
	Schema     int              `json:"schema"`
	Phase      string           `json:"phase"`
	WasRunning bool             `json:"was_running"`
	Previous   []configSnapshot `json:"previous"`
}

func (e *Engine) ramSetupJournalPath() string {
	return filepath.Join(e.paths.Runtime, "ram-setup.json")
}

func (e *Engine) legacyEngine() *Engine {
	legacy := *e
	paths := e.paths
	paths.Books, paths.RAM, paths.USBScript, paths.USBBlock, paths.BooksMount = "", "", "", "", ""
	legacy.paths = paths
	if service, ok := e.service.(*osService); ok {
		copy := *service
		copy.paths = paths
		legacy.service = &copy
	}
	return &legacy
}

func (e *Engine) ramSetupPaths() map[string]string {
	paths := e.configPaths()
	paths["layout"] = e.paths.layoutPath()
	paths["ready"] = filepath.Join(e.paths.Runtime, "ready")
	return paths
}

func (e *Engine) recoverRAMSetup(ctx context.Context) error {
	var journal ramSetupJournal
	if err := readJSON(e.ramSetupJournalPath(), &journal); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if journal.Schema != 1 || len(journal.Previous) > len(e.ramSetupPaths()) {
		return errors.New("invalid RAM setup recovery journal")
	}
	if journal.Phase == "committed" {
		for _, previous := range journal.Previous {
			if previous.ID == "core-helper" {
				e.retirePreviousHelper(previous.Link)
			}
		}
		if err := os.RemoveAll(e.paths.legacyBinDir()); err != nil {
			return err
		}
		if err := os.Remove(e.ramSetupJournalPath()); err != nil {
			return err
		}
		return syncDirectory(e.paths.Runtime)
	}
	if journal.Phase != "activating" {
		return errors.New("unknown RAM setup recovery phase")
	}
	if err := e.service.Stop(ctx); err != nil {
		return err
	}
	paths := e.ramSetupPaths()
	seen := map[string]bool{}
	for _, previous := range journal.Previous {
		path, ok := paths[previous.ID]
		if !ok || seen[previous.ID] {
			return errors.New("invalid RAM setup recovery path")
		}
		seen[previous.ID] = true
		if err := applySnapshot(path, previous); err != nil {
			return err
		}
	}
	if journal.WasRunning {
		if err := e.legacyEngine().service.Start(ctx); err != nil {
			return err
		}
	}
	if err := os.Remove(e.ramSetupJournalPath()); err != nil {
		return err
	}
	return syncDirectory(e.paths.Runtime)
}

func (e *Engine) commitRAMSetup(ctx context.Context, pin releasePin, stage string, wasRunning bool) error {
	journal := ramSetupJournal{
		Schema: 1, Phase: "activating", WasRunning: wasRunning, Previous: []configSnapshot{},
	}
	for id, path := range e.ramSetupPaths() {
		previous, err := snapshot(id, path)
		if err != nil {
			return err
		}
		journal.Previous = append(journal.Previous, previous)
	}
	if err := writeJSON(e.ramSetupJournalPath(), journal); err != nil {
		return err
	}
	fail := func(cause error) error {
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return errors.Join(cause, e.recoverRAMSetup(recovery))
	}
	if wasRunning {
		if err := e.service.Stop(ctx); err != nil {
			return fail(err)
		}
	}
	if err := e.activateRAMPair(stage, pin); err != nil {
		return fail(err)
	}
	if err := writeJSON(e.paths.layoutPath(), runtimeLayout{Schema: 1, Current: pin}); err != nil {
		return fail(err)
	}
	desired, err := e.desiredSetup()
	if err != nil {
		return fail(err)
	}
	if err := e.commitSetup(ctx, desired, false); err != nil {
		return fail(err)
	}
	if wasRunning {
		if err := e.startHealthy(ctx, pin.Version); err != nil {
			return fail(err)
		}
	}
	journal.Phase = "committed"
	if err := writeJSON(e.ramSetupJournalPath(), journal); err != nil {
		return err
	}
	if err := e.recoverRAMSetup(ctx); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(e.paths.RAM, "bin.previous"))
}

func (e *Engine) stageTrustedSystemPair(ctx context.Context) (releasePin, string, error) {
	legacy := e.legacyEngine()
	if err := legacy.recoverUpdate(ctx, false); err != nil {
		return releasePin{}, "", err
	}
	if _, err := os.Lstat(legacy.updateJournalPath()); err == nil {
		return releasePin{}, "", errors.New("start Tailscale to validate the pending system update before storage migration")
	} else if !os.IsNotExist(err) {
		return releasePin{}, "", err
	}
	if err := legacy.validateRuntimeDirectories(); err != nil {
		return releasePin{}, "", err
	}
	version, err := legacy.pairVersion(ctx, e.paths.legacyBinDir())
	if err != nil {
		return releasePin{}, "", err
	}
	pin, err := pinDirectory(e.paths.legacyBinDir(), version)
	if err != nil {
		return releasePin{}, "", err
	}
	bytes, err := validatePin(pin)
	if err != nil {
		return releasePin{}, "", err
	}
	if err := e.requireRAM(bytes); err != nil {
		return releasePin{}, "", err
	}
	work, err := os.MkdirTemp(e.paths.RAM, "migration-")
	if err != nil {
		return releasePin{}, "", err
	}
	bin := filepath.Join(work, "bin")
	if err := ensureDirectory(bin, 0700); err != nil {
		os.RemoveAll(work)
		return releasePin{}, "", err
	}
	for name, expected := range pin.Files {
		if err := copyPinned(ctx, filepath.Join(e.paths.legacyBinDir(), name),
			filepath.Join(bin, name), expected, 0755); err != nil {
			os.RemoveAll(work)
			return releasePin{}, "", err
		}
	}
	return pin, bin, nil
}

func (e *Engine) prepareBooks(ctx context.Context) error {
	unlock, err := acquireActionLock(e.paths.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	release, err := acquireServiceLock(ctx, e.paths.ServiceLock)
	if err != nil {
		return err
	}
	defer release()
	var profile deviceProfile
	// Before first provisioning, close the small device-identification read
	// before installing the USB guard. Subsequent setup reads are guarded too.
	if _, guardErr := os.Lstat(e.usbGuardPath()); guardErr == nil {
		err = e.withPublicIO(ctx, func(context.Context) error {
			var readErr error
			profile, readErr = e.deviceProfile()
			return readErr
		})
	} else if !os.IsNotExist(guardErr) {
		return guardErr
	} else {
		profile, err = e.deviceProfile()
	}
	if err != nil {
		return err
	}
	if _, err := os.Lstat(e.paths.Runtime); err == nil {
		if err := ensureDirectory(e.paths.Runtime, 0700); err != nil {
			return err
		}
		owner, err := os.ReadFile(filepath.Join(e.paths.Runtime, "owner"))
		if err != nil || string(owner) != "storage-v1\n" {
			return errors.New("unknown system runtime was retained")
		}
		if err := e.recoverRAMSetup(ctx); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	current, err := e.recognizedRuntime()
	if err != nil {
		return err
	}
	if !current {
		if entries, err := os.ReadDir(e.paths.Base); err == nil && len(entries) != 0 {
			return errors.New("unknown existing boot setup was retained")
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := ensureDirectory(e.paths.Runtime, 0700); err != nil {
		return err
	}
	if err := e.requireSpace(1 << 20); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(e.paths.Runtime, "owner"), []byte("storage-v1\n"), 0600); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(e.paths.Runtime, "installer-owner"), []byte("k-tailscale-v2\n"), 0600); err != nil {
		return err
	}
	for _, dir := range []string{e.paths.Base, filepath.Dir(e.paths.CoreLink)} {
		if err := ensureDirectory(dir, 0755); err != nil {
			return err
		}
	}
	if err := ensureSystemDirectory(e.paths.Links, profile.linksMode); err != nil {
		return err
	}
	if err := ensureSystemDirectory(filepath.Dir(e.paths.Rules), 0755); err != nil {
		return err
	}
	if err := ensureDirectory(filepath.Join(e.paths.Runtime, "state"), 0700); err != nil {
		return err
	}
	if err := e.installUSBGuard(); err != nil {
		return err
	}
	if _, err := e.readLayout(); err == nil {
		if err := e.recoverRAMUpdate(ctx, false); err != nil {
			return err
		}
		layout, err := e.readLayout()
		if err != nil {
			return err
		}
		if err := e.recoverAddon(); err != nil {
			return err
		}
		if err := e.loadRAMPair(ctx, layout.Current); err != nil {
			return err
		}
		return e.withPublicIO(ctx, func(ctx context.Context) error {
			desired, err := e.desiredSetup()
			if err != nil {
				return err
			}
			previous, _ := os.Readlink(e.paths.CoreLink)
			if err := e.commitSetup(ctx, desired, false); err != nil {
				return err
			}
			e.retirePreviousHelper(previous)
			return nil
		})
	} else if !os.IsNotExist(err) {
		return err
	}
	return e.withPublicIO(ctx, func(ctx context.Context) error {
		var pin releasePin
		var bin string
		var err error
		if current {
			pin, bin, err = e.stageTrustedSystemPair(ctx)
		} else {
			var claimed Artifact
			if err := readJSON(filepath.Join(e.paths.Payload, "release.json"), &claimed); err != nil {
				return err
			}
			pin, bin, err = e.stageSignedRelease(ctx, filepath.Join(e.paths.Payload, "release.tgz"), claimed)
		}
		if err != nil {
			return err
		}
		defer os.RemoveAll(filepath.Dir(bin))
		if err := e.persistPublicPair(ctx, bin, pin); err != nil {
			return err
		}
		wasRunning := false
		if current {
			wasRunning, err = e.service.Running(ctx)
			if err != nil {
				return err
			}
		}
		if err := e.commitRAMSetup(ctx, pin, bin, wasRunning); err != nil {
			return err
		}
		for _, name := range []string{"release.json", "release.tgz"} {
			if err := os.Remove(filepath.Join(e.paths.Payload, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})
}

func (e *Engine) startRAMService(ctx context.Context) error {
	release, err := acquireServiceLock(ctx, e.paths.ServiceLock)
	if err != nil {
		return err
	}
	defer release()
	if err := e.recoverRAMSetup(ctx); err != nil {
		return err
	}
	if _, err := os.Lstat(e.paths.layoutPath()); os.IsNotExist(err) {
		return e.legacyEngine().startSystemServiceLocked(ctx)
	} else if err != nil {
		return err
	}
	current, err := e.recognizedRuntime()
	if err != nil {
		return err
	}
	if !current {
		return errors.New("setup must finish before starting the RAM runtime")
	}
	if err := e.recoverRAMUpdate(ctx, true); err != nil {
		return err
	}
	layout, err := e.readLayout()
	if err != nil {
		return err
	}
	if err := e.recoverAddon(); err != nil {
		return err
	}
	if err := e.loadRAMPair(ctx, layout.Current); err != nil {
		return err
	}
	if err := e.validateRuntimeDirectories(); err != nil {
		return err
	}
	if version, err := e.pairVersion(ctx, e.paths.binaryDir()); err != nil || version != layout.Current.Version {
		return errors.Join(err, errors.New("RAM pair does not match the selected release"))
	}
	return e.startHealthy(ctx, layout.Current.Version)
}
