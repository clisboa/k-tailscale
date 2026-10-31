package helper

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// A journal is durable before the first live rename. Backups are never consumed
// by rollback, so recovery can itself be interrupted and repeated.
type updateJournal struct {
	Schema     int    `json:"schema"`
	Phase      string `json:"phase"`
	Previous   string `json:"previous"`
	Version    string `json:"version"`
	WasRunning bool   `json:"was_running"`
}

func (e *Engine) updateRoot() string        { return filepath.Join(e.paths.Runtime, "updates") }
func (e *Engine) updateJournalPath() string { return filepath.Join(e.updateRoot(), "transaction.json") }
func treeSize(directory string) (int64, error) {
	var total int64
	count := 0
	err := filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > 512 {
			return errors.New("state contains too many files")
		}
		i, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !i.Mode().IsRegular() {
			return errors.New("state contains a link or special file")
		}
		total += i.Size()
		if total > 256<<20 {
			return errors.New("state checkpoint exceeds size limit")
		}
		return nil
	})
	return total, err
}
func cloneTree(source, target string, links bool) error {
	if _, err := treeSize(source); err != nil {
		return err
	}
	if err := ensureDirectory(target, 0700); err != nil {
		return err
	}
	err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if d.IsDir() {
			return ensureDirectory(dest, 0700)
		}
		if links {
			return os.Link(path, dest)
		}
		return copyPrivate(path, dest, 128<<20)
	})
	if err != nil {
		return err
	}
	// Sync every directory containing a newly linked/copied entry.
	return filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return syncDirectory(path)
		}
		return nil
	})
}
func (e *Engine) requireSpace(bytes int64) error {
	free, err := e.free(e.paths.Runtime)
	if err != nil {
		return err
	}
	if bytes+spaceReserve > free {
		return fmt.Errorf("Not enough system storage: need %d MiB, available %d MiB. Books storage is not used.", (bytes+spaceReserve+(1<<20)-1)>>20, free>>20)
	}
	return nil
}
func renameDurable(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(from)); err != nil {
		return err
	}
	if filepath.Dir(from) != filepath.Dir(to) {
		return syncDirectory(filepath.Dir(to))
	}
	return nil
}
func (e *Engine) pairVersion(ctx context.Context, dir string) (string, error) {
	a, err := e.service.Version(ctx, filepath.Join(dir, "tailscale"))
	if err != nil {
		return "", err
	}
	b, err := e.service.Version(ctx, filepath.Join(dir, "tailscaled"))
	if err != nil {
		return "", err
	}
	if a != b || !stableVersion(a) {
		return "", errors.New("executable pair mismatch")
	}
	return a, nil
}
func (e *Engine) activateTailscale(ctx context.Context, a Artifact) error {
	if err := ensureDirectory(e.updateRoot(), 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(e.updateJournalPath()); err == nil {
		return errors.New("an update awaits startup validation; restart Tailscale first")
	} else if !os.IsNotExist(err) {
		return err
	}
	old, err := e.pairVersion(ctx, filepath.Join(e.paths.Runtime, "bin"))
	if err != nil {
		return err
	}
	if !newerVersion(a.Version, old) {
		return errors.New("release is not newer than installed Tailscale")
	}
	size, err := treeSize(filepath.Join(e.paths.Runtime, "state"))
	if err != nil {
		return err
	}
	if err := e.requireSpace(a.Expanded + 3*size); err != nil {
		return err
	}
	txn := filepath.Join(e.updateRoot(), "transaction")
	if err := os.RemoveAll(txn); err != nil {
		return err
	}
	if err := ensureDirectory(txn, 0700); err != nil {
		return err
	}
	stage := filepath.Join(txn, "candidate")
	e.reportUpdate("Validating the new executable pair…")
	if err := extractArtifact(ctx, a, stage); err != nil {
		return err
	}
	if v, err := e.pairVersion(ctx, stage); err != nil {
		return err
	} else if v != a.Version {
		return errors.New("staged executable version mismatch")
	}
	running, err := e.service.Running(ctx)
	if err != nil {
		return err
	}
	size, err = treeSize(filepath.Join(e.paths.Runtime, "state"))
	if err != nil {
		return err
	}
	if err := e.requireSpace(3 * size); err != nil {
		return err
	}
	e.reportUpdate("Installing Tailscale; preserving saved identity…")
	if err := e.service.Stop(ctx); err != nil {
		return err
	}
	// Before a journal exists, a failure can simply restart the unchanged pair.
	restartOld := func(cause error) error {
		if running {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			cause = errors.Join(cause, e.service.Start(rctx))
		}
		return cause
	}
	size, err = treeSize(filepath.Join(e.paths.Runtime, "state"))
	if err != nil {
		return restartOld(err)
	}
	if err := e.requireSpace(3 * size); err != nil {
		return restartOld(err)
	}
	if err := cloneTree(filepath.Join(e.paths.Runtime, "state"), filepath.Join(txn, "checkpoint"), false); err != nil {
		return restartOld(err)
	}
	// Sync the checkpoint's parent entry and every newly created ancestor before
	// the journal promises that these rollback sources exist.
	for _, dir := range []string{txn, e.updateRoot(), e.paths.Runtime} {
		if err := e.persistDirectory(dir); err != nil {
			return restartOld(err)
		}
	}
	j := updateJournal{Schema: 1, Phase: "activating", Previous: old, Version: a.Version, WasRunning: running}
	if err := writeJSON(e.updateJournalPath(), j); err != nil {
		return restartOld(err)
	}
	fail := func(cause error) error {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		return errors.Join(cause, e.rollbackUpdate(rctx, j))
	}
	if err := renameDurable(filepath.Join(e.paths.Runtime, "bin"), filepath.Join(txn, "previous")); err != nil {
		return fail(err)
	}
	if err := renameDurable(stage, filepath.Join(e.paths.Runtime, "bin")); err != nil {
		return fail(err)
	}
	if !running {
		j.Phase = "deferred"
		if err := writeJSON(e.updateJournalPath(), j); err != nil {
			return fail(err)
		}
		return nil
	}
	e.reportUpdate("Starting and checking the updated daemon…")
	if err := e.startHealthy(ctx, a.Version); err != nil {
		return fail(err)
	}
	return e.commitUpdate(j)
}
func (e *Engine) startHealthy(ctx context.Context, v string) error {
	if err := e.service.Start(ctx); err != nil {
		return err
	}
	if err := e.service.Healthy(ctx, v); err != nil {
		return err
	}
	return e.service.DisableNativeUpdates(ctx)
}
func (e *Engine) cleanupUpdate() error {
	if err := os.RemoveAll(filepath.Join(e.updateRoot(), "transaction")); err != nil {
		return err
	}
	if err := os.Remove(e.updateJournalPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(e.updateRoot())
}
func (e *Engine) commitUpdate(j updateJournal) error {
	j.Phase = "committed"
	if err := writeJSON(e.updateJournalPath(), j); err != nil {
		return err
	}
	return e.cleanupUpdate()
}
func (e *Engine) restoreTree(source, name string, links bool) error {
	txn := filepath.Join(e.updateRoot(), "transaction")
	tmp := filepath.Join(txn, "restore-"+name)
	trash := filepath.Join(txn, "discard-"+name)
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := cloneTree(source, tmp, links); err != nil {
		return err
	}
	if err := os.RemoveAll(trash); err != nil {
		return err
	}
	live := filepath.Join(e.paths.Runtime, name)
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
func (e *Engine) rollbackUpdate(ctx context.Context, j updateJournal) error {
	e.reportUpdate("Restoring the previous executables and saved state…")
	j.Phase = "rollback"
	if err := writeJSON(e.updateJournalPath(), j); err != nil {
		return err
	}
	if err := e.service.Stop(ctx); err != nil {
		return err
	}
	txn := filepath.Join(e.updateRoot(), "transaction")
	prev := filepath.Join(txn, "previous")
	if _, err := os.Lstat(prev); err == nil {
		if err := e.restoreTree(prev, "bin", true); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if v, err := e.pairVersion(ctx, filepath.Join(e.paths.Runtime, "bin")); err != nil {
		return err
	} else if v != j.Previous {
		return errors.New("previous executable backup is unavailable; recovery retained")
	}
	if err := e.restoreTree(filepath.Join(txn, "checkpoint"), "state", false); err != nil {
		return err
	}
	if j.WasRunning {
		if err := e.startHealthy(ctx, j.Previous); err != nil {
			return err
		}
	}
	return e.commitUpdate(j)
}

// Caller holds the service lock. A normal command retains a deliberately stopped
// daemon; boot/start is allowed to validate a deferred candidate.
func (e *Engine) recoverUpdate(ctx context.Context, start bool) error {
	var j updateJournal
	if err := readJSON(e.updateJournalPath(), &j); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if j.Schema != 1 || !stableVersion(j.Previous) || !newerVersion(j.Version, j.Previous) {
		return errors.New("invalid update recovery journal")
	}
	switch j.Phase {
	case "committed":
		return e.cleanupUpdate()
	case "activating", "rollback":
		return e.rollbackUpdate(ctx, j)
	case "deferred":
		if !start {
			return nil
		}
		if err := e.startHealthy(ctx, j.Version); err != nil {
			return e.rollbackUpdate(ctx, j)
		}
		return e.commitUpdate(j)
	default:
		return errors.New("unrecognized update recovery phase")
	}
}
func (e *Engine) InstallTailscale(ctx context.Context) error {
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
	current, err := e.recognizedRuntime()
	if err == nil && !current {
		err = errors.New("setup must finish first")
	}
	if err == nil {
		err = e.recoverUpdate(ctx, false)
	}
	lock()
	if err != nil {
		return err
	}
	latest, err := e.upstream.Latest(ctx)
	if err != nil {
		return err
	}
	if err := ensureDirectory(e.updateRoot(), 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(e.updateJournalPath()); err == nil {
		return errors.New("a previous update awaits startup validation")
	} else if !os.IsNotExist(err) {
		return err
	}
	// The action lock proves these private scratch directories have no writer.
	if err := os.RemoveAll(filepath.Join(e.updateRoot(), "transaction")); err != nil {
		return err
	}
	scratch, err := filepath.Glob(filepath.Join(e.updateRoot(), "download-*"))
	if err != nil {
		return err
	}
	for _, name := range scratch {
		if err := os.RemoveAll(name); err != nil {
			return err
		}
	}
	currentVersion, err := e.pairVersion(ctx, filepath.Join(e.paths.Runtime, "bin"))
	if err != nil {
		return err
	}
	if !newerVersion(latest, currentVersion) {
		return errors.New("Tailscale is already up to date")
	}
	cache, err := os.MkdirTemp(e.updateRoot(), "download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(cache)
	e.reportUpdate("Downloading and verifying official Tailscale…")
	defer os.Remove(filepath.Join(e.updateRoot(), "progress.json"))
	a, err := e.upstream.Download(ctx, latest, cache)
	if err != nil {
		return err
	}
	lock, err = acquireServiceLock(ctx, e.paths.ServiceLock)
	if err != nil {
		return err
	}
	defer lock()
	if err := e.installAllowed(); err != nil {
		return err
	}
	return e.activateTailscale(ctx, a)
}

// Directory persistence is a filesystem boundary, with fsync as the default.
func (e *Engine) persistDirectory(path string) error {
	if e.persist != nil {
		return e.persist(path)
	}
	return syncDirectory(path)
}
