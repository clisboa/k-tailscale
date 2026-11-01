package helper

import (
	"context"
	"os"
	"path/filepath"
)

// A diagnostic copy is optional. In particular, USB cancellation must not
// reopen the public volume after the export guard has joined the setup writer.
func (e *Engine) reportSetup(ctx context.Context, result error) {
	message := "Public Tailscale files use books storage; verified executables run from RAM. Private identity remains on system storage.\n"
	if result != nil {
		message = result.Error() + "\n"
	}
	if len(message) > 4096 {
		message = message[:4096] + "\n"
	}
	owner, err := os.ReadFile(filepath.Join(e.paths.Runtime, "installer-owner"))
	if err != nil || string(owner) != "k-tailscale-v2\n" {
		return
	}
	_ = writeAtomic(filepath.Join(e.paths.Runtime, "last-setup.txt"), []byte(message), 0600)
	if !e.paths.ramMode() || ctx.Err() != nil {
		return
	}
	_ = e.withPublicIO(ctx, func(context.Context) error {
		directory := e.paths.Books
		if e.paths.BooksMount != "" {
			directory = filepath.Join(e.paths.BooksMount, ".adds/tailscale-ui")
		}
		if err := e.ensurePublicDirectory(directory); err != nil {
			return err
		}
		return writeAtomicPublic(filepath.Join(directory, "migration-status.txt"), []byte(message))
	})
}

func writeAtomicPublic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".report-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncPublicDirectory(filepath.Dir(path))
}
