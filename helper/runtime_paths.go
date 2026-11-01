package helper

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
)

type executablePin struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type releasePin struct {
	Version string                   `json:"version"`
	Files   map[string]executablePin `json:"files"`
}

type runtimeLayout struct {
	Schema  int        `json:"schema"`
	Current releasePin `json:"current"`
}

func (p Paths) ramMode() bool        { return p.Books != "" && p.RAM != "" }
func (p Paths) legacyBinDir() string { return filepath.Join(p.Runtime, "bin") }
func (p Paths) binaryDir() string {
	if p.ramMode() {
		return filepath.Join(p.RAM, "bin")
	}
	return p.legacyBinDir()
}
func (p Paths) layoutPath() string { return filepath.Join(p.Runtime, "layout.json") }
func (p Paths) usbBlockPath() string {
	if p.USBBlock != "" {
		return p.USBBlock
	}
	return filepath.Join(p.RAM, "usb-exporting")
}

func validatePin(pin releasePin) (int64, error) {
	if !stableVersion(pin.Version) || len(pin.Files) != 2 {
		return 0, errors.New("invalid protected release record")
	}
	var total int64
	for _, name := range []string{"tailscale", "tailscaled"} {
		file, ok := pin.Files[name]
		hash, err := hex.DecodeString(file.SHA256)
		if !ok || err != nil || len(hash) != 32 || file.Size <= 0 || file.Size > maxExpanded {
			return 0, errors.New("invalid protected executable record")
		}
		total += file.Size
	}
	if total > maxExpanded {
		return 0, errors.New("protected executable pair exceeds size limit")
	}
	return total, nil
}

func pinDigest(pin releasePin) string {
	data, _ := json.Marshal(pin)
	return digest(data)
}

func (e *Engine) readLayout() (runtimeLayout, error) {
	var layout runtimeLayout
	if err := readJSON(e.paths.layoutPath(), &layout); err != nil {
		return layout, err
	}
	if layout.Schema != 1 {
		return layout, errors.New("invalid runtime layout schema")
	}
	_, err := validatePin(layout.Current)
	return layout, err
}
