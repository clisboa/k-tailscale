package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Only public executables live on books storage. Both the recovery decision and
// the immutable identity checkpoint remain on the private system partition.
type ramUpdateJournal struct {
	Schema     int        `json:"schema"`
	Phase      string     `json:"phase"`
	Previous   releasePin `json:"previous"`
	Candidate  releasePin `json:"candidate"`
	WasRunning bool       `json:"was_running"`
}

func (e *Engine) ramUpdateJournalPath() string {
	return filepath.Join(e.updateRoot(), "ram-transaction.json")
}
func (e *Engine) ramTransaction() string { return filepath.Join(e.updateRoot(), "ram-transaction") }

func (e *Engine) ramInstalled() (bool, error) {
	if !e.paths.ramMode() {
		return false, nil
	}
	_, err := os.Lstat(e.paths.layoutPath())
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (e *Engine) cleanupRAMUpdate() error {
	if err := os.RemoveAll(e.ramTransaction()); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(e.paths.RAM, "bin.previous")); err != nil {
		return err
	}
	if err := os.Remove(e.ramUpdateJournalPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(e.updateRoot())
}

func (e *Engine) commitRAMUpdate(j ramUpdateJournal) error {
	j.Phase = "committed"
	if err := writeJSON(e.ramUpdateJournalPath(), j); err != nil {
		return err
	}
	return e.cleanupRAMUpdate()
}

func (e *Engine) restoreRAMState() error {
	txn := e.ramTransaction()
	tmp, trash := filepath.Join(txn, "restore-state"), filepath.Join(txn, "discard-state")
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := cloneTree(filepath.Join(txn, "checkpoint"), tmp, false); err != nil {
		return err
	}
	if err := os.RemoveAll(trash); err != nil {
		return err
	}
	live := filepath.Join(e.paths.Runtime, "state")
	if _, err := os.Lstat(live); err == nil {
		if err := renameDurable(live, trash); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := renameDurable(tmp, live); err != nil {
		return err
	}
	return os.RemoveAll(trash)
}

func (e *Engine) restoreRAMPair(ctx context.Context, pin releasePin) error {
	if e.ramPairReady(pin) {
		return nil
	}
	previous := filepath.Join(e.paths.RAM, "bin.previous")
	if found, err := pinDirectory(previous, pin.Version); err == nil && pinDigest(found) == pinDigest(pin) {
		// activateRAMPair owns the reserved bin.previous name. Move this source
		// out of that slot before using it as the rollback candidate.
		work, err := os.MkdirTemp(e.paths.RAM, "rollback-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
		stage := filepath.Join(work, "bin")
		if err := os.Rename(previous, stage); err != nil {
			return err
		}
		return e.activateRAMPair(stage, pin)
	}
	return e.loadRAMPair(ctx, pin)
}

func (e *Engine) rollbackRAMUpdate(ctx context.Context, j ramUpdateJournal) error {
	e.reportUpdate("Restoring the previous executables and saved state…")
	j.Phase = "rollback"
	if err := writeJSON(e.ramUpdateJournalPath(), j); err != nil {
		return err
	}
	if err := e.service.Stop(ctx); err != nil {
		return err
	}
	if err := e.restoreRAMPair(ctx, j.Previous); err != nil {
		return err
	}
	if err := writeJSON(e.paths.layoutPath(), runtimeLayout{Schema: 1, Current: j.Previous}); err != nil {
		return err
	}
	if err := e.restoreRAMState(); err != nil {
		return err
	}
	if j.WasRunning {
		if err := e.startHealthy(ctx, j.Previous.Version); err != nil {
			return err
		}
	}
	return e.commitRAMUpdate(j)
}

// Caller holds the service lock. A stopped daemon validates a deferred update
// only on explicit start; interrupted activation always restores its checkpoint.
func (e *Engine) recoverRAMUpdate(ctx context.Context, start bool) error {
	var j ramUpdateJournal
	if err := readJSON(e.ramUpdateJournalPath(), &j); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := validatePin(j.Previous); err != nil {
		return err
	}
	if _, err := validatePin(j.Candidate); err != nil {
		return err
	}
	if j.Schema != 1 || !newerVersion(j.Candidate.Version, j.Previous.Version) {
		return errors.New("invalid RAM update recovery journal")
	}
	switch j.Phase {
	case "committed":
		return e.cleanupRAMUpdate()
	case "activating", "rollback":
		return e.rollbackRAMUpdate(ctx, j)
	case "deferred":
		if !start {
			return nil
		}
		if err := e.loadRAMPair(ctx, j.Candidate); err != nil {
			return errors.Join(err, e.rollbackRAMUpdate(ctx, j))
		}
		if err := e.startHealthy(ctx, j.Candidate.Version); err != nil {
			return errors.Join(err, e.rollbackRAMUpdate(ctx, j))
		}
		return e.commitRAMUpdate(j)
	default:
		return errors.New("unknown RAM update recovery phase")
	}
}

// stage is a signature-verified, version-checked private RAM pair. Caller holds
// the service lock and public I/O for the complete transaction and its rollback.
func (e *Engine) activateBooksRelease(ctx context.Context, pin releasePin, stage string) error {
	if _, err := validatePin(pin); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureDirectory(e.updateRoot(), 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(e.ramUpdateJournalPath()); err == nil {
		return errors.New("a previous update awaits startup validation")
	} else if !os.IsNotExist(err) {
		return err
	}
	layout, err := e.readLayout()
	if err != nil {
		return err
	}
	if !newerVersion(pin.Version, layout.Current.Version) {
		return errors.New("release is not newer than installed Tailscale")
	}
	if !e.ramPairReady(layout.Current) {
		return errors.New("current verified RAM pair is unavailable")
	}
	size, err := treeSize(filepath.Join(e.paths.Runtime, "state"))
	if err != nil {
		return err
	}
	if err := e.requireSpace(3*size + 1<<20); err != nil {
		return err
	}
	if err := os.RemoveAll(e.ramTransaction()); err != nil {
		return err
	}
	if err := ensureDirectory(e.ramTransaction(), 0700); err != nil {
		return err
	}
	running, err := e.service.Running(ctx)
	if err != nil {
		return err
	}
	if err := e.service.Stop(ctx); err != nil {
		return err
	}
	restartOld := func(cause error) error {
		if running {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			cause = errors.Join(cause, e.startHealthy(rctx, layout.Current.Version))
		}
		return cause
	}
	size, err = treeSize(filepath.Join(e.paths.Runtime, "state"))
	if err != nil {
		return restartOld(err)
	}
	if err := e.requireSpace(3*size + 1<<20); err != nil {
		return restartOld(err)
	}
	if err := cloneTree(filepath.Join(e.paths.Runtime, "state"), filepath.Join(e.ramTransaction(), "checkpoint"), false); err != nil {
		return restartOld(err)
	}
	for _, path := range []string{e.ramTransaction(), e.updateRoot(), e.paths.Runtime} {
		if err := e.persistDirectory(path); err != nil {
			return restartOld(err)
		}
	}
	j := ramUpdateJournal{Schema: 1, Phase: "activating", Previous: layout.Current, Candidate: pin, WasRunning: running}
	if err := writeJSON(e.ramUpdateJournalPath(), j); err != nil {
		return restartOld(err)
	}
	fail := func(cause error) error {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		return errors.Join(cause, e.rollbackRAMUpdate(rctx, j))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := e.activateRAMPair(stage, pin); err != nil {
		return fail(err)
	}
	if err := writeJSON(e.paths.layoutPath(), runtimeLayout{Schema: 1, Current: pin}); err != nil {
		return fail(err)
	}
	if !running {
		j.Phase = "deferred"
		if err := writeJSON(e.ramUpdateJournalPath(), j); err != nil {
			return fail(err)
		}
		return nil
	}
	e.reportUpdate("Starting and checking the updated daemon…")
	if err := e.startHealthy(ctx, pin.Version); err != nil {
		return fail(err)
	}
	return e.commitRAMUpdate(j)
}

func (e *Engine) installBooksRelease(ctx context.Context) error {
	unlock, err := acquireActionLock(e.paths.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	if err := e.installAllowed(); err != nil {
		return err
	}
	lock, err := acquireServiceLock(ctx, e.paths.ServiceLock)
	if err != nil {
		return err
	}
	defer lock()
	current, err := e.recognizedRuntime()
	if err == nil && !current {
		err = errors.New("setup must finish first")
	}
	if err == nil {
		err = e.recoverRAMUpdate(ctx, false)
	}
	if err == nil {
		var layout runtimeLayout
		layout, err = e.readLayout()
		if err == nil {
			err = e.loadRAMPair(ctx, layout.Current)
		}
	}
	if err != nil {
		return err
	}
	if _, err := os.Lstat(e.ramUpdateJournalPath()); err == nil {
		return errors.New("a previous update awaits startup validation")
	} else if !os.IsNotExist(err) {
		return err
	}
	latest, err := e.upstream.Latest(ctx)
	if err != nil {
		return err
	}
	layout, err := e.readLayout()
	if err != nil {
		return err
	}
	if !newerVersion(latest, layout.Current.Version) {
		return errors.New("Tailscale is already up to date")
	}
	e.reportUpdate("Downloading and verifying official Tailscale…")
	defer os.Remove(filepath.Join(e.updateRoot(), "progress.json"))
	return e.withPublicIO(ctx, func(ctx context.Context) error {
		if err := e.ensurePublicDirectory(e.paths.Books); err != nil {
			return err
		}
		cache, err := os.MkdirTemp(e.paths.Books, ".download-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(cache)
		artifact, err := e.upstream.Download(ctx, latest, cache)
		if err != nil {
			return err
		}
		pin, stage, err := e.stageSignedRelease(ctx, artifact.Path, artifact)
		if err != nil {
			return err
		}
		defer os.RemoveAll(filepath.Dir(stage))
		if err := e.persistPublicPair(ctx, stage, pin); err != nil {
			return err
		}
		if err := e.installAllowed(); err != nil {
			return err
		}
		return e.activateBooksRelease(ctx, pin, stage)
	})
}
