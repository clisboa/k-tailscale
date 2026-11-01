package helper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func pinDirectory(directory, version string) (releasePin, error) {
	pin := releasePin{Version: version, Files: map[string]executablePin{}}
	for _, name := range []string{"tailscale", "tailscaled"} {
		path := filepath.Join(directory, name)
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
		if err != nil {
			return releasePin{}, err
		}
		file := os.NewFile(uintptr(fd), path)
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxExpanded {
			file.Close()
			return releasePin{}, errors.New("invalid executable while pinning verified release")
		}
		hash := sha256.New()
		n, err := io.Copy(hash, io.LimitReader(file, maxExpanded+1))
		file.Close()
		if err != nil {
			return releasePin{}, err
		}
		if n != info.Size() || n > maxExpanded {
			return releasePin{}, errors.New("executable changed while computing protected hash")
		}
		pin.Files[name] = executablePin{Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}
	}
	_, err := validatePin(pin)
	return pin, err
}

func (e *Engine) ensurePublicDirectory(path string) error {
	base := e.paths.Books
	if e.paths.BooksMount != "" {
		base = e.paths.BooksMount
	}
	relative, err := filepath.Rel(base, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("public artifact path leaves the books directory")
	}
	if err := rejectDirectoryLinks(base); os.IsNotExist(err) && base == e.paths.Books {
		if err := rejectDirectoryLinks(filepath.Dir(base)); err != nil {
			return err
		}
		if err := os.Mkdir(base, 0755); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	current := base
	if relative == "." {
		return nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe public artifact directory")
		}
	}
	return nil
}

func syncPublicDirectory(path string) error {
	err := syncDirectory(path)
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		// Older FAT drivers lack directory fsync. File contents were synced
		// individually; sync also flushes their directory entries before pins.
		syscall.Sync()
		return nil
	}
	return err
}

// The caller holds public I/O. Only a complete, verified directory gets its
// stable version name; incomplete directories are never selected by root pins.
func (e *Engine) persistPublicPair(ctx context.Context, source string, pin releasePin) error {
	bytes, err := validatePin(pin)
	if err != nil {
		return err
	}
	if err := e.ensurePublicDirectory(e.paths.Books); err != nil {
		return err
	}
	final := filepath.Join(e.paths.Books, pin.Version)
	if _, err := os.Lstat(final); err == nil {
		if err := rejectDirectoryLinks(final); err != nil {
			return err
		}
		existing, err := pinDirectory(final, pin.Version)
		if err != nil || pinDigest(existing) != pinDigest(pin) {
			return errors.New("existing public version differs; its files were retained")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	free, err := e.free(e.paths.Books)
	if err != nil {
		return err
	}
	if free < bytes+spaceReserve {
		return errors.New("not enough books storage for the verified executable pair")
	}
	stage, err := os.MkdirTemp(e.paths.Books, ".pair-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for name, expected := range pin.Files {
		if err := copyPinned(ctx, filepath.Join(source, name), filepath.Join(stage, name), expected, 0644); err != nil {
			return err
		}
	}
	if err := syncPublicDirectory(stage); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(stage, final); err != nil {
		return err
	}
	return syncPublicDirectory(e.paths.Books)
}

// Public archive bytes are copied into private RAM and authenticated again.
// No binary is executed or pinned from a mutable public archive directly.
func (e *Engine) stageSignedRelease(ctx context.Context, path string, claimed Artifact) (releasePin, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return releasePin{}, "", err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxDownload {
		return releasePin{}, "", errors.New("invalid public release archive")
	}
	if err := e.requireRAM(info.Size()); err != nil {
		return releasePin{}, "", err
	}
	stage, err := os.MkdirTemp(e.paths.RAM, "signed-")
	if err != nil {
		return releasePin{}, "", err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(stage)
		}
	}()
	snapshot := filepath.Join(stage, "release.tgz")
	dataPin := executablePin{Size: info.Size()}
	// The distribution signature below authenticates the complete snapshot;
	// a hash from the public source is not a trust anchor.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return releasePin{}, "", err
	}
	in := os.NewFile(uintptr(fd), path)
	opened, err := in.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() != info.Size() {
		in.Close()
		return releasePin{}, "", errors.New("public release changed before snapshot")
	}
	out, err := os.OpenFile(snapshot, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		in.Close()
		return releasePin{}, "", err
	}
	n, copyErr := copyContext(ctx, out, in, dataPin.Size)
	err = errors.Join(copyErr, in.Close(), out.Sync(), out.Close())
	if err != nil || n != dataPin.Size {
		return releasePin{}, "", errors.Join(err, errors.New("public release snapshot was incomplete"))
	}
	artifact, err := e.upstream.VerifyLocal(snapshot, claimed.Version, claimed.Proof)
	if err != nil {
		return releasePin{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return releasePin{}, "", err
	}
	if err := e.requireRAM(artifact.Expanded); err != nil {
		return releasePin{}, "", err
	}
	bin := filepath.Join(stage, "bin")
	if err := extractArtifact(ctx, artifact, bin); err != nil {
		return releasePin{}, "", err
	}
	if version, err := e.pairVersion(ctx, bin); err != nil || version != artifact.Version {
		return releasePin{}, "", errors.Join(err, errors.New("verified RAM executables report the wrong version"))
	}
	pin, err := pinDirectory(bin, artifact.Version)
	if err != nil {
		return releasePin{}, "", err
	}
	if err := os.Remove(snapshot); err != nil {
		return releasePin{}, "", err
	}
	ok = true
	return pin, bin, nil
}
