package helper

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceRejectsUnverifiedExecutableType(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "tailscale")
	if err := os.WriteFile(filename, []byte("#!/bin/sh\necho 1.104.4\n"), 0755); err != nil {
		t.Fatal(err)
	}
	s := &osService{}
	if _, err := s.Version(context.Background(), filename); err == nil {
		t.Fatal("non-ARM executable accepted")
	}
}
