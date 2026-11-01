package helper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type publicIOOwner struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

type publicIOContextKey struct{}

func (e *Engine) withPublicIO(ctx context.Context, fn func(context.Context) error) error {
	if ctx.Value(publicIOContextKey{}) == e.paths.RAM {
		return fn(ctx)
	}
	release, err := e.acquirePublicIO(ctx)
	if err != nil {
		return err
	}
	defer release()
	return fn(context.WithValue(ctx, publicIOContextKey{}, e.paths.RAM))
}

func processStartToken(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", errors.New("invalid process identity")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return "", errors.New("incomplete process identity")
	}
	return fields[19], nil
}

func (e *Engine) usbBlocked() (bool, error) {
	_, err := os.Lstat(e.paths.usbBlockPath())
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (e *Engine) acquirePublicIO(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ensureDirectory(e.paths.RAM, 0700); err != nil {
		return nil, err
	}
	blocked, err := e.usbBlocked()
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, errors.New("books storage is unavailable during USB export")
	}
	release, err := acquireServiceLock(ctx, filepath.Join(e.paths.RAM, "public-io.lock"))
	if err != nil {
		return nil, err
	}
	blocked, err = e.usbBlocked()
	if err != nil || blocked {
		release()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("books storage is unavailable during USB export")
	}
	if err := e.booksMounted(); err != nil {
		release()
		return nil, err
	}
	if err := e.validateUSBGuard(); err != nil {
		release()
		return nil, err
	}
	start, err := processStartToken(os.Getpid())
	if err != nil && runtime.GOOS == "linux" {
		release()
		return nil, err
	}
	// Non-Linux unit tests can exercise copies and locks, but an empty token
	// can never authorize process cancellation at a USB export boundary.
	owner := publicIOOwner{PID: os.Getpid(), Start: start}
	path := filepath.Join(e.paths.RAM, "public-io.json")
	if err := writeJSON(path, owner); err != nil {
		release()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = os.Remove(path)
			release()
		})
	}, nil
}

func (e *Engine) booksMounted() error {
	if e.paths.BooksMount == "" {
		return rejectDirectoryLinks(e.paths.Books)
	}
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[1] == e.paths.BooksMount && fields[2] == "vfat" {
			return nil
		}
	}
	return errors.New("books storage is not mounted; reconnect it before loading or updating")
}

func (e *Engine) cancelPublicOwner() error {
	var owner publicIOOwner
	if err := readJSON(filepath.Join(e.paths.RAM, "public-io.json"), &owner); os.IsNotExist(err) {
		// The writer may still be registering. The lock is checked again.
		return nil
	} else if err != nil {
		return err
	}
	if owner.PID == os.Getpid() {
		// An in-process operation must release its own scope. Never signal
		// ourselves; a timeout keeps export blocked if it does not finish.
		return nil
	}
	if owner.PID <= 1 || owner.Start == "" {
		return errors.New("cannot validate the active books-storage process")
	}
	start, err := processStartToken(owner.PID)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || start != owner.Start {
		return errors.New("active books-storage process identity changed")
	}
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", owner.PID))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	executable = strings.TrimSuffix(executable, " (deleted)")
	allowed := executable == e.paths.CandidateHelper
	if target, err := filepath.EvalSymlinks(e.paths.CoreLink); err == nil && target == executable {
		allowed = true
	}
	if !allowed {
		return errors.New("active books-storage process is not an owned helper")
	}
	if err := syscall.Kill(owner.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func (e *Engine) BeginUSB(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureDirectory(e.paths.RAM, 0700); err != nil {
		return err
	}
	if err := ensureDirectory(e.paths.usbBlockPath(), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(e.paths.RAM, "public-io.lock"),
		os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		if err := e.cancelPublicOwner(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("USB export blocked until addon books-storage I/O closes: %w", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (e *Engine) EndUSB() error {
	if err := e.booksMounted(); err != nil {
		return err
	}
	if err := os.Remove(e.paths.usbBlockPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func usbWrapper(original []byte, expected, helperPath, backupPath string) ([]byte, error) {
	if digest(original) != expected {
		return nil, errors.New("unknown vendor USB script was retained")
	}
	helper := shellQuote(helperPath)
	backup := shellQuote(backupPath)
	script := "#!/bin/sh\n# k-tailscale public-storage I/O guard\n" +
		"case \"$ACTION\" in\n" +
		"add)\n  " + helper + " usb-begin || exit $?\n" +
		"  /bin/sh " + backup + " \"$@\"\n  result=$?\n" +
		"  if [ \"$result\" -ne 0 ]; then " + helper + " usb-end >/dev/null 2>&1; fi\n" +
		"  exit \"$result\"\n  ;;\n" +
		"remove)\n  /bin/sh " + backup + " \"$@\" || exit $?\n" +
		"  exec " + helper + " usb-end\n  ;;\n" +
		"*) exec /bin/sh " + backup + " \"$@\" ;;\nesac\n"
	return []byte(script), nil
}
