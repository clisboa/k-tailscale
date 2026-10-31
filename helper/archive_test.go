package helper

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureArchive(t *testing.T, entries []*tar.Header) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "release.tgz")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(entry); err != nil {
			t.Fatal(err)
		}
		if entry.Size > 0 {
			if _, err := tw.Write([]byte(strings.Repeat("x", int(entry.Size)))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return filename
}

func validHeaders() []*tar.Header {
	return []*tar.Header{
		{Name: "tailscale_1.104.4_arm/tailscale", Size: 17, Mode: 0755, Typeflag: tar.TypeReg},
		{Name: "tailscale_1.104.4_arm/tailscaled", Size: 23, Mode: 0755, Typeflag: tar.TypeReg},
	}
}

func TestArchiveExpandedSize(t *testing.T) {
	plan, err := inspectArchive(fixtureArchive(t, validHeaders()), "1.104.4")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Expanded != 40 || len(plan.Files) != 2 {
		t.Fatalf("incorrect space accounting: %+v", plan)
	}
}

func TestArchiveRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../tailscale", "/etc/passwd", "tailscale_1.104.4_arm/../tailscale", "tailscale_1.104.4_arm/a/tailscale", "other/tailscale"} {
		t.Run(name, func(t *testing.T) {
			entries := validHeaders()
			entries[0].Name = name
			if _, err := inspectArchive(fixtureArchive(t, entries), "1.104.4"); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestArchiveRejectsLinksDuplicatesAndMissing(t *testing.T) {
	entries := validHeaders()
	entries[0].Typeflag = tar.TypeSymlink
	entries[0].Size = 0
	entries[0].Linkname = "/bin/sh"
	for _, headers := range [][]*tar.Header{entries, append(validHeaders(), validHeaders()[0]), validHeaders()[:1]} {
		if _, err := inspectArchive(fixtureArchive(t, headers), "1.104.4"); err == nil {
			t.Fatal("invalid archive accepted")
		}
	}
}
