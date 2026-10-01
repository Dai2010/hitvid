// Copyright (C) 2025 Hitmux
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.

// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// --- Command-line Flags ---
var (
	videoPath  string
	fps        int
	symbols    string
	colors     string
	dither     string
	width      int
	height     int
	numThreads int
	scaleMode  string
	showHelp   bool
)

// --- Global Playback State & Config ---
var (
	// Playback state
	isPaused                    = false
	currentFrameIndex           = 0
	totalFrames                 = 0
	extractionComplete          = false
	renderingComplete           = false
	currentSpeedMultiplierIndex = 3 // Index for 1.0x speed

	// Playback configuration
	playbackSpeedMultipliers = []float64{0.25, 0.5, 0.75, 1.0, 1.25, 1.5, 2.0}
	seekAmountInFrames       = 0

	// Concurrency
	stateMutex     sync.Mutex
	frameReadyCond *sync.Cond
	userAction     = ""
)

const seekSeconds = 5

func seekTargetFrame(currentFrame, delta, totalFrames int) int {
	target := currentFrame + delta
	if target < 0 {
		return 0
	}
	if totalFrames > 0 && target >= totalFrames {
		return totalFrames - 1
	}
	return target
}

func validateNumThreads(threads int) error {
	if threads < 1 {
		return fmt.Errorf("-threads must be at least 1 (got %d)", threads)
	}
	return nil
}

func validateScaleMode(mode string) error {
	switch mode {
	case scaleFit, scaleFill, scaleStretch:
		return nil
	}
	return fmt.Errorf("-scale must be one of %s, %s or %s (got %q)", scaleFit, scaleFill, scaleStretch, mode)
}

// validateDimensions rejects negative -w/-h before the terminal defaults are
// applied. buildVideoFilter turns these into scale and crop sizes, where a
// negative value either fails the filter graph outright or, in ffmpeg's
// expression language, quietly produces something else.
func validateDimensions(width, height int) error {
	if width < 0 || height < 0 {
		return fmt.Errorf("-w and -h must not be negative (got %d and %d)", width, height)
	}
	return nil
}

// buildVideoFilter renders the FFmpeg -vf argument for one scaling mode. The
// pixel box is the character canvas converted to pixels, so the frame FFmpeg
// produces already has the shape Chafa is about to draw into: "fit" keeps the
// source aspect inside that box (letterboxed), "fill" grows the source to cover
// the box and crops the overflow, and "stretch" distorts the source to the box.
func buildVideoFilter(frameRate, width, height int, mode string) string {
	boxWidth := width * chafaSourceScale
	boxHeight := height * chafaSourceScale * cellAspectRatio
	var scaleFilter string
	switch mode {
	case scaleFill:
		scaleFilter = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d",
			boxWidth, boxHeight, boxWidth, boxHeight)
	case scaleStretch:
		scaleFilter = fmt.Sprintf("scale=%d:%d", boxWidth, boxHeight)
	default:
		scaleFilter = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", boxWidth, boxHeight)
	}
	return fmt.Sprintf("fps=%d,%s", frameRate, scaleFilter)
}

var supportedVideoExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".mov": true, ".avi": true, ".webm": true, ".flv": true,
}

const renderedFrameCapacity = 120

// ffmpegThreads caps FFmpeg's decoder threads. The pipeline only consumes fps
// frames per second and the decoder is back-pressured by frameSlots, so one
// decode thread keeps up; FFmpeg's frame threading multiplies the decoder's
// frame pool, which on HD video costs tens of MB of RAM for no added
// throughput.
const ffmpegThreads = 1

// chafaSourceScale is how many source pixels per terminal column FFmpeg hands
// to Chafa. A cell is only a few subpixels wide, so Chafa resamples every frame
// down to the canvas anyway; 6 keeps FFmpeg's scale and MJPEG-encode cost near
// its floor while still giving Chafa a few samples per cell to dither with.
const chafaSourceScale = 6

