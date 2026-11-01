package helper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUSBGuardPreservesOriginalAndRepairsInterruptedReplacement(t *testing.T) {
	e, _ := ramFixture(t)
	e.paths.USBScript = filepath.Join(e.paths.Runtime, "vendor-entry")
	e.paths.CandidateHelper = filepath.Join(e.paths.Runtime, "helper")
	original := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(e.paths.USBScript, original, 0755); err != nil {
		t.Fatal(err)
	}
	expected := digest(original)
	if err := e.installUSBGuardForHash(expected); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(e.usbBackupPath())
	if err != nil || string(backup) != string(original) {
		t.Fatal("vendor original was not preserved", err)
	}
	if err := e.validateUSBGuardForHash(expected); err != nil {
		t.Fatal(err)
	}
	var record usbGuardRecord
	if err := readJSON(e.usbGuardPath(), &record); err != nil {
		t.Fatal(err)
	}
	record.Previous = record.Current
	record.Current = "interrupted-before-new-wrapper"
	if err := writeJSON(e.usbGuardPath(), record); err != nil {
		t.Fatal(err)
	}
	if err := e.validateUSBGuardForHash(expected); err == nil {
		t.Fatal("incomplete wrapper transaction was treated as installed")
	}
	if err := e.installUSBGuardForHash(expected); err != nil {
		t.Fatal("interrupted wrapper could not be repaired", err)
	}
	if err := e.validateUSBGuardForHash(expected); err != nil {
		t.Fatal(err)
	}
	custom := []byte("#!/bin/sh\n# user customization\nexit 0\n")
	if err := os.WriteFile(e.paths.USBScript, custom, 0755); err != nil {
		t.Fatal(err)
	}
	if err := e.installUSBGuardForHash(expected); err == nil {
		t.Fatal("customized vendor entry was overwritten")
	}
	actual, err := os.ReadFile(e.paths.USBScript)
	if err != nil || string(actual) != string(custom) {
		t.Fatal("customized entry changed", err)
	}
}
