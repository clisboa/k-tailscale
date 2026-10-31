//go:build linux

package helper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func factoryDirectory(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("ownership fixture requires container root")
	}
	if err := os.MkdirAll(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 3018, 3018); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
func TestPrepareAcceptsFactoryOwnedSystemLinksWithoutChangingOwnership(t *testing.T) {
	e, _ := setupFixture(t)
	factoryDirectory(t, e.paths.Links, 0755)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal("supported Kobo firmware layout rejected", err)
	}
	info, err := os.Stat(e.paths.Links)
	if err != nil {
		t.Fatal(err)
	}
	if info.Sys().(*syscall.Stat_t).Uid != 3018 || info.Mode().Perm() != 0755 {
		t.Fatal("shared factory directory metadata changed")
	}
}
func TestAddonParentsAcceptFactoryUsrWithoutChangingMetadata(t *testing.T) {
	root := t.TempDir()
	factoryDirectory(t, filepath.Join(root, "usr"), 0775)
	if err := safeParents(root, "usr/local/Kobo/imageformats/libtailscaleui.so"); err != nil {
		t.Fatal("factory addon ancestors rejected", err)
	}
	info, _ := os.Stat(filepath.Join(root, "usr"))
	if info.Sys().(*syscall.Stat_t).Uid != 3018 || info.Mode().Perm() != 0775 {
		t.Fatal("shared factory ancestor metadata changed")
	}
}
func TestPrivateRuntimeStillRejectsFactoryOwnerWithDiagnosticPath(t *testing.T) {
	e, _ := setupFixture(t)
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(e.paths.Runtime, "state")
	factoryDirectory(t, state, 0700)
	err := e.Prepare(context.Background())
	if err == nil {
		t.Fatal("foreign-owned private state accepted")
	}
	if !strings.Contains(err.Error(), state) || !strings.Contains(err.Error(), "3018") {
		t.Fatal("ownership error does not identify failed directory", err)
	}
}

func TestFactoryOwnershipExceptionDoesNotAcceptUnrelatedOwnersOrWritablePaths(t *testing.T) {
	for _, kind := range []string{"unknown-owner", "world-writable", "private-helper"} {
		t.Run(kind, func(t *testing.T) {
			e, _ := setupFixture(t)
			factoryDirectory(t, e.paths.Links, 0755)
			switch kind {
			case "unknown-owner":
				if err := os.Chown(e.paths.Links, 3019, 3019); err != nil {
					t.Fatal(err)
				}
			case "world-writable":
				if err := os.Chmod(e.paths.Links, 0777); err != nil {
					t.Fatal(err)
				}
			case "private-helper":
				factoryDirectory(t, e.paths.Base, 0755)
			}
			if err := e.Prepare(context.Background()); err == nil {
				t.Fatal("unsafe ownership or permissions accepted")
			}
		})
	}
}

func TestAddonInstallPreservesFactoryUsrAndPrivateIdentity(t *testing.T) {
	e, m, archive := addonFixture(t)
	usr := filepath.Join(e.paths.SystemRoot, "usr")
	factoryDirectory(t, usr, 0775)
	if err := e.applyAddon(archive, m); err != nil {
		t.Fatal("addon installation rejected supported firmware", err)
	}
	data, err := os.ReadFile(filepath.Join(e.paths.Runtime, "state/identity"))
	if err != nil || string(data) != "saved disconnected identity" {
		t.Fatal("factory compatibility changed identity", err)
	}
	info, err := os.Stat(usr)
	if err != nil {
		t.Fatal(err)
	}
	if info.Sys().(*syscall.Stat_t).Uid != 3018 || info.Mode().Perm() != 0775 {
		t.Fatal("addon changed shared firmware metadata")
	}
}