// Scaling modes accepted by the -scale flag.
const (
	scaleFit     = "fit"
	scaleFill    = "fill"
	scaleStretch = "stretch"
)

// cellAspectRatio is a terminal cell's height divided by its width. Cells are
// about twice as tall as they are wide, which is also the ratio Chafa assumes
// by default, so the pixel box handed to FFmpeg has the same shape as the
// character canvas.
const cellAspectRatio = 2

type renderJob struct {
	index int
	jpeg  []byte
}

type inputEvent byte

const (
	inputQuit inputEvent = iota
	inputPause
	inputSpeedUp
	inputSpeedDown
	inputPrev
	inputNext
	inputSeekForward
	inputSeekBackward
)

// readInputEvents is the process-wide stdin reader. Playback sessions only
// consume decoded events from its channel, so seeking never starts another
// goroutine blocked in os.Stdin.Read.
func readInputEvents() <-chan inputEvent {
	return inputEventsFromReader(os.Stdin)
}

func inputEventsFromReader(r io.Reader) <-chan inputEvent {
	events := make(chan inputEvent, 16)
	go func() {
		defer close(events)
		reader := bufio.NewReader(r)
		for {
			first, err := reader.ReadByte()
			if err != nil {
				return
			}
			var event inputEvent
			switch {
			case first == 'q' || first == 3:
				event = inputQuit
			case first == ' ':
				event = inputPause
			case first == '+':
				event = inputSpeedUp
			case first == '-':
				event = inputSpeedDown
			case first == '\x1b':
				second, secondErr := reader.ReadByte()
				third, thirdErr := reader.ReadByte()
				if secondErr != nil || thirdErr != nil || second != '[' {
					continue
				}
				switch third {
				case 'A':
					event = inputPrev
				case 'B':
					event = inputNext
				case 'C':
					event = inputSeekForward
				case 'D':
					event = inputSeekBackward
				default:
					continue
				}
			default:
				continue
			}
			events <- event
		}
	}()
	return events
}

// renderedFrameStore keeps only frames that are waiting for playback. The
// decoder is back-pressured by frameSlots, so this map cannot grow without
// bound during a long video.
type renderedFrameStore struct {
	frames map[int][]byte
}

func newRenderedFrameStore() *renderedFrameStore {
	return &renderedFrameStore{frames: make(map[int][]byte, renderedFrameCapacity)}
}

func (s *renderedFrameStore) put(index int, content []byte) {
	s.frames[index] = content
}

func (s *renderedFrameStore) take(index int) ([]byte, bool) {
	content, ok := s.frames[index]
	if ok {
		delete(s.frames, index)
	}
	return content, ok
}

// streamJPEGFrames splits FFmpeg's image2pipe output into complete JPEG
// images. JPEG byte stuffing keeps FFD9 reserved for the end marker.
func streamJPEGFrames(ctx context.Context, r io.Reader, emit func([]byte) error) error {
	const chunkSize = 64 * 1024
	const maxJPEGSize = 64 * 1024 * 1024
	buffer := make([]byte, 0, chunkSize*2)
	chunk := make([]byte, chunkSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.Read(chunk)
		if n > 0 {
			buffer = append(buffer, chunk[:n]...)
			for {
				start := bytes.Index(buffer, []byte{0xff, 0xd8})
				if start < 0 {
					if len(buffer) > 1 {
						buffer = buffer[len(buffer)-1:]
					}
					break
				}
				endRel := bytes.Index(buffer[start+2:], []byte{0xff, 0xd9})
				if endRel < 0 {
					if start > 0 {
						buffer = buffer[start:]
					}
					break
				}
				end := start + 2 + endRel + 2
				frame := append([]byte(nil), buffer[start:end]...)
				if err := emit(frame); err != nil {
					return err
				}
				buffer = buffer[end:]
			}
			if len(buffer) > maxJPEGSize {
				return fmt.Errorf("JPEG frame exceeds %d bytes", maxJPEGSize)
			}
		}
		if err != nil {
			if err == io.EOF {
				if len(bytes.TrimSpace(buffer)) != 0 {
					return fmt.Errorf("incomplete JPEG frame at end of FFmpeg stream")
				}
				return nil
			}
			return err
		}
	}
}

