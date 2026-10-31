package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type checkRecord struct {
	Checked int64  `json:"checked"`
	Next    int64  `json:"next"`
	Latest  string `json:"latest"`
	Error   string `json:"error,omitempty"`
}
type UpdateStatus struct {
	Automatic      bool   `json:"automatic"`
	Current        string `json:"current"`
	Latest         string `json:"latest"`
	Checked        int64  `json:"checked"`
	Error          string `json:"error,omitempty"`
	Available      bool   `json:"available"`
	Pending        bool   `json:"pending"`
	AddonCurrent   string `json:"addon_current"`
	AddonLatest    string `json:"addon_latest"`
	AddonAvailable bool   `json:"addon_available"`
	AddonError     string `json:"addon_error,omitempty"`
	Restart        bool   `json:"restart"`
	CanInstall     bool   `json:"can_install"`
	Reason         string `json:"reason,omitempty"`
}

func (e *Engine) installAllowed() error {
	if e.eligibility != nil {
		return e.eligibility()
	}
	return nil
}
func wifiPresent() bool {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) > 3 && strings.HasPrefix(f[0], "wlan") && f[1] == "00000000" {
			flags, err := strconv.ParseUint(f[3], 16, 32)
			if err == nil && flags&1 != 0 {
				return true
			}
		}
	}
	return false
}
func deviceInstallAllowed() error {
	if !wifiPresent() {
		return errors.New("Connect Wi-Fi to install updates.")
	}
	dirs, _ := filepath.Glob("/sys/class/power_supply/*")
	for _, dir := range dirs {
		kind, _ := os.ReadFile(filepath.Join(dir, "type"))
		if strings.TrimSpace(string(kind)) != "Battery" {
			continue
		}
		status, _ := os.ReadFile(filepath.Join(dir, "status"))
		if strings.TrimSpace(string(status)) == "Charging" || strings.TrimSpace(string(status)) == "Full" {
			return nil
		}
		capacity, err := os.ReadFile(filepath.Join(dir, "capacity"))
		if err != nil {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(string(capacity)))
		if err == nil && v >= 20 && v <= 100 {
			return nil
		}
	}
	return errors.New("Charge the Kobo to at least 20% or connect power before installing.")
}
func (e *Engine) Status(ctx context.Context) (UpdateStatus, error) {
	s := UpdateStatus{AddonCurrent: BuildVersion}
	enabled, err := e.checksEnabled()
	if err != nil {
		return s, err
	}
	s.Automatic = enabled
	s.CanInstall = true
	if err := e.installAllowed(); err != nil {
		s.CanInstall = false
		s.Reason = err.Error()
	}
	var r checkRecord
	if err := readJSON(filepath.Join(e.updateRoot(), "check.json"), &r); err != nil && !os.IsNotExist(err) {
		return s, err
	}
	var ar checkRecord
	if err := readJSON(filepath.Join(e.updateRoot(), "addon-check.json"), &ar); err != nil && !os.IsNotExist(err) {
		return s, err
	}
	s.AddonLatest = ar.Latest
	s.AddonError = ar.Error
	s.AddonAvailable = ar.Error == "" && newerVersion(ar.Latest, BuildVersion)
	if data, err := os.ReadFile(filepath.Join(e.updateRoot(), "restart-required")); err == nil {
		s.Restart = strings.TrimSpace(string(data)) != BuildVersion
		if s.Restart {
			s.AddonAvailable = false
		}
	}
	s.Latest = r.Latest
	s.Checked = r.Checked
	s.Error = r.Error
	v, err := e.pairVersion(ctx, filepath.Join(e.paths.Runtime, "bin"))
	if err != nil {
		return s, err
	}
	s.Current = v
	s.Available = stableVersion(r.Latest) && newerVersion(r.Latest, v)
	var j updateJournal
	if err := readJSON(e.updateJournalPath(), &j); err == nil {
		s.Pending = true
		s.CanInstall = false
		s.Reason = "Restart Tailscale to finish validating the previous update."
	} else if !os.IsNotExist(err) {
		return s, err
	}
	return s, nil
}
func (e *Engine) Check(ctx context.Context, auto bool) error {
	if auto {
		enabled, err := e.checksEnabled()
		if err != nil {
			return err
		}
		if !enabled {
			return nil
		}
		if _, err := os.Lstat(e.paths.Lock); err == nil {
			return nil
		}
		if e.eligibility != nil && !wifiPresent() {
			return nil
		}
	}
	if err := ensureDirectory(e.updateRoot(), 0700); err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	unlock, err := acquireServiceLock(cctx, e.paths.ServiceLock+".check")
	if err != nil {
		return errors.New("an update check is already running")
	}
	defer unlock()
	var r checkRecord
	err = readJSON(filepath.Join(e.updateRoot(), "check.json"), &r)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	now := time.Now().Unix()
	if auto && now < r.Next && r.Checked <= now {
		return nil
	}
	r.Checked = now
	r.Next = now + int64((30*time.Minute)/time.Second)
	r.Error = "Update check was interrupted. Try Check now."
	if err := writeJSON(filepath.Join(e.updateRoot(), "check.json"), r); err != nil {
		return err
	}
	v, fetchErr := e.upstream.Latest(ctx)
	if fetchErr != nil {
		r.Error = "Could not check upstream releases. Try again when Wi-Fi is available."
	} else {
		r.Latest = v
		r.Error = ""
		r.Next = now + int64((6*time.Hour)/time.Second)
	}
	if err := writeJSON(filepath.Join(e.updateRoot(), "check.json"), r); err != nil {
		return err
	}
	if e.addon != nil {
		ar := checkRecord{Checked: now}
		m, err := e.addon.manifest(ctx)
		if err != nil {
			ar.Error = "Could not check addon releases. Try Check now later."
			r.Next = now + 1800
			if err := writeJSON(filepath.Join(e.updateRoot(), "check.json"), r); err != nil {
				return err
			}
		} else {
			ar.Latest = m.Version
			if newerVersion(m.MinHelper, BuildVersion) {
				ar.Error = "This release needs a newer USB installer."
			}
		}
		if err := writeJSON(filepath.Join(e.updateRoot(), "addon-check.json"), ar); err != nil {
			return err
		}
	}
	return fetchErr
}

type updatePreferences struct {
	Schema    int  `json:"schema"`
	Automatic bool `json:"automatic"`
}

func (e *Engine) checksEnabled() (bool, error) {
	var p updatePreferences
	if err := readJSON(filepath.Join(e.updateRoot(), "preferences.json"), &p); os.IsNotExist(err) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	if p.Schema != 1 {
		return false, errors.New("invalid update preferences")
	}
	return p.Automatic, nil
}
func (e *Engine) SetChecks(enabled bool) error {
	if err := ensureDirectory(e.updateRoot(), 0700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(e.updateRoot(), "preferences.json"), updatePreferences{Schema: 1, Automatic: enabled})
}
func (e *Engine) reportUpdate(message string) {
	_ = writeJSON(filepath.Join(e.updateRoot(), "progress.json"), map[string]string{"progress": message})
}
func (e *Engine) Progress() (map[string]string, error) {
	var p map[string]string
	err := readJSON(filepath.Join(e.updateRoot(), "progress.json"), &p)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	return p, err
}
