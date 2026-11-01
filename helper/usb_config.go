package helper

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Identical vendor script in the inspected 390/4.46.23852 and 376/4.38.23852 archives.
const vendorUSBHash = "ae1d700ee4fe886bfdbca7814b95b473e3b0b6c391c2146e49c337c43174e8a7"

type usbGuardRecord struct {
	Schema   int    `json:"schema"`
	Original string `json:"original"`
	Current  string `json:"current"`
	Previous string `json:"previous,omitempty"`
	Helper   string `json:"helper"`
}

func readRegular(path string, limit int64) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("unexpected file type or size")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file exceeds size limit")
	}
	return data, nil
}

func (e *Engine) usbGuardPath() string  { return filepath.Join(e.paths.Runtime, "usb-guard.json") }
func (e *Engine) usbBackupPath() string { return filepath.Join(e.paths.Runtime, "vendor-usb.sh") }

// Provision before any public I/O, including the first installation. The record
// is durable before replacement and permits repair after either side of a crash.
func (e *Engine) installUSBGuard() error {
	return e.installUSBGuardForHash(vendorUSBHash)
}

func (e *Engine) installUSBGuardForHash(expected string) error {
	if e.paths.USBScript == "" {
		return nil
	}
	if !filepath.IsAbs(e.paths.CandidateHelper) {
		return errors.New("USB guard requires an installed absolute helper path")
	}
	current, err := readRegular(e.paths.USBScript, 64<<10)
	if err != nil {
		return err
	}
	var record usbGuardRecord
	recordErr := readJSON(e.usbGuardPath(), &record)
	observed := digest(current)
	if recordErr != nil && !os.IsNotExist(recordErr) {
		return recordErr
	}
	if recordErr == nil && (record.Schema != 1 || record.Original != expected) {
		return errors.New("unknown USB ownership record was retained")
	}
	if observed != expected &&
		(recordErr != nil || (observed != record.Current && observed != record.Previous)) {
		return errors.New("custom USB script was retained; automatic storage coordination is unavailable")
	}
	backup, err := readRegular(e.usbBackupPath(), 64<<10)
	if os.IsNotExist(err) && observed == expected {
		backup = current
		if err := writeAtomic(e.usbBackupPath(), backup, 0600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if digest(backup) != expected {
		return errors.New("original USB script backup does not match supported firmware")
	}
	wrapper, err := usbWrapper(backup, expected, e.paths.CandidateHelper, e.usbBackupPath())
	if err != nil {
		return err
	}
	if observed == digest(wrapper) && recordErr == nil && record.Current == observed && record.Previous == "" {
		return nil
	}
	updated := usbGuardRecord{
		Schema: 1, Original: expected, Current: digest(wrapper),
		Previous: observed, Helper: e.paths.CandidateHelper,
	}
	if err := writeJSON(e.usbGuardPath(), updated); err != nil {
		return err
	}
	if err := writeAtomic(e.paths.USBScript, wrapper, 0755); err != nil {
		return err
	}
	updated.Previous = ""
	return writeJSON(e.usbGuardPath(), updated)
}

func (e *Engine) validateUSBGuard() error {
	return e.validateUSBGuardForHash(vendorUSBHash)
}

func (e *Engine) validateUSBGuardForHash(expected string) error {
	if e.paths.USBScript == "" {
		return nil
	}
	var record usbGuardRecord
	if err := readJSON(e.usbGuardPath(), &record); err != nil {
		return errors.New("setup must install USB storage coordination before public I/O")
	}
	current, err := readRegular(e.paths.USBScript, 64<<10)
	if err != nil {
		return err
	}
	if record.Schema != 1 || record.Original != expected || digest(current) != record.Current {
		return errors.New("USB storage coordination changed; retry setup before accessing books storage")
	}
	return nil
}
