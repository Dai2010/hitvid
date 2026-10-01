//go:build native && !(cgo && (linux || android) && (amd64 || arm64))

package main

import "context"

func nativeBackendAvailable() bool {
	return false
}

func playVideoNative(context.Context, string, int) string {
	return "finished"
}