// getPlaylist scans a directory for video files and returns a sorted list.
func getPlaylist(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var playlist []string
	for _, entry := range entries {
		if !entry.IsDir() {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if supportedVideoExtensions[ext] {
				playlist = append(playlist, filepath.Join(dir, entry.Name()))
			}
		}
	}
	sort.Strings(playlist)
	return playlist, nil
}

// getVideoDuration uses ffprobe to get the video duration in seconds.
func getVideoDuration(ctx context.Context, videoPath string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", videoPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}
	durationStr := strings.TrimSpace(out.String())
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration from ffprobe: %w", err)
	}
	return duration, nil
}

// formatTime converts a frame index into a MM:SS time string.
func formatTime(frameIndex int, frameRate int) string {
	if frameRate <= 0 {
		return "00:00"
	}
	seconds := frameIndex / frameRate
	minutes := seconds / 60
	seconds = seconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

// handleInput applies events to one playback session. The process-wide reader
// remains alive when this session is cancelled.
func handleInput(ctx context.Context, events <-chan inputEvent, cancel context.CancelFunc) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				cancel()
				return
			}
			if ctx.Err() != nil {
				return
			}
			stateMutex.Lock()
			switch event {
			case inputQuit:
				userAction = "quit"
				cancel()
			case inputPause:
				isPaused = !isPaused
			case inputSpeedUp:
				if currentSpeedMultiplierIndex < len(playbackSpeedMultipliers)-1 {
					currentSpeedMultiplierIndex++
				}
			case inputSpeedDown:
				if currentSpeedMultiplierIndex > 0 {
					currentSpeedMultiplierIndex--
				}
			case inputPrev:
				userAction = "prev"
				cancel()
			case inputNext:
				userAction = "next"
				cancel()
			case inputSeekForward:
				userAction = "seek-forward"
				cancel()
			case inputSeekBackward:
				userAction = "seek-backward"
				cancel()
			}
			if userAction == "quit" || userAction == "next" || userAction == "prev" || strings.HasPrefix(userAction, "seek-") {
				frameReadyCond.Broadcast()
			}
			stateMutex.Unlock()
		}
	}
}

