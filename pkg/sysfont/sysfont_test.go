package sysfont

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Fonts are found by file name, ignoring case, in subfolders too, and the
// first of several names that is installed wins.
func TestFindAndList(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows Fonts folder isn't redirectable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(home, "none"))
	// the folder the platform searches first, so the walk sees these files,
	// and a folder every install has, so Dirs can be checked against it
	first, sys := filepath.Join(home, "data", "fonts"), "/usr/share/fonts"
	sub := filepath.Join(first, "truetype", "acme")
	if runtime.GOOS == "darwin" {
		first, sys = filepath.Join(home, "Library", "Fonts"), "/System/Library/Fonts"
		sub = filepath.Join(first, "acme")
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"AcmeSans-Bold.TTF", "AcmeMono.otf", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(sub, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	index() // afresh, with these folders
	if p, ok := Find("missing.ttf", "acmesans-bold.ttf"); !ok || !strings.HasSuffix(p, "AcmeSans-Bold.TTF") {
		t.Errorf("Find: %q %v", p, ok)
	}
	if _, ok := Find("notes.txt"); ok {
		t.Error("a non-font file was indexed")
	}
	l := List()
	if len(l) < 2 || !contains(l, "AcmeMono.otf") || contains(l, "notes.txt") {
		t.Errorf("List: %v", l)
	}
	if d := Dirs(); d[0] != first || !contains(d, sys) {
		t.Errorf("Dirs: %v", d)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
