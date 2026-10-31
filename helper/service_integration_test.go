package helper

import (
	"context"
	"debug/elf"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Re-execute this real native test binary as a version-reporting CLI or a
// Unix-LocalAPI daemon. Production process and socket code runs unchanged.
func TestMain(m *testing.M) {
	name := filepath.Base(os.Args[0])
	if name == "tailscale" {
		if len(os.Args) == 3 && os.Args[2] == "logout" && strings.HasPrefix(os.Args[1], "--socket=") {
			socket := strings.TrimPrefix(os.Args[1], "--socket=")
			if _, err := os.Stat(socket + ".fail"); err == nil {
				os.Exit(7)
			}
			if _, err := os.Stat(socket + ".block"); err == nil {
				time.Sleep(time.Minute)
			}
			if err := os.WriteFile(socket+".logout", []byte("signed out"), 0600); err != nil {
				os.Exit(8)
			}
			os.Exit(0)
		}
		for _, a := range os.Args[1:] {
			if a == "version" {
				fmt.Println(`{"short":"1.104.4"}`)
				os.Exit(0)
			}
		}
		os.Exit(0)
	}
	if name == "tailscaled" {
		if len(os.Args) > 1 && os.Args[1] == "--version" {
			fmt.Println("1.104.4")
			os.Exit(0)
		}
		socket := ""
		for _, a := range os.Args[1:] {
			if strings.HasPrefix(a, "--socket=") {
				socket = strings.TrimPrefix(a, "--socket=")
			}
		}
		_ = os.Remove(socket)
		listener, err := net.Listen("unix", socket)
		if err != nil {
			os.Exit(2)
		}
		server := http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"Version": "1.104.4", "BackendState": "Stopped"})
		})}
		_ = server.Serve(listener)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestOSServiceLogoutUsesSocketAndPropagatesFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("native ELF test")
	}
	root := t.TempDir()
	p := Paths{Runtime: root, Socket: filepath.Join(root, "socket")}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyPrivate(exe, filepath.Join(root, "bin/tailscale"), 128<<20); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "bin/tailscale"), 0755); err != nil {
		t.Fatal(err)
	}
	s := &osService{paths: p}
	operation, ok := interface{}(s).(interface{ Logout(context.Context) error })
	if !ok {
		t.Fatal("OS service has no logout command")
	}
	if err := operation.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(p.Socket + ".logout"); err != nil || string(got) != "signed out" {
		t.Fatal("wrong logout argv", err)
	}
	if err := os.WriteFile(p.Socket+".fail", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := operation.Logout(context.Background()); err == nil {
		t.Fatal("logout CLI failure ignored")
	}
	if err := os.Remove(p.Socket + ".fail"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Socket+".block", nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := operation.Logout(ctx); err == nil {
		t.Fatal("logout cancellation ignored")
	}
}
func TestOSServiceLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /proc and pidof")
	}
	root := t.TempDir()
	p := Paths{Runtime: root, Socket: filepath.Join(root, "run/socket")}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tailscale", "tailscaled"} {
		path := filepath.Join(root, "bin", name)
		if err := copyPrivate(exe, path, 128<<20); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	s := &osService{paths: p, machine: elf.EM_AARCH64, class: elf.ELFCLASS64}
	if runtime.GOARCH == "amd64" {
		s.machine = elf.EM_X86_64
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer s.Stop(context.Background())
	for _, name := range []string{"tailscale", "tailscaled"} {
		if v, err := s.Version(ctx, filepath.Join(root, "bin", name)); err != nil || v != "1.104.4" {
			t.Fatal("version", v, err)
		}
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Healthy(ctx, "1.104.4"); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableNativeUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	if running, err := s.Running(ctx); err != nil || !running {
		t.Fatal("daemon not tracked", err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if running, err := s.Running(ctx); err != nil || running {
		t.Fatal("daemon did not stop", err)
	}
}