// playVideo handles one in-memory playback session. startFrame is used when a
// seek restarts FFmpeg at a new timestamp.
func playVideo(ctx context.Context, path string, startFrame int) string {
	if nativeBackendAvailable() {
		if scaleMode != scaleFit {
			log.Printf("Warning: the native backend does not implement -scale %s; using %s\r\n", scaleMode, scaleFit)
		}
		return playVideoNative(ctx, path, startFrame)
	}

	// --- Reset state for the new video ---
	stateMutex.Lock()
	isPaused = false
	currentFrameIndex = startFrame
	totalFrames = 0
	extractionComplete = false
	renderingComplete = false
	frameStore := newRenderedFrameStore()
	userAction = "" // Clear previous action
	stateMutex.Unlock()

	// --- Get video info ---
	videoDuration, err := getVideoDuration(ctx, path)
	if err != nil {
		log.Printf("Warning: Could not get video duration for %s: %v\r\n", path, err)
	} else {
		stateMutex.Lock()
		totalFrames = int(videoDuration * float64(fps))
		stateMutex.Unlock()
	}

	if totalFrames > 0 && startFrame >= totalFrames {
		startFrame = totalFrames - 1
		stateMutex.Lock()
		currentFrameIndex = startFrame
		stateMutex.Unlock()
	}

	// --- Setup in-memory rendering pipeline ---
	var wgRender sync.WaitGroup
	jobs := make(chan renderJob, numThreads*2)
	frameSlots := make(chan struct{}, renderedFrameCapacity)
	sessionCtx, sessionCancel := context.WithCancel(ctx)
	defer sessionCancel()
	for i := 0; i < numThreads; i++ {
		wgRender.Add(1)
		go func() {
			defer wgRender.Done()
			for job := range jobs {
				chafaArgs := []string{"--size", fmt.Sprintf("%dx%d", width, height), "--symbols", symbols, "--colors", colors, "--dither", dither, "-"}
				chafaCmd := exec.CommandContext(sessionCtx, "chafa", chafaArgs...)
				chafaCmd.Stdin = bytes.NewReader(job.jpeg)
				output, err := chafaCmd.Output()
				if err != nil {
					if sessionCtx.Err() == nil {
						log.Printf("chafa failed for frame %d: %v\r\n", job.index, err)
					}
					output = nil
				}
				if runtime.GOOS != "windows" && output != nil {
					output = bytes.ReplaceAll(output, []byte("\n"), []byte("\r\n"))
				}
				stateMutex.Lock()
				frameStore.put(job.index, output)
				frameReadyCond.Broadcast()
				stateMutex.Unlock()
			}
		}()
	}

	// --- Start FFmpeg and stream JPEG frames directly through stdout ---
	ffmpegVF := buildVideoFilter(fps, width, height, scaleMode)
	ffmpegArgs := []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-threads", strconv.Itoa(ffmpegThreads),
	}
	if startFrame > 0 {
		// -ss belongs before -i: FFmpeg then seeks at the demuxer. Placed after
		// -i it decodes and discards every frame from the start of the file
		// instead, which each seek pays for with no back-pressure to hold it
		// back.
		ffmpegArgs = append(ffmpegArgs, "-ss", fmt.Sprintf("%.3f", float64(startFrame)/float64(fps)))
	}
	ffmpegArgs = append(ffmpegArgs, "-i", path)
	ffmpegArgs = append(ffmpegArgs, "-vf", ffmpegVF, "-q:v", "2", "-f", "image2pipe", "-vcodec", "mjpeg", "pipe:1")
	ffmpegCmd := exec.CommandContext(sessionCtx, "ffmpeg", ffmpegArgs...)
	var ffmpegErr bytes.Buffer
	ffmpegCmd.Stderr = &ffmpegErr
	stdout, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to create ffmpeg output pipe: %v", err)
		return "finished"
	}
	if err := ffmpegCmd.Start(); err != nil {
		log.Printf("Failed to start ffmpeg: %v", err)
		return "finished"
	}

	decodeDone := make(chan struct{})
	go func() {
		defer close(decodeDone)
		defer close(jobs)
		nextFrame := startFrame
		streamErr := streamJPEGFrames(sessionCtx, stdout, func(jpeg []byte) error {
			select {
			case frameSlots <- struct{}{}:
			case <-sessionCtx.Done():
				return sessionCtx.Err()
			}
			job := renderJob{index: nextFrame, jpeg: jpeg}
			select {
			case jobs <- job:
				nextFrame++
				return nil
			case <-sessionCtx.Done():
				<-frameSlots
				return sessionCtx.Err()
			}
		})
		waitErr := ffmpegCmd.Wait()
		if streamErr != nil && sessionCtx.Err() == nil {
			log.Printf("FFmpeg frame stream failed: %v\r\n", streamErr)
		}
		if waitErr != nil && sessionCtx.Err() == nil && ffmpegErr.Len() > 0 {
			log.Printf("FFmpeg failed: %s\r\n", strings.TrimSpace(ffmpegErr.String()))
		}
		stateMutex.Lock()
		extractionComplete = true
		frameReadyCond.Broadcast()
		stateMutex.Unlock()
	}()
	renderDone := make(chan struct{})
	go func() {
		defer close(renderDone)
		<-decodeDone
		wgRender.Wait()
		stateMutex.Lock()
		renderingComplete = true
		frameReadyCond.Broadcast()
		stateMutex.Unlock()
	}()

	// --- Playback Loop ---
	playbackLoop(sessionCtx, frameStore, frameSlots)
	sessionCancel()

	<-decodeDone
	<-renderDone

	stateMutex.Lock()
	finalAction := userAction
	stateMutex.Unlock()

	if finalAction != "" {
		return finalAction
	}
	return "finished"
}

