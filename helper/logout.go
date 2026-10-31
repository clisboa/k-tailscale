package helper

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

// SignOut serializes credential changes with setup, connection and updates.
// Only tailscaled manages the profile; no credential files are removed here.
func (e *Engine) SignOut(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
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
	current, err := e.recognizedRuntime()
	if err != nil {
		return err
	}
	if !current {
		return errors.New("setup must finish first")
	}
	if err := e.recoverAddon(); err != nil {
		return err
	}
	if err := e.recoverUpdate(ctx, false); err != nil {
		return err
	}
	if err := e.validateRuntimeDirectories(); err != nil {
		return err
	}
	if _, err := e.service.Version(ctx, filepath.Join(e.paths.Runtime, "bin/tailscale")); err != nil {
		return err
	}
	running, err := e.service.Running(ctx)
	if err != nil {
		return err
	}
	if !running {
		return errors.New("Tailscale service is not running. Tap Connect, then retry Sign out.")
	}
	return e.service.Logout(ctx)
}
