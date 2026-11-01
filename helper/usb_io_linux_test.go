//go:build linux

package helper

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestPublicIOChild(t *testing.T) {
	if os.Getenv("KTAILSCALE_IO_CHILD") != "1" {
		return
	}
	var paths Paths
	if err := json.Unmarshal([]byte(os.Getenv("KTAILSCALE_IO_PATHS")), &paths); err != nil {
		t.Fatal(err)
	}
	e := &Engine{paths: paths}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()
	release, err := e.acquirePublicIO(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	file, err := os.Open(filepath.Join(paths.Books, "1.104.4/tailscale"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Runtime, "child-open"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Runtime, "child-closed"), []byte("closed"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestUSBEntryCancelsOnlyOwnedProcessAndWaitsForItsFileClose(t *testing.T) {
	e, _ := ramFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.paths.CandidateHelper = executable
	paths, err := json.Marshal(e.paths)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestPublicIOChild$")
	child.Env = append(os.Environ(), "KTAILSCALE_IO_CHILD=1", "KTAILSCALE_IO_PATHS="+string(paths))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(e.paths.Runtime, "child-open")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not acquire public I/O")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := e.BeginUSB(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.paths.Runtime, "child-closed")); err != nil {
		t.Fatal("export proceeded before the public file was closed", err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal("owned writer did not shut down cleanly", err)
	}
}
