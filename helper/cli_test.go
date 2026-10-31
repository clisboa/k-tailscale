package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIUsageAndVersion(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"fetch-release"}, {"fetch-release", "--version", "1.105.0", "--directory", t.TempDir()}} {
		var out, errout bytes.Buffer
		if code := Run(context.Background(), args, &out, &errout); code != 2 {
			t.Fatalf("%v: code %d", args, code)
		}
		if out.Len() != 0 || errout.Len() == 0 {
			t.Fatal("usage output must go to stderr")
		}
	}
	var out, errout bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &out, &errout); code != 0 {
		t.Fatal(code)
	}
	var info map[string]string
	if err := json.Unmarshal(out.Bytes(), &info); err != nil || info["version"] == "" {
		t.Fatalf("bad version output: %q", out.String())
	}
}

func TestLogoutIsRecognizedWithoutExtraArguments(t *testing.T) {
	var out, errout bytes.Buffer
	if code := Run(context.Background(), []string{"logout"}, &out, &errout); code != 1 {
		t.Fatalf("logout must be a recognized Kobo-only operation, got %d", code)
	}
	out.Reset()
	errout.Reset()
	if code := Run(context.Background(), []string{"logout", "extra"}, &out, &errout); code != 2 {
		t.Fatal(code)
	}
}

func TestCLIOfflineVerificationRejectsUnsignedManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "release.json")
	if err := os.WriteFile(manifest, []byte(`{"version":"1.104.4"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	code := Run(context.Background(), []string{"verify-payload", "--manifest", manifest, "--archive", fixtureArchive(t, validHeaders())}, &out, &errout)
	if code != 1 || out.Len() != 0 || errout.Len() == 0 {
		t.Fatalf("unsigned manifest: code=%d stdout=%q stderr=%q", code, out.String(), errout.String())
	}
}
