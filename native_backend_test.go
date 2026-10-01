//go:build native && cgo && (linux || android) && (amd64 || arm64)

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeBackendEmbeddedDecodeAndRender(t *testing.T) {
	if !nativeBackendAvailable() {
		t.Fatal("native backend is unavailable in a native build")
	}
	t.Setenv("TERM", "xterm-256color")

	dir := t.TempDir()
	path := filepath.Join(dir, "frame.ppm")
	ppm := append([]byte("P6\n2 2\n255\n"),
		255, 0, 0,
		0, 255, 0,
		0, 0, 255,
		255, 255, 255,
	)
	if err := os.WriteFile(path, ppm, 0600); err != nil {
		t.Fatalf("write PPM fixture: %v", err)
	}

	decoder, err := openNativeDecoder(path, 0, 0)
	if err != nil {
		t.Fatalf("open embedded decoder: %v", err)
	}
	defer decoder.close()

	pixels, frameWidth, frameHeight, stride, _, err := decoder.next()
	if err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if len(pixels) == 0 || frameWidth != 2 || frameHeight != 2 || stride < 6 {
		t.Fatalf("unexpected decoded frame: len=%d width=%d height=%d stride=%d", len(pixels), frameWidth, frameHeight, stride)
	}

	oldWidth, oldHeight := width, height
	oldSymbols, oldColors, oldDither := symbols, colors, dither
	width, height = 2, 2
	symbols, colors, dither = "block", "256", "none"
	defer func() {
		width, height = oldWidth, oldHeight
		symbols, colors, dither = oldSymbols, oldColors, oldDither
	}()

	renderer, err := openNativeRenderer()
	if err != nil {
		t.Fatalf("open embedded renderer: %v", err)
	}
	defer renderer.close()

	output, err := renderer.render(pixels, frameWidth, frameHeight, stride)
	if err != nil {
		t.Fatalf("render frame: %v", err)
	}
	if len(bytes.TrimSpace(output)) == 0 {
		t.Fatal("embedded renderer returned empty output")
	}
	if !bytes.Contains(output, []byte("\x1b[")) {
		t.Fatalf("embedded renderer emitted no escape sequences; the terminal description is empty (got %q)", output)
	}
}
