package helper

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const maxExpanded = 256 << 20

type archivePlan struct {
	Expanded int64
	Files    map[string]int64
}

func inspectArchive(filename, version string) (archivePlan, error) {
	plan := archivePlan{Files: map[string]int64{}}
	if !stableVersion(version) {
		return plan, errors.New("invalid stable version")
	}
	f, err := os.Open(filename)
	if err != nil {
		return plan, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return plan, err
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxExpanded + 1}
	tr := tar.NewReader(limited)
	prefix := "tailscale_" + version + "_arm/"
	seen := map[string]bool{}
	for count := 0; ; count++ {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return plan, fmt.Errorf("archive header: %w", err)
		}
		if count >= 24 {
			return plan, errors.New("too many archive entries")
		}
		name := strings.TrimSuffix(header.Name, "/")
		if path.Clean(name) != name || strings.Contains(name, "\\") || seen[name] {
			return plan, errors.New("unsafe or duplicate archive path")
		}
		seen[name] = true
		if header.Typeflag == tar.TypeDir {
			if name != strings.TrimSuffix(prefix, "/") && name != prefix+"systemd" {
				return plan, errors.New("unexpected archive directory")
			}
			continue
		}
		if header.Typeflag != tar.TypeReg || !strings.HasPrefix(name, prefix) || len(header.PAXRecords) > 0 {
			return plan, errors.New("unexpected archive file or link")
		}
		relative := strings.TrimPrefix(name, prefix)
		switch relative {
		case "tailscale", "tailscaled":
			if header.Size <= 0 || header.Size > 96<<20 {
				return plan, errors.New("invalid executable size")
			}
			plan.Files[relative] = header.Size
			plan.Expanded += header.Size
		case "systemd/tailscaled.service", "systemd/tailscaled.defaults", "systemd/tailscale-online.target", "systemd/tailscale-wait-online.service", "LICENSE", "COPYING":
			if header.Size < 0 || header.Size > 1<<20 {
				return plan, errors.New("invalid ancillary file size")
			}
		default:
			return plan, errors.New("unexpected archive payload")
		}
	}
	// Consume gzip's checksum and allow only tar's bounded zero padding.
	tail, err := io.ReadAll(limited)
	if err != nil {
		return plan, err
	}
	if limited.N <= 0 || len(tail) > 64<<10 {
		return plan, errors.New("expanded archive exceeds limit")
	}
	for _, b := range tail {
		if b != 0 {
			return plan, errors.New("nonzero data after archive")
		}
	}
	if len(plan.Files) != 2 {
		return plan, errors.New("archive must contain one CLI and one daemon")
	}
	return plan, nil
}
