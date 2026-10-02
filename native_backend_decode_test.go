//go:build native && cgo && (linux || android) && (amd64 || arm64)

package main

import (
	"testing"
)

// The two fixtures are tiny (160x120, 24 fps, 48 frames, 2 s) and committed on
// purpose: the release job runs `go test -tags native ./...` in a container that
// has the pinned libraries but no ffmpeg CLI, so a fixture has to be a file.
//
//   testdata/tiny-24fps.mp4  start_time 0, keyframes at frames 0 and 24
//   testdata/offset-start.ts the same frames in MPEG-TS, start_time 1.4 s
const (
	gridFixture = "testdata/tiny-24fps.mp4"
	tsFixture   = "testdata/offset-start.ts"
)

// decodeToEnd drains the decoder and returns how many frames it produced.
func decodeToEnd(t *testing.T, path string, targetFPS int) int {
	t.Helper()
	decoder, err := openNativeDecoder(path, targetFPS, 0)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer decoder.close()

	count := 0
	for {
		pixels, _, _, _, _, err := decoder.next()
		if err != nil {
			t.Fatalf("decode %s after %d frames: %v", path, count, err)
		}
		if pixels == nil {
			return count
		}
		count++
	}
}

// decodeToEndFromSeek is decodeToEnd with a seek first; target_fps is 0 so the
// output grid is out of the way and only the seek itself is under test.
func decodeToEndFromSeek(t *testing.T, path string, seconds float64) int {
	t.Helper()
	decoder, err := openNativeDecoder(path, 0, 0)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer decoder.close()
	if err := decoder.seek(seconds); err != nil {
		t.Fatalf("seek %s to %.2fs: %v", path, seconds, err)
	}

	count := 0
	for {
		pixels, _, _, _, _, err := decoder.next()
		if err != nil {
			t.Fatalf("decode %s after seek: %v", path, err)
		}
		if pixels == nil {
			return count
		}
		count++
	}
}

// The output grid keeps the first frame falling into each slot of
// floor(pts*fps + 0.5), so these counts follow from that definition rather than
// from whatever the decoder happens to emit: 48 source frames at 24 fps give
// slots 0..47 at -fps 24 (48 frames) and slots 0..29 at -fps 15 (30 frames).
func TestNativeDecoderOutputGrid(t *testing.T) {
	if got := decodeToEnd(t, gridFixture, 24); got != 48 {
		t.Errorf("24 fps source at -fps 24: got %d frames, want 48", got)
	}
	if got := decodeToEnd(t, gridFixture, 15); got != 30 {
		t.Errorf("24 fps source at -fps 15: got %d frames, want 30", got)
	}

	// Same frames, same rate, but this container's timeline starts at 1.4 s. The
	// grid is anchored at the first frame, so the container's start offset must
	// not change how many frames come out.
	mp4 := decodeToEnd(t, gridFixture, 15)
	if got := decodeToEnd(t, tsFixture, 15); got != mp4 {
		t.Errorf("offset-start container at -fps 15: got %d frames, want %d (same as the mp4)", got, mp4)
	}
}

// The fixture holds keyframes at 0 s and 1 s, so seeking to 1.0 s has to land on
// the second one and leave exactly half the frames to decode.
//
// The MPEG-TS fixture (testdata/offset-start.ts, stream start_time 1.48 s) is
// not asserted here because its demuxer has no timestamp-to-byte-position index,
// so avformat_seek_file may land on a coarse keyframe rather than the requested
// relative time. Adding stream->start_time was measured to move the target even
// farther forward and can reach EOF. Track the container-specific seek behavior
// separately in issue #7 instead of freezing a coarse landing point as expected.
func TestNativeDecoderSeekLandsOnTheTargetKeyframe(t *testing.T) {
	if total := decodeToEnd(t, gridFixture, 0); total != 48 {
		t.Fatalf("fixture holds %d frames, want 48", total)
	}
	if got := decodeToEndFromSeek(t, gridFixture, 1.0); got != 24 {
		t.Errorf("frames decoded after seeking to 1.0s of a 2s stream: got %d, want 24 (second keyframe to EOF)", got)
	}
}

// The C renderer receives the buffer by pointer, so an empty frame would panic
// index 0 inside a render worker - which takes the whole process down instead of
// dropping one frame.
func TestNativeRendererRejectsEmptyFrame(t *testing.T) {
	if !nativeBackendAvailable() {
		t.Fatal("native backend is unavailable in a native build")
	}
	t.Setenv("TERM", "xterm-256color")

	oldWidth, oldHeight := width, height
	width, height = 4, 4
	defer func() {
		width, height = oldWidth, oldHeight
	}()

	renderer, err := openNativeRenderer()
	if err != nil {
		t.Fatalf("open embedded renderer: %v", err)
	}
	defer renderer.close()

	if _, err := renderer.render(nil, 0, 0, 0); err == nil {
		t.Fatal("renderer accepted an empty frame; the buffer is passed to C as a pointer")
	}
}
