package helper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const ramMemoryReserve = 64 << 20

func availableMemory() (int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "MemAvailable:" && fields[2] == "kB" {
			n, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil && n > 0 && n < 1<<40 {
				return n * 1024, nil
			}
		}
	}
	return 0, errors.New("cannot determine available RAM")
}

func checkTmpfs(path string) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return err
	}
	if uint64(stat.Type) != 0x01021994 {
		return errors.New("active executables require a RAM-backed tmpfs")
	}
	return nil
}

func (e *Engine) checkRAMBacking() error {
	if e.ramCheck != nil {
		return e.ramCheck(e.paths.RAM)
	}
	return checkTmpfs(e.paths.RAM)
}

func (e *Engine) requireRAM(bytes int64) error {
	if err := ensureDirectory(e.paths.RAM, 0700); err != nil {
		return err
	}
	if err := e.checkRAMBacking(); err != nil {
		return err
	}
	free := e.free
	if free == nil {
		free = freeBytes
	}
	space, err := free(e.paths.RAM)
	if err != nil {
		return err
	}
	if bytes < 0 || bytes > 2*maxExpanded || space < bytes+spaceReserve {
		return errors.New("not enough tmpfs space for verified executables")
	}
	memory := e.memory
	if memory == nil {
		memory = availableMemory
	}
	available, err := memory()
	if err != nil {
		return err
	}
	if available < bytes+ramMemoryReserve {
		return fmt.Errorf("not enough RAM: need %d MiB including reserve, available %d MiB",
			(bytes+ramMemoryReserve+(1<<20)-1)>>20, available>>20)
	}
	return nil
}

func copyContext(ctx context.Context, out io.Writer, in io.Reader, limit int64) (int64, error) {
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := in.Read(buffer)
		if total+int64(n) > limit {
			return total, errors.New("copy exceeds verified size")
		}
		if n > 0 {
			written, err := out.Write(buffer[:n])
			total += int64(written)
			if err != nil {
				return total, err
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func copyPinned(ctx context.Context, source, target string, pin executablePin, mode os.FileMode) error {
	fd, err := syscall.Open(source, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	in := os.NewFile(uintptr(fd), source)
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != pin.Size {
		return errors.New("public executable size or type changed")
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	hash := sha256.New()
	n, err := copyContext(ctx, io.MultiWriter(out, hash), in, pin.Size)
	if err != nil {
		return err
	}
	if n != pin.Size || hex.EncodeToString(hash.Sum(nil)) != pin.SHA256 {
		return errors.New("public executable does not match protected SHA256")
	}
	return out.Sync()
}

func rejectDirectoryLinks(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("public artifact directory contains a link or special file")
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func (e *Engine) ramPairReady(pin releasePin) bool {
	if e.checkRAMBacking() != nil {
		return false
	}
	var ready struct{ Fingerprint string }
	if readJSON(filepath.Join(e.paths.RAM, "ready.json"), &ready) != nil || ready.Fingerprint != pinDigest(pin) {
		return false
	}
	for name, expected := range pin.Files {
		info, err := os.Lstat(filepath.Join(e.paths.binaryDir(), name))
		if err != nil || !info.Mode().IsRegular() || info.Size() != expected.Size || info.Mode().Perm()&0022 != 0 {
			return false
		}
	}
	return true
}

// Caller holds public I/O while reading the untrusted, USB-visible cache.
func (e *Engine) stageRAMPair(ctx context.Context, pin releasePin) (string, error) {
	bytes, err := validatePin(pin)
	if err != nil {
		return "", err
	}
	if err := e.requireRAM(bytes); err != nil {
		return "", err
	}
	source := filepath.Join(e.paths.Books, pin.Version)
	if err := rejectDirectoryLinks(source); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(e.paths.RAM, "pair-")
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(stage)
		}
	}()
	for name, expected := range pin.Files {
		if err := copyPinned(ctx, filepath.Join(source, name), filepath.Join(stage, name), expected, 0755); err != nil {
			return "", err
		}
	}
	if err := syncDirectory(stage); err != nil {
		return "", err
	}
	ok = true
	return stage, nil
}

func (e *Engine) activateRAMPair(stage string, pin releasePin) error {
	live := e.paths.binaryDir()
	previous := filepath.Join(e.paths.RAM, "bin.previous")
	if err := os.RemoveAll(previous); err != nil {
		return err
	}
	hadPrevious := false
	if _, err := os.Lstat(live); err == nil {
		if err := os.Rename(live, previous); err != nil {
			return err
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stage, live); err != nil {
		if hadPrevious {
			_ = os.Rename(previous, live)
		}
		return err
	}
	if err := writeJSON(filepath.Join(e.paths.RAM, "ready.json"),
		struct{ Fingerprint string }{Fingerprint: pinDigest(pin)}); err != nil {
		_ = os.RemoveAll(live)
		if hadPrevious {
			_ = os.Rename(previous, live)
		}
		return err
	}
	return syncDirectory(e.paths.RAM)
}

func (e *Engine) loadRAMPair(ctx context.Context, pin releasePin) error {
	if _, err := validatePin(pin); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureDirectory(e.paths.RAM, 0700); err != nil {
		return err
	}
	if e.ramPairReady(pin) {
		return nil
	}
	return e.withPublicIO(ctx, func(ctx context.Context) error {
		stage, err := e.stageRAMPair(ctx, pin)
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		if _, err := os.Stat(e.paths.binaryDir()); err == nil && e.service != nil {
			if running, err := e.service.Running(ctx); err != nil {
				return err
			} else if running {
				return errors.New("stop the existing daemon before replacing its RAM executable pair")
			}
		}
		return e.activateRAMPair(stage, pin)
	})
}
