package helper

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A relocatable ARM ELF fixture with standard .modinfo and __versions sections;
// it contains no runnable driver code and is never passed to a real loader.
func tunFixture(t *testing.T) (string, tunModuleSpec) {
	t.Helper()
	spec := tunModuleSpec{Schema: 1, Model: "376", Kernel: "4.1.15-test", VersionMagic: "4.1.15-test SMP preempt mod_unload modversions ARMv7 p2v8", ModuleLayout: "0xb005e3c1", InstallPath: "mnt/onboard/.adds/k-tailscale/kernel/clara-hd/tun.ko"}
	names := []byte("\x00.shstrtab\x00.modinfo\x00__versions\x00")
	info := []byte("license=GPL\x00vermagic=" + spec.VersionMagic + " \x00depends=\x00")
	versions := make([]byte, 64)
	binary.LittleEndian.PutUint32(versions, 0xb005e3c1)
	copy(versions[4:], "module_layout")
	data := make([]byte, 52)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 1, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 1)
	binary.LittleEndian.PutUint16(data[18:], 40)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint16(data[40:], 52)
	sections := make([]byte, 4*40)
	for i, entry := range []struct {
		name uint32
		data []byte
	}{{1, names}, {11, info}, {20, versions}} {
		offset := len(data)
		data = append(data, entry.data...)
		s := sections[(i+1)*40:]
		binary.LittleEndian.PutUint32(s, entry.name)
		binary.LittleEndian.PutUint32(s[4:], 1)
		if i == 0 {
			binary.LittleEndian.PutUint32(s[4:], 3)
		}
		binary.LittleEndian.PutUint32(s[16:], uint32(offset))
		binary.LittleEndian.PutUint32(s[20:], uint32(len(entry.data)))
		binary.LittleEndian.PutUint32(s[32:], 1)
	}
	binary.LittleEndian.PutUint32(data[32:], uint32(len(data)))
	binary.LittleEndian.PutUint16(data[46:], 40)
	binary.LittleEndian.PutUint16(data[48:], 4)
	binary.LittleEndian.PutUint16(data[50:], 1)
	data = append(data, sections...)
	sum := sha256.Sum256(data)
	spec.SHA256 = hex.EncodeToString(sum[:])
	spec.Size = int64(len(data))
	path := filepath.Join(t.TempDir(), "tun.ko")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path, spec
}

func TestTUNBootstrapUsesBuiltinWithoutInspectingModuleOrKernel(t *testing.T) {
	hooks := tunHooks{probe: func() error { return nil }, kernel: func() (string, error) { t.Fatal("builtin probed kernel"); return "", nil }, load: func(context.Context, string) error { t.Fatal("builtin loaded module"); return nil }}
	if err := ensureTUN(context.Background(), "missing", tunModuleSpec{}, hooks); err != nil {
		t.Fatal(err)
	}
}

func TestTUNBootstrapLoadsOnlyVerifiedMatchingModuleAndReprobes(t *testing.T) {
	path, spec := tunFixture(t)
	probes, loads := 0, 0
	hooks := tunHooks{probe: func() error {
		probes++
		if loads == 0 {
			return syscall.ENODEV
		}
		return nil
	}, kernel: func() (string, error) { return spec.Kernel, nil }, load: func(_ context.Context, p string) error {
		if p != path {
			t.Fatal("unexpected loader argument", p)
		}
		loads++
		return nil
	}}
	if err := ensureTUN(context.Background(), path, spec, hooks); err != nil {
		t.Fatal(err)
	}
	if probes != 2 || loads != 1 {
		t.Fatal("module was not loaded and reprobed", probes, loads)
	}
}

func TestTUNBootstrapRefusesUnsupportedOrChangedInputsBeforeLoading(t *testing.T) {
	for _, kind := range []string{"kernel", "missing", "tampered", "symlink", "permissions", "architecture", "vermagic", "layout"} {
		t.Run(kind, func(t *testing.T) {
			path, spec := tunFixture(t)
			kernel := spec.Kernel
			switch kind {
			case "kernel":
				kernel = "4.1.15-another-kernel"
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "tampered":
				data, _ := os.ReadFile(path)
				data[60] ^= 1
				os.WriteFile(path, data, 0644)
			case "symlink":
				target := path + ".original"
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(path, 0666); err != nil {
					t.Fatal(err)
				}
			case "architecture":
				data, _ := os.ReadFile(path)
				binary.LittleEndian.PutUint16(data[18:], 183)
				os.WriteFile(path, data, 0644)
				spec.SHA256 = digest(data)
			case "vermagic":
				spec.VersionMagic = "different kernel flags"
			case "layout":
				spec.ModuleLayout = "0x00000001"
			}
			hooks := tunHooks{probe: func() error { return syscall.ENODEV }, kernel: func() (string, error) { return kernel, nil }, load: func(context.Context, string) error { t.Fatal("unverified module was loaded"); return nil }}
			if err := ensureTUN(context.Background(), path, spec, hooks); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestTUNBootstrapHandlesLoadFailureAndAlreadyLoadedRace(t *testing.T) {
	for _, race := range []bool{false, true} {
		path, spec := tunFixture(t)
		loaded := false
		hooks := tunHooks{probe: func() error {
			if loaded && race {
				return nil
			}
			return syscall.ENODEV
		}, kernel: func() (string, error) { return spec.Kernel, nil }, load: func(context.Context, string) error { loaded = true; return errors.New("loader failed") }}
		err := ensureTUN(context.Background(), path, spec, hooks)
		if race && err != nil {
			t.Fatal("working concurrently loaded driver was rejected", err)
		}
		if !race && (err == nil || !strings.Contains(err.Error(), "loader failed")) {
			t.Fatal("loader failure hidden", err)
		}
	}
}

func TestTUNBootstrapPreservesPermissionAndCancellationErrors(t *testing.T) {
	hooks := tunHooks{probe: func() error { return syscall.EACCES }, kernel: func() (string, error) { t.Fatal("permission error attempted autoload"); return "", nil }}
	if err := ensureTUN(context.Background(), "", tunModuleSpec{}, hooks); !errors.Is(err, syscall.EACCES) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hooks.probe = func() error { return syscall.ENODEV }
	if err := ensureTUN(ctx, "", tunModuleSpec{}, hooks); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRAMTUNAuthenticatesPublicFilesAndRetainsNoUSBDependency(t *testing.T) {
	e, _ := setupFixture(t)
	enableRAMFixture(t, e)
	fixture, spec := tunFixture(t)
	e.paths.TUNModule = filepath.Join(e.paths.Books, "tun.ko")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.paths.TUNModule, data, 0666); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(e.paths.RAM, "kernel/tun.ko")
	if err := e.ensureRAMTUN(context.Background(), target, spec); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(e.paths.Books, e.paths.Books+"-unmounted"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(e.paths.usbBlockPath(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := e.ensureRAMTUN(context.Background(), target, spec); err != nil {
		t.Fatal("verified module still depends on books", err)
	}
	data[60] ^= 1
	if err := os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.ensureRAMTUN(context.Background(), target, spec); err == nil {
		t.Fatal("changed RAM module accepted while public source unavailable")
	}
}
