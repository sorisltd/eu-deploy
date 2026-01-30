package utils

import (
	"os"
	"os/exec"
)

// RunShellCommand runs the given command string using bash -lc.
func RunShellCommand(command string, dir string) error {
	cmd := exec.Command("bash", "-lc", command)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
