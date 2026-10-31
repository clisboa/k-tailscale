package helper

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed addon-public.pem
var addonPublicPEM []byte

const projectReleaseBase = "https://github.com/clisboa/k-tailscale/releases/"
const maxAddonDownload = 24 << 20
const maxAddonExpanded = 32 << 20

var addonScripts = []string{"control.sh", "migrate.sh", "on-boot-root.sh", "connect-root.sh", "login-root.sh", "diagnose-root.sh", "setup.sh", "runtime-boot.sh"}

type addonManifest struct {
	Schema    int    `json:"schema"`
	Version   string `json:"version"`
	MinHelper string `json:"min_helper"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	URL       string `json:"url"`
}
type addonSource struct {
	upstream *Upstream
	base     string
	key      ed25519.PublicKey
}

func newAddonSource(u *Upstream) (*addonSource, error) {
	b, _ := pem.Decode(addonPublicPEM)
	if b == nil {
		return nil, errors.New("invalid embedded addon key")
	}
	parsed, err := x509.ParsePKIXPublicKey(b.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("invalid addon key type")
	}
	return &addonSource{upstream: u, base: projectReleaseBase, key: key}, nil
}
func (s *addonSource) manifest(ctx context.Context) (addonManifest, error) {
	u := *s.upstream
	u.baseURL = s.base
	data, err := u.small(ctx, "latest/download/update.json", 64<<10)
	if err != nil {
		return addonManifest{}, err
	}
	sig, err := u.small(ctx, "latest/download/update.json.sig", ed25519.SignatureSize)
	if err != nil {
		return addonManifest{}, err
	}
	return s.verifyManifest(data, sig)
}
func (s *addonSource) verifyManifest(data, sig []byte) (addonManifest, error) {
	var m addonManifest
	if len(data) > 64<<10 || !ed25519.Verify(s.key, data, sig) {
		return m, errors.New("addon release signature was rejected")
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	_, valid := parseVersion(m.Version)
	_, validMin := parseVersion(m.MinHelper)
	if m.Schema != 1 || !valid || !validMin || m.Size <= 0 || m.Size > maxAddonDownload || len(m.SHA256) != 64 {
		return m, errors.New("invalid addon release metadata")
	}
	if _, err := hex.DecodeString(m.SHA256); err != nil {
		return m, err
	}
	expected := s.base + "download/v" + m.Version + "/k-tailscale-update.tgz"
	if m.URL != expected {
		return m, errors.New("unexpected addon release URL")
	}
	return m, nil
}
func addonScope(v string) map[string]os.FileMode {
	scope := map[string]os.FileMode{"usr/local/Kobo/imageformats/libtailscaleui.so": 0755, "usr/local/tailscale-ui/release-version": 0644, "usr/local/k-tailscale/releases/" + v + "/helper": 0755, "usr/local/k-tailscale/releases/" + v + "/THIRD_PARTY_NOTICES.txt": 0644}
	for _, name := range addonScripts {
		scope["usr/local/tailscale-ui/"+name] = 0755
	}
	return scope
}
func extractAddon(filename, dir, v string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	limited := &io.LimitedReader{R: gz, N: maxAddonExpanded + 1}
	tr := tar.NewReader(limited)
	scope := addonScope(v)
	seen := map[string]bool{}
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		count++
		mode, ok := scope[h.Name]
		if !ok || seen[h.Name] || count > len(scope) || h.Typeflag != tar.TypeReg || len(h.PAXRecords) > 0 || h.Size < 0 || h.Size > 16<<20 || os.FileMode(h.Mode) != mode {
			return errors.New("addon archive has an unexpected path, mode or entry")
		}
		dest := filepath.Join(dir, h.Name)
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		err = errors.Join(copyErr, out.Sync(), out.Close())
		if err != nil {
			return err
		}
		seen[h.Name] = true
	}
	// Validate gzip checksum and reject hidden nonzero data after tar EOF.
	tail, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if limited.N <= 0 || len(tail) > 1<<20 {
		return errors.New("addon archive exceeds size limit")
	}
	for _, b := range tail {
		if b != 0 {
			return errors.New("addon archive contains trailing data")
		}
	}
	if len(seen) != len(scope) {
		return errors.New("addon archive is incomplete")
	}
	data, err := os.ReadFile(filepath.Join(dir, "usr/local/tailscale-ui/release-version"))
	if err != nil || strings.TrimSpace(string(data)) != v {
		return errors.New("addon version file mismatch")
	}
	return nil
}

type addonFile struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Mode    uint32 `json:"mode"`
}
type addonJournal struct {
	Schema  int         `json:"schema"`
	Phase   string      `json:"phase"`
	Version string      `json:"version"`
	Files   []addonFile `json:"files"`
}

func (e *Engine) addonPath(name string) string { return filepath.Join(e.paths.SystemRoot, name) }
func (e *Engine) addonJournalPath() string {
	return filepath.Join(e.updateRoot(), "addon-transaction.json")
}
func (e *Engine) recoverAddon() error {
	var j addonJournal
	if err := readJSON(e.addonJournalPath(), &j); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	_, validVersion := parseVersion(j.Version)
	if j.Schema != 1 || !validVersion {
		return errors.New("invalid addon recovery journal")
	}
	scope := addonScope(j.Version)
	seen := map[string]bool{}
	if len(j.Files) != len(scope) {
		return errors.New("incomplete addon recovery scope")
	}
	for _, f := range j.Files {
		if _, ok := scope[f.Name]; !ok || seen[f.Name] || f.Mode&^0777 != 0 {
			return errors.New("invalid addon recovery entry")
		}
		seen[f.Name] = true
	}
	txn := filepath.Join(e.updateRoot(), "addon-transaction")
	switch j.Phase {
	case "applying":
		for _, f := range j.Files {
			target := e.addonPath(f.Name)
			if f.Present {
				data, err := os.ReadFile(filepath.Join(txn, "backup", f.Name))
				if err != nil {
					return err
				}
				if err := writeAtomic(target, data, os.FileMode(f.Mode)); err != nil {
					return err
				}
			} else {
				if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
					return err
				}
				if err := syncDirectory(filepath.Dir(target)); err != nil {
					return err
				}
			}
		}
		j.Phase = "rolled-back"
		if err := writeJSON(e.addonJournalPath(), j); err != nil {
			return err
		}
	case "rolled-back":
		// Backups may already be partly deleted. Restored live files are durable.
	case "committed":
		if err := writeAtomic(filepath.Join(e.updateRoot(), "restart-required"), []byte(j.Version+"\n"), 0600); err != nil {
			return err
		}
	default:
		return errors.New("unrecognized addon recovery phase")
	}
	if err := os.RemoveAll(txn); err != nil {
		return err
	}
	if err := os.Remove(e.addonJournalPath()); err != nil {
		return err
	}
	return syncDirectory(e.updateRoot())
}
func safeParents(root, name string) error {
	current, relative := root, ""
	for _, part := range strings.Split(filepath.Dir(name), "/") {
		current = filepath.Join(current, part)
		relative = filepath.Join(relative, part)
		var err error
		switch relative {
		case "usr":
			err = ensureSystemDirectory(current, 0775)
		case "usr/local", "usr/local/Kobo", "usr/local/Kobo/imageformats":
			err = ensureSystemDirectory(current, 0755)
		default:
			err = ensureDirectory(current, 0755)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) applyAddon(archive string, m addonManifest) error {
	if err := e.recoverAddon(); err != nil {
		return err
	}
	txn := filepath.Join(e.updateRoot(), "addon-transaction")
	if err := os.RemoveAll(txn); err != nil {
		return err
	}
	if err := ensureDirectory(txn, 0700); err != nil {
		return err
	}
	stage := filepath.Join(txn, "candidate")
	if err := extractAddon(archive, stage, m.Version); err != nil {
		return err
	}
	candidate := filepath.Join(stage, "usr/local/k-tailscale/releases/"+m.Version+"/helper")
	data, err := commandOutput(context.Background(), candidate, "version")
	if err != nil {
		return err
	}
	var built struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &built) != nil || built.Version != m.Version {
		return errors.New("addon helper build version mismatch")
	}
	j := addonJournal{Schema: 1, Phase: "applying", Version: m.Version}
	names := []string{}
	for name := range addonScope(m.Version) {
		names = append(names, name)
	}
	sort.Strings(names)
	// Replace the loaded library last. The active core helper remains unchanged;
	// the next successful Nickel setup promotes the new versioned helper.
	lib := "usr/local/Kobo/imageformats/libtailscaleui.so"
	names = append(append([]string{}, names[1:]...), names[0])
	if names[len(names)-1] != lib {
		return errors.New("addon library ordering is invalid")
	}
	var backupSize int64
	for _, name := range names {
		if err := safeParents(e.paths.SystemRoot, name); err != nil {
			return err
		}
		target := e.addonPath(name)
		f := addonFile{Name: name}
		info, err := os.Lstat(target)
		if err == nil {
			if !info.Mode().IsRegular() || info.Size() > 16<<20 {
				return errors.New("custom addon path was retained")
			}
			f.Present = true
			f.Mode = uint32(info.Mode().Perm())
			backupSize += info.Size()
			dest := filepath.Join(txn, "backup", name)
			if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
				return err
			}
			if err := os.Link(target, dest); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		j.Files = append(j.Files, f)
	}
	if err := e.requireSpace(backupSize); err != nil {
		return err
	}
	if err := syncTreeDirectories(txn); err != nil {
		return err
	}
	for _, dir := range []string{e.updateRoot(), e.paths.Runtime} {
		if err := e.persistDirectory(dir); err != nil {
			return err
		}
	}
	if err := writeJSON(e.addonJournalPath(), j); err != nil {
		return err
	}
	for _, f := range j.Files {
		data, err := os.ReadFile(filepath.Join(stage, f.Name))
		if err != nil {
			return errors.Join(err, e.recoverAddon())
		}
		if err := writeAtomic(e.addonPath(f.Name), data, addonScope(m.Version)[f.Name]); err != nil {
			return errors.Join(err, e.recoverAddon())
		}
	}
	j.Phase = "committed"
	if err := writeJSON(e.addonJournalPath(), j); err != nil {
		return err
	}
	return e.recoverAddon()
}
func syncTreeDirectories(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return syncDirectory(path)
		}
		return nil
	})
}
func (e *Engine) RecoverFiles(ctx context.Context) error {
	unlock, err := acquireActionLock(e.paths.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	release, err := acquireServiceLock(ctx, e.paths.ServiceLock)
	if err != nil {
		return err
	}
	defer release()
	current, err := e.recognizedRuntime()
	if err != nil {
		return err
	}
	if !current {
		return errors.New("setup must finish first")
	}
	if err := e.recoverAddon(); err != nil {
		return err
	}
	return e.recoverUpdate(ctx, false)
}
func (e *Engine) InstallAddon(ctx context.Context) error {
	unlock, err := acquireActionLock(e.paths.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	if err := e.installAllowed(); err != nil {
		return err
	}
	lock, err := acquireServiceLock(ctx, e.paths.ServiceLock)
	if err != nil {
		return err
	}
	defer lock()
	current, err := e.recognizedRuntime()
	if err != nil {
		return err
	}
	if !current {
		return errors.New("setup must finish first")
	}
	if err := e.recoverAddon(); err != nil {
		return err
	}
	if err := ensureDirectory(e.updateRoot(), 0700); err != nil {
		return err
	}
	if data, err := os.ReadFile(filepath.Join(e.updateRoot(), "restart-required")); err == nil && strings.TrimSpace(string(data)) != BuildVersion {
		return errors.New("Restart the Kobo to finish the installed addon update first.")
	}
	source := e.addon
	if source == nil {
		return errors.New("addon update source is unavailable")
	}
	m, err := source.manifest(ctx)
	if err != nil {
		return err
	}
	if !newerVersion(m.Version, BuildVersion) || newerVersion(m.MinHelper, BuildVersion) {
		return errors.New("addon release is not compatible or newer")
	}
	library := e.addonPath("usr/local/Kobo/imageformats/libtailscaleui.so")
	if _, err := os.Lstat(library); os.IsNotExist(err) {
		return errors.New("Wait for toolbar startup protection to finish, then try again.")
	} else if err != nil {
		return err
	}
	if err := e.requireSpace(m.Size + 2*maxAddonExpanded); err != nil {
		return err
	}
	e.reportUpdate("Downloading and verifying the addon…")
	defer os.Remove(filepath.Join(e.updateRoot(), "progress.json"))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		return err
	}
	response, err := source.upstream.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("addon download returned HTTP %d", response.StatusCode)
	}
	cache, err := os.CreateTemp(e.updateRoot(), "addon-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(cache.Name())
	defer cache.Close()
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(cache, hash), io.LimitReader(response.Body, m.Size+1))
	if err := errors.Join(copyErr, cache.Sync(), cache.Close()); err != nil {
		return err
	}
	if n != m.Size || hex.EncodeToString(hash.Sum(nil)) != m.SHA256 {
		return errors.New("addon download size or SHA256 mismatch")
	}
	if err := e.installAllowed(); err != nil {
		return err
	}
	e.reportUpdate("Installing addon files; keep the Kobo powered on…")
	return e.applyAddon(cache.Name(), m)
}