func playbackLoop(ctx context.Context, frameStore *renderedFrameStore, frameSlots chan struct{}) {
	for {
		stateMutex.Lock()
		// Check for exit conditions first
		if ctx.Err() != nil {
			stateMutex.Unlock()
			return
		}
		if extractionComplete && totalFrames > 0 && currentFrameIndex >= totalFrames {
			stateMutex.Unlock()
			return // Video finished naturally
		}

		// Wait for the current frame to be rendered.
		content, ready := frameStore.frames[currentFrameIndex]
		for !ready && !renderingComplete && ctx.Err() == nil {
			printInfoUnlocked("BUFFERING", currentFrameIndex, height, fps, -1, totalFrames)
			frameReadyCond.Wait()
			content, ready = frameStore.frames[currentFrameIndex]
		}

		if ctx.Err() != nil {
			stateMutex.Unlock()
			return
		}
		if !ready {
			stateMutex.Unlock()
			return
		}

		speed := playbackSpeedMultipliers[currentSpeedMultiplierIndex]
		if isPaused {
			printInfo("PAUSED", currentFrameIndex, height, fps, speed)
			stateMutex.Unlock()
			time.Sleep(100 * time.Millisecond)
			continue
		}

		frameStartTime := time.Now()
		frameIdx := currentFrameIndex
		content, _ = frameStore.take(frameIdx)
		currentFrameIndex++
		stateMutex.Unlock()
		<-frameSlots

		if content == nil {
			continue
		}

		fmt.Print("\x1b[H")
		fmt.Print(string(content))
		printInfo("PLAYING", frameIdx, height, fps, speed)

		frameDelay := time.Duration(float64(time.Second) / (float64(fps) * speed))
		elapsed := time.Since(frameStartTime)
		sleepDuration := frameDelay - elapsed
		if sleepDuration > 0 {
			time.Sleep(sleepDuration)
		}
	}
}

func printHelp() {
	fmt.Println(`
        hitvid v1.1.3 - High-performance terminal video player

        Description:
            hitvid is a tool that uses ffmpeg and chafa to render and play videos in your terminal.
        It supports playlists, rich playback controls, and highly customizable rendering options.

		Dependencies:
		    Compatibility build: ffmpeg and chafa must be in PATH.
		    Native build: run make native and build with -tags native.

        Usage:
            go run . [options] <video file path>

        Example:
            go run . -fps 24 -colors full ./my_video.mp4

        Command-line options:
            -video <path>
               Path to the video file. Can also be given as the last positional argument.
            -fps <integer>
               Frame rate (FPS) for video extraction and playback. (Default: 15)
            -symbols <string>
               Symbol set to use for chafa (e.g., block, ascii, legacy). (Default: "block")
            -colors <string>
               Color mode used by chafa (e.g., 16, 256, full). (Default: "256")
            -dither <string>
               Dithering algorithm used by chafa (e.g., ordered, diffusion, none). (Default: "ordered")
            -w <integer>
               Render width. (Default: terminal width)
            -h <integer>
               Render height. (Default: terminal height - 1)
            -scale <mode>
               Scaling mode: fit, fill or stretch. (Default: "fit")
            -threads <integer>
               Number of parallel threads to use for rendering. (Default: 4)
            -help, -help
               Display this help message and exit.

        Playback Controls (Keyboard Shortcuts):
            Q / Ctrl+C : Exit the program.
        Spacebar : Pause or resume playback.
            + : Increase playback speed.
            - : Decrease playback speed.
            → (right arrow) : Jump forward 5 seconds.
            ← (left arrow) : Jump back 5 seconds.
            ↑ (up arrow) : Previous video in the playlist.
            ↓ (Down arrow): Next video in the playlist.
        `)
}

