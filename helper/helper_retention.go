package helper

import (
	"os"
	"path/filepath"
)

// previous comes only from a recognized setup's owned core-helper snapshot.
// Retire that release after promotion, so successive addon updates do not keep
// accumulating large Go helpers on the small system partition. Unknown files
// or paths are retained. The current core and USB wrapper must both select the
// replacement before its predecessor can be removed.
func (e *Engine) retirePreviousHelper(previous string) {
	if previous == "" || previous == e.paths.CandidateHelper {
		return
	}
	current, err := os.Readlink(e.paths.CoreLink)
	if err != nil || current != e.paths.CandidateHelper {
		return
	}
	if e.paths.USBScript != "" {
		var guard usbGuardRecord
		if readJSON(e.usbGuardPath(), &guard) != nil || guard.Helper != current || e.validateUSBGuard() != nil {
			return
		}
	}
	base := filepath.Join(filepath.Dir(e.paths.CoreLink), "releases")
	version := filepath.Base(filepath.Dir(previous))
	if _, ok := parseVersion(version); !ok || previous != filepath.Join(base, version, "helper") {
		return
	}
	directory := filepath.Dir(previous)
	if ensureDirectory(directory, 0755) != nil {
		return
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		return
	}
	for _, entry := range entries {
		if entry.Name() != "helper" && entry.Name() != "THIRD_PARTY_NOTICES.txt" {
			return
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return
		}
	}
	// A failed cleanup is harmless: the selected core and recovery records are
	// already committed, and a future complete installer can reclaim the space.
	_ = os.RemoveAll(directory)
	_ = syncDirectory(base)
}
