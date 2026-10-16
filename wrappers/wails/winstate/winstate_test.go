package winstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "window-state.json")
	want := State{Width: 1280, Height: 900, X: 40, Y: 60, Maximised: true}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok := Load(path)
	if !ok {
		t.Fatal("Load: ok = false for freshly saved state")
	}
	if !got.PosValid {
		t.Error("Load: PosValid = false for an in-range position")
	}
	got.PosValid = false
	if got != want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestLoadMissingOrInvalid(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"empty path":   "",
		"missing file": filepath.Join(dir, "absent.json"),
		"corrupt json": corrupt,
	} {
		if _, ok := Load(path); ok {
			t.Errorf("%s: ok = true, want false", name)
		}
	}
}

func TestLoadRejectsBadSizes(t *testing.T) {
	for name, st := range map[string]State{
		"below minimum": {Width: 200, Height: 900},
		"absurdly big":  {Width: 1280, Height: 99999},
		"zero value":    {},
	} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := Save(path, st); err != nil {
			t.Fatal(err)
		}
		if _, ok := Load(path); ok {
			t.Errorf("%s: ok = true, want false", name)
		}
	}
}

func TestLoadInvalidatesMinimisedSentinelPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := Save(path, State{Width: 1100, Height: 760, X: -32000, Y: -32000}); err != nil {
		t.Fatal(err)
	}
	got, ok := Load(path)
	if !ok {
		t.Fatal("Load: ok = false, want true (size is sane)")
	}
	if got.PosValid {
		t.Error("PosValid = true for the -32000 minimised sentinel")
	}
}
