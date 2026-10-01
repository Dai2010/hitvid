package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestInputEventsFromReader(t *testing.T) {
	got := inputEventsFromReader(bytes.NewBufferString("q +-\x1b[A\x1b[B\x1b[C\x1b[D"))
	want := []inputEvent{inputQuit, inputPause, inputSpeedUp, inputSpeedDown, inputPrev, inputNext, inputSeekForward, inputSeekBackward}
	for _, expected := range want {
		select {
		case event, ok := <-got:
			if !ok {
				t.Fatalf("input events closed before %v", expected)
			}
			if event != expected {
				t.Errorf("got event %v, want %v", event, expected)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for input event")
		}
	}
	if _, ok := <-got; ok {
		t.Fatal("expected event channel to close at end of input")
	}
}

func TestStreamJPEGFrames(t *testing.T) {
	input := append([]byte("noise"), 0xff, 0xd8, 0x01, 0x02, 0xff, 0xd9)
	input = append(input, 0xff, 0xd8, 0x03, 0x04, 0xff, 0xd9)
	var frames [][]byte
	err := streamJPEGFrames(context.Background(), bytes.NewReader(input), func(frame []byte) error {
		frames = append(frames, frame)
		return nil
	})
	if err != nil {
		t.Fatalf("streamJPEGFrames returned error: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if !bytes.Equal(frames[0], []byte{0xff, 0xd8, 0x01, 0x02, 0xff, 0xd9}) {
		t.Fatalf("first frame mismatch: %x", frames[0])
	}
}

func TestStreamJPEGFramesRejectsIncompleteFrame(t *testing.T) {
	err := streamJPEGFrames(context.Background(), bytes.NewReader([]byte{0xff, 0xd8, 0x01}), func([]byte) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected incomplete frame error")
	}
}

func TestStreamJPEGFramesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := streamJPEGFrames(ctx, bytes.NewReader(nil), func([]byte) error {
		return errors.New("callback must not run")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestRenderedFrameStore(t *testing.T) {
	store := newRenderedFrameStore()
	store.put(3, []byte("frame"))
	content, ok := store.take(3)
	if !ok || string(content) != "frame" {
		t.Fatalf("take returned (%q, %v)", content, ok)
	}
	if _, ok := store.take(3); ok {
		t.Fatal("frame remained in store after take")
	}
}

func TestSeekTargetFrame(t *testing.T) {
	tests := []struct {
		name    string
		current int
		delta   int
		total   int
		want    int
	}{
		{name: "forward from current position", current: 200, delta: 75, total: 500, want: 275},
		{name: "backward from current position", current: 200, delta: -75, total: 500, want: 125},
		{name: "clamp to beginning", current: 20, delta: -75, total: 500, want: 0},
		{name: "clamp to final frame", current: 450, delta: 75, total: 500, want: 499},
		{name: "unknown duration", current: 200, delta: 75, total: 0, want: 275},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := seekTargetFrame(test.current, test.delta, test.total); got != test.want {
				t.Errorf("seekTargetFrame(%d, %d, %d) = %d, want %d", test.current, test.delta, test.total, got, test.want)
			}
		})
	}
}

func TestValidateNumThreads(t *testing.T) {
	if err := validateNumThreads(1); err != nil {
		t.Fatalf("validateNumThreads(1) returned %v", err)
	}
	for _, threads := range []int{0, -1} {
		if err := validateNumThreads(threads); err == nil {
			t.Errorf("validateNumThreads(%d) succeeded, want an error", threads)
		}
	}
}

func TestRenderInfoLineFitsTerminalWidth(t *testing.T) {
	statuses := []string{"PLAYING", "PAUSED", "BUFFERING", "FINISHED"}
	for _, width := range []int{1, 2, 20, 40, 80, 89, 90, 200} {
		for _, status := range statuses {
			got := renderInfoLine(status, 120, 15, 1.0, 900, width)
			if runes := len([]rune(got)); runes > width {
				t.Errorf("renderInfoLine(%q, width=%d) produced %d columns: %q", status, width, runes, got)
			}
		}
	}
}

func TestRenderInfoLineUnchangedWhenItFits(t *testing.T) {
	got := renderInfoLine("PLAYING", 0, 15, 1.0, 30, 200)
	want := "[PLAYING] 00:00 / 00:02 | Speed: 1.00x | " + controlsHint
	if got != want {
		t.Errorf("renderInfoLine returned %q, want %q", got, want)
	}
}

func TestRenderInfoLineKeepsStatusOnNarrowTerminal(t *testing.T) {
	got := renderInfoLine("PLAYING", 0, 15, 1.0, 30, 20)
	if runes := len([]rune(got)); runes != 20 {
		t.Fatalf("renderInfoLine produced %d columns, want 20: %q", runes, got)
	}
	if !strings.HasPrefix(got, "[PLAYING] 00:00") {
		t.Errorf("renderInfoLine dropped the playback state prefix: %q", got)
	}
}

func TestValidateScaleMode(t *testing.T) {
	for _, mode := range []string{scaleFit, scaleFill, scaleStretch} {
		if err := validateScaleMode(mode); err != nil {
			t.Errorf("validateScaleMode(%q) returned %v", mode, err)
		}
	}
	for _, mode := range []string{"", "FIT", "cover", "zoom"} {
		if err := validateScaleMode(mode); err == nil {
			t.Errorf("validateScaleMode(%q) succeeded, want an error", mode)
		}
	}
}

func TestValidateDimensions(t *testing.T) {
	for _, dims := range [][2]int{{0, 0}, {1, 1}, {40, 11}, {200, 60}} {
		if err := validateDimensions(dims[0], dims[1]); err != nil {
			t.Errorf("validateDimensions(%d, %d) returned %v", dims[0], dims[1], err)
		}
	}
	for _, dims := range [][2]int{{-1, 11}, {40, -1}, {-40, -11}} {
		if err := validateDimensions(dims[0], dims[1]); err == nil {
			t.Errorf("validateDimensions(%d, %d) succeeded, want an error", dims[0], dims[1])
		}
	}
}

func TestValidateNativeScaleMode(t *testing.T) {
	for _, mode := range []string{scaleFit, scaleFill, scaleStretch} {
		if err := validateNativeScaleMode(mode, false); err != nil {
			t.Errorf("compatibility backend rejected %q: %v", mode, err)
		}
	}
	if err := validateNativeScaleMode(scaleFit, true); err != nil {
		t.Errorf("native backend rejected fit: %v", err)
	}
	for _, mode := range []string{scaleFill, scaleStretch} {
		if err := validateNativeScaleMode(mode, true); err == nil {
			t.Errorf("native backend accepted unsupported mode %q", mode)
		}
	}
}

func TestBuildVideoFilter(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{
			name: "fit keeps the source aspect inside the pixel box",
			mode: scaleFit,
			want: "fps=15,scale=240:132:force_original_aspect_ratio=decrease",
		},
		{
			name: "fill covers the box and crops the overflow",
			mode: scaleFill,
			want: "fps=15,scale=240:132:force_original_aspect_ratio=increase,crop=240:132",
		},
		{
			name: "stretch distorts the source to the box",
			mode: scaleStretch,
			want: "fps=15,scale=240:132",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := buildVideoFilter(15, 40, 11, test.mode); got != test.want {
				t.Errorf("buildVideoFilter(15, 40, 11, %q) = %q, want %q", test.mode, got, test.want)
			}
		})
	}
}
