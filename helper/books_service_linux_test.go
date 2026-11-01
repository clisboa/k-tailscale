package helper

import (
	"context"
	"debug/elf"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestOSServiceRAMDaemonHasNoBooksMappingsOrHandlesAcrossUSB(t *testing.T) {
	root := t.TempDir()
	if err := checkTmpfs(root); err != nil {
		t.Skip("requires an executable tmpfs; covered by test-linux-root.sh")
	}
	p := Paths{Runtime: filepath.Join(root, "private"), RAM: filepath.Join(root, "ram"), Books: filepath.Join(root, "books"), Socket: filepath.Join(root, "run/socket")}
	source := filepath.Join(p.Books, "1.104.4")
	for _, path := range []string{source, filepath.Join(p.Runtime, "state")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tailscale", "tailscaled"} {
		if err := copyPrivate(executable, filepath.Join(source, name), 128<<20); err != nil {
			t.Fatal(err)
		}
	}
	pin, err := pinDirectory(source, "1.104.4")
	if err != nil {
		t.Fatal(err)
	}
	s := &osService{paths: p, machine: elf.EM_AARCH64, class: elf.ELFCLASS64}
	if runtime.GOARCH == "amd64" {
		s.machine = elf.EM_X86_64
	}
	e := &Engine{paths: p, free: freeBytes, service: s}
	// This root suite runs /tmp as executable tmpfs, like the inspected Kobo.
	if err := e.loadRAMPair(context.Background(), pin); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer s.Stop(context.Background())
	t.Setenv("TS_LOGS_DIR", "/inherited/incorrect/logs")
	if err := e.startHealthy(ctx, pin.Version); err != nil {
		t.Fatal(err)
	}
	logs, err := os.ReadFile(p.Socket + ".logsdir")
	if err != nil || string(logs) != filepath.Join(p.RAM, "logs") {
		t.Fatal("daemon log buffers do not use private RAM", err)
	}
	pids, err := s.pids(ctx)
	if err != nil || len(pids) != 1 {
		t.Fatal("missing RAM daemon", err)
	}
	actual, references, err := daemonBooksReferences(pids[0], p.Books)
	if err != nil || references != 0 || actual != filepath.Join(p.binaryDir(), "tailscaled") {
		t.Fatal("daemon retains books dependency", actual, references, err)
	}
	if err := e.BeginUSB(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(p.Books, p.Books+"-unmounted"); err != nil {
		t.Fatal(err)
	}
	if err := s.Healthy(ctx, pin.Version); err != nil {
		t.Fatal("USB stopped RAM daemon", err)
	}
	if version, err := s.Version(ctx, filepath.Join(p.binaryDir(), "tailscale")); err != nil || version != pin.Version {
		t.Fatal("CLI requires books", err)
	}
	if err := s.Logout(ctx); err != nil {
		t.Fatal("logout requires books", err)
	}
	after, err := s.pids(ctx)
	if err != nil || len(after) != 1 || after[0] != pids[0] {
		t.Fatal("USB changed running daemon", err)
	}
	if err := os.Rename(p.Books+"-unmounted", p.Books); err != nil {
		t.Fatal(err)
	}
	if err := e.EndUSB(); err != nil {
		t.Fatal(err)
	}
}
