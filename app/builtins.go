package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

// handleEcho joins everything after the command name with single spaces (args[0] is "echo" itself).
func handleEcho(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}

	cleanStr := strings.Join(args[1:], " ")

	return cleanStr, nil
}

// handlePWD returns the current working directory; args is unused since pwd takes no arguments.
func handlePWD(args []string) (string, error) {
	dir, err := os.Getwd()

	if err != nil {
		return "", err
	}

	return dir, nil
}

// handleCD changes directory to args[1], resolving "~" to $HOME.
func handleCD(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("cd: missing operand")
	}

	if args[1] == "~" {
		homePath := os.Getenv("HOME") // "~" only, no "~user" or "~/path" expansion
		err := os.Chdir(homePath)
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cd: %s: No such home directory", homePath)
		}
		if err != nil {
			return err
		}
	} else {
		err := os.Chdir(args[1])
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cd: %s: No such directory", args[1])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// handleTYPE classifies args[1] as a shell builtin, an external command resolved via PATH, or not found.
func handleTYPE(args []string, builtInSet map[string]string) (string, error) {
	if len(args) == 1 {
		return "No args provided", nil
	}

	_, ok := builtInSet[args[1]] // only the key matters here; builtInSet's values are unused placeholders

	if ok {
		return fmt.Sprintf("%s is a shell builtin", args[1]), nil
	} else {
		pathAns, err := exec.LookPath(args[1])

		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Sprintf("%s: not found", args[1]), nil
		}

		if err != nil {
			return "Error while visiting file", err
		}

		return fmt.Sprintf("%s is %s", args[1], pathAns), nil
	}
}

// handleExecFile runs an external program resolved from PATH, wiring stdin/stdout/stderr through (or to redirectTarget per mode).
func handleExecFile(args []string, redirectTarget string, mode int) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("no command provided")
	}

	_, err := exec.LookPath(args[0])

	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Sprintf("%s: command not found", args[0]), err
	}

	if err != nil {
		return "Error while visiting file", err
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if mode == 1 || mode == 2 || mode == 3 || mode == 4 {
		flags := os.O_WRONLY | os.O_CREATE
		if mode == 1 || mode == 2 {
			flags |= os.O_TRUNC // truncate modes overwrite the file
		}
		f, err := os.OpenFile(redirectTarget, flags, 0644)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if mode == 3 || mode == 4 {
			// Windows O_APPEND only grants FILE_APPEND_DATA, which most child processes can't write through, so seek to EOF on a normal handle instead.
			if _, err := f.Seek(0, io.SeekEnd); err != nil {
				return "", err
			}
		}

		if mode == 2 || mode == 4 {
			cmd.Stderr = f // stderr modes
		} else {
			cmd.Stdout = f // stdout modes
		}
	}

	err_cmd := cmd.Run()

	if err_cmd != nil {
		return "Error while executing file.", err_cmd
	}

	return "", nil

}

var completeSet = map[string]string{}

func handleComplete(args []string) string {
	if len(args) < 2 {
		return "In-Valid number of arguments"
	}
	switch args[1] {
	case "-C":
		if len(args) < 4 {
			return "In-Valid number of arguments"
		}
		completeSet[args[3]] = args[2]
		return ""
	case "-p":
		if len(args) < 3 {
			return "In-Valid number of arguments"
		}
		val, ok := completeSet[args[2]]

		if !ok {
			return fmt.Sprintf("complete: %s: no completion specification", args[2])
		} else {
			return fmt.Sprintf("complete -C '%s' %s", val, args[2])
		}

	default:
		return fmt.Sprintf("complete: %s: no completion specification", args[1])
	}
}