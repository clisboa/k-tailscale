package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ramFixture(t *testing.T) (*Engine, releasePin) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		paths: Paths{
			Runtime:  filepath.Join(root, "private"),
			Books:    filepath.Join(root, "public"),
			RAM:      filepath.Join(root, "ram"),
			USBBlock: filepath.Join(root, "exporting"),
		},
		free:     func(string) (int64, error) { return 1 << 30, nil },
		memory:   func() (int64, error) { return 1 << 30, nil },
		ramCheck: func(string) error { return nil },
	}
	if err := os.MkdirAll(e.paths.Runtime, 0700); err != nil {
		t.Fatal(err)
	}
	pin := releasePin{Version: "1.104.4", Files: map[string]executablePin{}}
	for _, name := range []string{"tailscale", "tailscaled"} {
		data := []byte("verified " + name)
		pin.Files[name] = executablePin{Size: int64(len(data)), SHA256: digest(data)}
		path := filepath.Join(e.paths.Books, pin.Version, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0666); err != nil {
			t.Fatal(err)
		}
	}
	return e, pin
}

func TestRAMPairLoadsVerifiedPublicFilesAndSurvivesBooksRemoval(t *testing.T) {
	e, pin := ramFixture(t)
	if err := e.loadRAMPair(context.Background(), pin); err != nil {
		t.Fatal(err)
	}
	for name, expected := range pin.Files {
		data, err := os.ReadFile(filepath.Join(e.paths.binaryDir(), name))
		if err != nil || digest(data) != expected.SHA256 {
			t.Fatal("RAM copy is not the pinned executable", err)
		}
	}
	if err := os.Rename(e.paths.Books, e.paths.Books+"-unmounted"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(e.paths.USBBlock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := e.loadRAMPair(context.Background(), pin); err != nil {
		t.Fatal("valid RAM cache required exported books storage", err)
	}
}

func TestRAMPairRejectsTamperingAndLinksBeforeActivation(t *testing.T) {
	for _, kind := range []string{"changed", "symlink", "missing"} {
		t.Run(kind, func(t *testing.T) {
			e, pin := ramFixture(t)
			path := filepath.Join(e.paths.Books, pin.Version, "tailscaled")
			switch kind {
			case "changed":
				data := []byte(strings.Repeat("x", int(pin.Files["tailscaled"].Size)))
				if err := os.WriteFile(path, data, 0666); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+"-elsewhere"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+"-elsewhere", path); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.loadRAMPair(context.Background(), pin); err == nil {
				t.Fatal("unverified public executable was activated")
			}
			if _, err := os.Stat(e.paths.binaryDir()); !os.IsNotExist(err) {
				t.Fatal("incomplete pair became active")
			}
		})
	}
}

func TestRAMPairResourceAndUSBPreflight(t *testing.T) {
	for _, kind := range []string{"memory", "tmpfs", "usb", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			e, pin := ramFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "memory":
				e.memory = func() (int64, error) { return 32 << 20, nil }
			case "tmpfs":
				e.free = func(string) (int64, error) { return 0, nil }
			case "usb":
				if err := os.Mkdir(e.paths.USBBlock, 0700); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			}
			if err := e.loadRAMPair(ctx, pin); err == nil {
				t.Fatal("failed preflight accepted")
			}
			if _, err := os.Stat(e.paths.binaryDir()); !os.IsNotExist(err) {
				t.Fatal("failed preflight activated executables")
			}
		})
	}
}