func main() {
	flag.StringVar(&videoPath, "video", "", "Path to the video file. Can also be provided as a positional argument.")
	flag.IntVar(&fps, "fps", 15, "Frames per second for extraction")
	flag.StringVar(&symbols, "symbols", "block", "Symbols to use for rendering")
	flag.StringVar(&colors, "colors", "256", "Color mode")
	flag.StringVar(&dither, "dither", "ordered", "Dithering mode")
	flag.IntVar(&width, "w", 0, "Display width (default: terminal width)")
	flag.IntVar(&height, "h", 0, "Display height (default: terminal height - 1)")
	flag.StringVar(&scaleMode, "scale", scaleFit, "Scaling mode: fit, fill or stretch")
	flag.IntVar(&numThreads, "threads", 4, "Number of parallel threads for Chafa rendering")
	flag.BoolVar(&showHelp, "help", false, "Show detailed program description")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] /path/to/video\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "A high-performance terminal video player.")
		fmt.Fprintln(os.Stderr, "\nOptions:")
		flag.PrintDefaults()
	}

	flag.Parse()

	if showHelp {
		printHelp()
		os.Exit(0)
	}

	if videoPath == "" {
		if flag.NArg() > 0 {
			videoPath = flag.Arg(0)
		} else {
			fmt.Print("Error: Video file path is required.\r\n")
			flag.Usage()
			os.Exit(1)
		}
	}
	if err := validateNumThreads(numThreads); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		flag.Usage()
		os.Exit(2)
	}
	if err := validateScaleMode(scaleMode); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		flag.Usage()
		os.Exit(2)
	}
	if err := validateDimensions(width, height); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		flag.Usage()
		os.Exit(2)
	}

	seekAmountInFrames = seekSeconds * fps
	frameReadyCond = sync.NewCond(&stateMutex)

	playlist, err := getPlaylist(filepath.Dir(videoPath))
	if err != nil || len(playlist) == 0 {
		log.Fatalf("Failed to find any videos in the directory: %v", err)
	}

	currentVideoIndex := -1
	for i, path := range playlist {
		if path == videoPath {
			currentVideoIndex = i
			break
		}
	}
	if currentVideoIndex == -1 {
		log.Fatalf("Could not find the specified video in the playlist.")
	}

	// --- Setup Terminal ---
	var termErr error
	termWidth, termHeight, termErr := term.GetSize(int(os.Stdout.Fd()))
	if termErr != nil {
		termWidth, termHeight = 80, 24
	}
	if width == 0 {
		width = termWidth
	}
	if height == 0 {
		height = termHeight - 1
	}

	oldState, termErr := term.MakeRaw(int(os.Stdin.Fd()))
	if termErr != nil {
		log.Fatalf("Failed to set terminal to raw mode: %v", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	fmt.Print("\x1b[?25l\x1b[?1049h")
	defer func() {
		// Leave the alternate screen before printing anything: switching back
		// restores the previous screen contents, so a message written while the
		// alternate screen is still active is discarded. The banner lands on the
		// normal screen, so clip it to the terminal rather than to the video
		// canvas - the two differ whenever -w or -h was given.
		fmt.Print("\x1b[?1049l\x1b[?25h")
		bannerWidth := width
		if columns, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			bannerWidth = columns
		}
		fmt.Print("\r\n", clipToTerminalWidth(exitBanner, bannerWidth), "\r\n")
	}()

	// --- Main Control Loop ---
	resumeFrame := 0
	events := readInputEvents()
	for {
		ctx, cancel := context.WithCancel(context.Background())

		// Route events to this playback session without creating another stdin reader.
		inputDone := make(chan struct{})
		go func() {
			handleInput(ctx, events, cancel)
			close(inputDone)
		}()

		action := playVideo(ctx, playlist[currentVideoIndex], resumeFrame)
		cancel() // Ensure everything from the previous video is stopped
		<-inputDone

		switch action {
		case "seek-forward":
			stateMutex.Lock()
			resumeFrame = seekTargetFrame(currentFrameIndex, seekAmountInFrames, totalFrames)
			stateMutex.Unlock()
			continue
		case "seek-backward":
			stateMutex.Lock()
			resumeFrame = seekTargetFrame(currentFrameIndex, -seekAmountInFrames, totalFrames)
			stateMutex.Unlock()
			continue
		case "next":
			currentVideoIndex = (currentVideoIndex + 1) % len(playlist)
			resumeFrame = 0
		case "prev":
			currentVideoIndex = (currentVideoIndex - 1 + len(playlist)) % len(playlist)
			resumeFrame = 0
		case "quit":
			return
		case "finished":
			resumeFrame = 0
			printInfoUnlocked("FINISHED", 0, height, fps, 0, 0)
			// Post-playback input loop
		postLoop:
			for {
				event, ok := <-events
				if !ok || event == inputQuit {
					return
				}
				switch event {
				case inputPrev:
					currentVideoIndex = (currentVideoIndex - 1 + len(playlist)) % len(playlist)
					resumeFrame = 0
					break postLoop
				case inputNext:
					currentVideoIndex = (currentVideoIndex + 1) % len(playlist)
					resumeFrame = 0
					break postLoop
				}
			}
		}
	}
}

