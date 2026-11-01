package helper

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUSBExportBlocksNewPublicWorkAndWaitsForHandles(t *testing.T) {
	e, _ := ramFixture(t)
	release, err := e.acquirePublicIO(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- e.BeginUSB(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		blocked, err := e.usbBlocked()
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("export did not block new I/O")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-finished:
		t.Fatalf("export proceeded before the current I/O closed: %v", err)
	default:
	}
	if next, err := e.acquirePublicIO(ctx); err == nil {
		next()
		t.Fatal("new books writer started during USB export")
	}
	release()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := e.EndUSB(); err != nil {
		t.Fatal(err)
	}
	release, err = e.acquirePublicIO(ctx)
	if err != nil {
		t.Fatal("remount did not restore public I/O", err)
	}
	release()
}

func TestUSBTimeoutFailsClosedAndMissingRemountStaysBlocked(t *testing.T) {
	e, _ := ramFixture(t)
	release, err := e.acquirePublicIO(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := e.BeginUSB(ctx); err == nil {
		t.Fatal("active writer did not prevent export")
	}
	release()
	if err := os.Rename(e.paths.Books, e.paths.Books+"-unmounted"); err != nil {
		t.Fatal(err)
	}
	if err := e.EndUSB(); err == nil {
		t.Fatal("missing remount was accepted")
	}
	blocked, err := e.usbBlocked()
	if err != nil || !blocked {
		t.Fatal("failed export/remount lost its I/O gate", err)
	}
}

func TestUSBWrapperValidatesOriginalAndPreservesVendorStatus(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.sh")
	helper := filepath.Join(dir, "helper.sh")
	data := []byte("#!/bin/sh\necho vendor >> '" + filepath.Join(dir, "calls") + "'\nexit 7\n")
	if err := os.WriteFile(original, data, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := usbWrapper(data, "wrong", helper, original); err == nil {
		t.Fatal("unknown vendor script accepted")
	}
	if _, err := usbWrapper(data, digest(data), helper, original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho \"$1\" >> '"+
		filepath.Join(dir, "calls")+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	script, err := usbWrapper(data, digest(data), helper, original)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "wrapper.sh")
	if err := os.WriteFile(wrapper, script, 0700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/sh", wrapper)
	command.Env = append(os.Environ(), "ACTION=add")
	if err := command.Run(); err == nil || command.ProcessState.ExitCode() != 7 {
		t.Fatal("vendor exit status was not retained", err)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil || strings.TrimSpace(string(calls)) != "usb-begin\nvendor\nusb-end" {
		t.Fatalf("wrong USB ordering: %q (%v)", calls, err)
	}
}
