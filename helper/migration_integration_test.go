package helper

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealMigrateThenPrepare(t *testing.T) {
	e, _ := setupFixture(t)
	onboard := filepath.Dir(filepath.Dir(e.paths.Version))
	source := filepath.Join(onboard, "tailscale")
	commands := filepath.Join(t.TempDir(), "commands")
	for _, dir := range []string{source, commands, e.paths.Base, e.paths.Links} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	put := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"tailscale", "tailscaled"} {
		put(filepath.Join(source, name), []byte("fixture binary"))
		if err := os.Symlink(filepath.Join(source, name), filepath.Join(e.paths.Links, name)); err != nil {
			t.Fatal(err)
		}
	}
	state := []byte("saved identity from books")
	put(filepath.Join(source, "tailscaled.state"), state)
	put(filepath.Join(e.paths.Base, "on-boot.sh"), []byte("#!/bin/sh\n"+source+"/tailscaled\n"))
	put(filepath.Join(e.paths.Base, "boot.sh"), []byte("#!/bin/sh\n/bin/sh "+e.paths.Base+"/on-boot.sh\n"))
	for _, name := range []string{"on-boot-root.sh", "connect-root.sh", "login-root.sh", "diagnose-root.sh", "runtime-boot.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		put(filepath.Join(e.paths.Helpers, name), data)
	}
	for _, name := range []string{"pidof", "pgrep"} {
		put(filepath.Join(commands, name), []byte("#!/bin/sh\nexit 1\n"))
	}
	put(filepath.Join(commands, "df"), []byte("#!/bin/sh\necho 'Filesystem 1024-blocks Used Available Capacity Mounted'\necho 'fixture 2000000 0 2000000 0% /'\n"))
	mounts := filepath.Join(commands, "mounts")
	put(mounts, []byte("/dev/fixture "+onboard+" vfat rw 0 0\n"))
	data, err := os.ReadFile("../scripts/migrate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.NewReplacer("/usr/local/tailscale-runtime", e.paths.Runtime, "/usr/local/tailscale-ui", e.paths.Helpers, "/usr/local/tailscale", e.paths.Base, "/usr/bin/tailscale", filepath.Join(e.paths.Links, "tailscale"), "/mnt/onboard", onboard, "/tmp/kobo-tailscale-connect.lock", e.paths.Lock, "/proc/mounts", mounts, "PATH=/sbin:/bin:/usr/sbin:/usr/bin", "PATH="+commands+":"+os.Getenv("PATH")).Replace(string(data))
	path := filepath.Join(commands, "migrate.sh")
	put(path, []byte(script))
	output, err := exec.Command("/bin/sh", path).CombinedOutput()
	if err != nil {
		t.Fatal("migration failed", err, string(output))
	}
	if err := e.Prepare(context.Background()); err != nil {
		t.Fatal("real migration could not be adopted", err)
	}
	got, _ := os.ReadFile(filepath.Join(e.paths.Runtime, "state/tailscaled.state"))
	if string(got) != string(state) {
		t.Fatal("adoption changed migrated identity")
	}
}
