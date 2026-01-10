package main

import (
	"os/exec"
	"runtime"
)

func OpenURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	//nolint:gosec
	return exec.Command(cmd, args...).Run()
}
