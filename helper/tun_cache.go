package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// The small optional module follows the same public-persistence/private-RAM
// boundary as Tailscale itself. Kernel code is never loaded from books storage.
func (e *Engine) ensureRAMTUN(ctx context.Context, target string, spec tunModuleSpec) error {
	if !e.paths.ramMode() {
		return errors.New("finish storage setup before loading a TUN module")
	}
	if err := e.checkRAMBacking(); err == nil && validateTUNFile(target, spec) == nil {
		return nil
	}
	return e.withPublicIO(ctx, func(ctx context.Context) error {
		if err := e.requireRAM(spec.Size); err != nil {
			return err
		}
		if err := rejectDirectoryLinks(filepath.Dir(e.paths.TUNModule)); err != nil {
			return err
		}
		if err := ensureDirectory(filepath.Dir(target), 0700); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(e.paths.RAM, "tun-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		candidate := filepath.Join(stage, "tun.ko")
		if err := copyPinned(ctx, e.paths.TUNModule, candidate, executablePin{Size: spec.Size, SHA256: spec.SHA256}, 0600); err != nil {
			return err
		}
		if err := validateTUNFile(candidate, spec); err != nil {
			return err
		}
		if err := os.Rename(candidate, target); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(target))
	})
}