// printInfo displays the playback status. It locks the mutex to safely get state.
func printInfo(status string, current, termH, frameRate int, speed float64) {
	stateMutex.Lock()
	total := totalFrames
	stateMutex.Unlock()
	printInfoUnlocked(status, current, termH, frameRate, speed, total)
}

// controlsHint is the fixed key legend appended to the playback status line.
const controlsHint = "Spc:Pause, +/-:Speed, L/R:Seek, U/D:Track, Q:Quit"

// exitBanner is printed once the alternate screen has been left.
const exitBanner = "Playback finished. Thank you for using hitvid!"

// clipToTerminalWidth truncates text to at most width columns. The status line
// is drawn on the terminal's last row, so a single character past the last
// column makes the terminal wrap onto a new row, which scrolls the screen and
// shifts every following frame.
func clipToTerminalWidth(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	return string(runes[:width])
}

// renderInfoLine builds the status line for one playback state, clipped to
// width columns so it can never wrap.
func renderInfoLine(status string, currentFrame, frameRate int, speed float64, totalFrames, width int) string {
	currentTimeStr := formatTime(currentFrame, frameRate)
	totalTimeStr := "??:??"
	if totalFrames > 0 {
		totalTimeStr = formatTime(totalFrames, frameRate)
	}
	var info string
	switch status {
	case "PLAYING", "PAUSED":
		info = fmt.Sprintf("[%s] %s / %s | Speed: %.2fx | %s", status, currentTimeStr, totalTimeStr, speed, controlsHint)
	case "BUFFERING":
		info = fmt.Sprintf("[%s] %s / %s...", status, currentTimeStr, totalTimeStr)
	case "FINISHED":
		info = "Playback finished. Press UP/DOWN for next/prev, or Q to quit."
	}
	return clipToTerminalWidth(info, width)
}

// printInfoUnlocked is the core display logic without mutex locking.
func printInfoUnlocked(status string, currentFrame, termH, frameRate int, speed float64, totalFrames int) {
	infoLine := termH + 1
	info := renderInfoLine(status, currentFrame, frameRate, speed, totalFrames, width)
	fmt.Printf("\x1b[%d;1H\x1b[K%s", infoLine, info)
}
