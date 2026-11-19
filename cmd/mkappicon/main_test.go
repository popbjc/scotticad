package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteIconset(t *testing.T) {
	dir := t.TempDir()
	if err := writeIconset(dir); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"icon_16x16.png":      16,
		"icon_16x16@2x.png":   32,
		"icon_32x32.png":      32,
		"icon_32x32@2x.png":   64,
		"icon_128x128.png":    128,
		"icon_128x128@2x.png": 256,
		"icon_256x256.png":    256,
		"icon_256x256@2x.png": 512,
		"icon_512x512.png":    512,
		"icon_512x512@2x.png": 1024,
	}
	for name, size := range want {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := png.DecodeConfig(f)
		closeErr := f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if cfg.Width != size || cfg.Height != size {
			t.Errorf("%s: got %dx%d, want %dx%d", name, cfg.Width, cfg.Height, size, size)
		}
	}
}
