package helper

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func syncDirectory(directory string) error {
	f, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func writeAtomic(filename string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(filename), ".k-tailscale-write-*")
	if err != nil {
		return err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filename); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(filename))
}

func writeJSON(filename string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeAtomic(filename, append(data, '\n'), 0600)
}

func readJSON(filename string, value any) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 256<<10+1))
	if err != nil {
		return err
	}
	if len(data) > 256<<10 {
		return errors.New("local metadata exceeds size limit")
	}
	return json.Unmarshal(data, value)
}

func ensureDirectory(directory string, mode os.FileMode) error {
	return ensureDirectoryPolicy(directory, mode, 0)
}

// Kobo 4.46.23852 ships shared system directories under build UID/GID 3018.
// Accept that exact factory metadata only at explicitly selected OS paths.
// Private runtime/state/helper directories always use ensureDirectory instead.
func ensureSystemDirectory(directory string, factoryMode os.FileMode) error {
	return ensureDirectoryPolicy(directory, 0755, factoryMode)
}

func ensureDirectoryPolicy(directory string, mode, factoryMode os.FileMode) error {
	if err := os.MkdirAll(directory, mode); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe directory: %s", directory)
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot verify directory owner: %s", directory)
	}
	permissions := info.Mode().Perm()
	privateOwner := metadata.Uid == uint32(os.Geteuid())
	factoryOwner := factoryMode != 0 && metadata.Uid == 3018 && metadata.Gid == 3018
	if !privateOwner && !factoryOwner {
		return fmt.Errorf("directory %s has an unexpected owner %d (expected %d)", directory, metadata.Uid, os.Geteuid())
	}
	safePermissions := permissions&0022 == 0
	if factoryOwner && permissions & ^factoryMode == 0 {
		safePermissions = true
	}
	if !safePermissions {
		return fmt.Errorf("unsafe directory permissions: %s (%#o)", directory, permissions)
	}
	return nil
}

func acquireActionLock(directory string) (func(), error) {
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, errors.New("another Tailscale action is active; restart after an interrupted action")
	}
	return func() { _ = os.Remove(directory) }, nil
}

func acquireServiceLock(ctx context.Context, filename string) (func(), error) {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func extractArtifact(ctx context.Context, artifact Artifact, directory string) error {
	if err := ensureDirectory(directory, 0700); err != nil {
		return err
	}
	f, err := os.Open(artifact.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, maxExpanded+1))
	prefix := "tailscale_" + artifact.Version + "_arm/"
	written := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		for _, name := range []string{"tailscale", "tailscaled"} {
			if header.Name != prefix+name {
				continue
			}
			if written[name] || header.Typeflag != tar.TypeReg || header.Size != artifact.Files[name] {
				return errors.New("verified archive changed during extraction")
			}
			out, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
			if err != nil {
				return err
			}
			n, copyErr := io.Copy(out, tr)
			syncErr := out.Sync()
			closeErr := out.Close()
			if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
				return err
			}
			if n != header.Size {
				return io.ErrUnexpectedEOF
			}
			written[name] = true
		}
	}
	if len(written) != 2 {
		return errors.New("executable pair is incomplete")
	}
	return syncDirectory(directory)
}

func copyPrivate(source, target string, limit int64) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(in, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return errors.New("copy exceeds size limit")
	}
	return out.Sync()
}
