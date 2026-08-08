package main

import (
	"path/filepath"
	"testing"
)

// TestDataDirPath locks the contract every write path depends on: one data
// directory for the database, backups and logs. When these disagree, a restore
// writes to a database nobody is reading and the data loss is silent.
func TestDataDirPath(t *testing.T) {
	t.Run("PAAP_DATA wins", func(t *testing.T) {
		t.Setenv("PAAP_DATA", "/tmp/paap-custom")
		if got := dataDirPath(); got != "/tmp/paap-custom" {
			t.Errorf("dataDirPath() = %q, want /tmp/paap-custom", got)
		}
	})

	t.Run("falls back to ~/.paap", func(t *testing.T) {
		t.Setenv("PAAP_DATA", "")
		t.Setenv("HOME", "/tmp/fake-home")
		want := filepath.Join("/tmp/fake-home", ".paap")
		if got := dataDirPath(); got != want {
			t.Errorf("dataDirPath() = %q, want %q", got, want)
		}
	})
}
