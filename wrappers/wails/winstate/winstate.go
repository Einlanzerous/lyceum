// Package winstate persists the desktop window's geometry between runs
// (LYCM-305). It deliberately imports nothing from Wails so it unit-tests on
// any host OS — the wrapper's package main only builds for Windows targets.
package winstate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// State is the persisted window geometry. PosValid reports whether X/Y came
// from a sane saved position and is derived on load, never stored.
type State struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Maximised bool `json:"maximised"`

	PosValid bool `json:"-"`
}

// Sizes below the app's minimum window size (main.go) or beyond any plausible
// desktop mean a corrupt file and discard the whole state; positions outside
// the virtual-screen range invalidate only the position (Windows parks
// minimised windows at -32000,-32000, which must never be restored).
const (
	minWidth, minHeight = 420, 560
	maxDim              = 16384
	posLimit            = 20000
)

// DefaultPath returns the state file under the OS per-user config dir
// (%AppData%\Lyceum on Windows). Empty string means "don't persist".
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Lyceum", "window-state.json")
}

// Load reads path and returns the sanitised state; ok is false when the file
// is missing, unreadable, or fails the size sanity check.
func Load(path string) (State, bool) {
	if path == "" {
		return State{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return State{}, false
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, false
	}
	if st.Width < minWidth || st.Height < minHeight || st.Width > maxDim || st.Height > maxDim {
		return State{}, false
	}
	st.PosValid = st.X > -posLimit && st.X < posLimit && st.Y > -posLimit && st.Y < posLimit
	return st, true
}

// Save writes st to path, creating the parent dir. A failed save only costs
// the next run its geometry, so the caller just logs the error.
func Save(path string, st State) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
