package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

func handleInput(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')

	if err != nil {
		return nil, fmt.Errorf("Unable to read Input")
	}

	line = strings.TrimSpace(line)

	var args []string
	var current strings.Builder
	inArg := false
	inQuote := false
	inDoubleQuote := false
	slash := false
	pendingEscape := false

	for _, r := range line {
		switch {
		case slash:
			current.WriteRune(r)
			slash = false
			inArg = true
		case inQuote:
			if r == '\'' {
				inQuote = false
			} else {
				current.WriteRune(r)
			}

		case inDoubleQuote:
			switch {
			case pendingEscape:
				pendingEscape = false
				if r == '"' || r == '\\' {
					current.WriteRune(r)
				} else {
					current.WriteRune('\\')
					current.WriteRune(r)
				}
			case r == '"':
				inDoubleQuote = false
			case r == '\\':
				pendingEscape = true
			default:
				current.WriteRune(r)
			}

		case r == '\'':
			inQuote = true
			inArg = true
		case r == '"':
			inDoubleQuote = true
			inArg = true
		case r == '\\' && !inQuote && !inDoubleQuote:
			slash = true

		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, current.String())
				current.Reset()
				inArg = false
			}
		default:
			current.WriteRune(r)
			inArg = true
		}
	}

	if inArg {
		args = append(args, current.String())
	}

	return args, nil
}

func extractRedirect(args []string) ([]string, string, error, int) {
	for i, a := range args {
		mode := 0
		switch a {
		case ">", "1>":
			mode = 1
		case "2>":
			mode = 2
		case ">>", "1>>":
			mode = 3
		case "2>>":
			mode = 4
		}
		if mode == 0 {
			continue
		}
		if i+1 >= len(args) {
			return nil, "", fmt.Errorf("syntax error: expected file after %s", a), 0
		}
		cleaned := append(append([]string{}, args[:i]...), args[i+2:]...)
		return cleaned, args[i+1], nil, mode
	}
	return args, "", nil, 0
}

func writeOutput(target, s string , mode int) error {

	if target == "" {
		fmt.Println(s)
		return nil
	}

	if mode == 1 {
		return os.WriteFile(target, []byte(s+"\n"), 0644)
	} else {
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

		if err != nil {
			return err
		}
		defer f.Close()

		_ , err = f.WriteString(s + "\n")

		if err != nil {
			return err
		}
	}

	return nil
}

func writeError(target string, err error,mode int) error {
	if err == nil {
		return nil
	}

	if target == "" {
		fmt.Println(err)
		return nil
	}

	if mode == 2 {
		return os.WriteFile(target, []byte(err.Error()+"\n"), 0644)
	} else {
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

		if err != nil {
			return err
		}
		defer f.Close()

		_ , err = f.WriteString(err.Error() + "\n")

		if err != nil {
			return err
		}
	}

	return nil

}

func handleEcho(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}

	cleanStr := strings.Join(args[1:], " ")

	return cleanStr, nil
}

func handlePWD(args []string) (string, error) {
	dir, err := os.Getwd()

	if err != nil {
		return "", err
	}

	return dir, nil
}

func handleCD(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("cd: missing operand")
	}

	if args[1] == "~" {
		homePath := os.Getenv("HOME")
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

func handleTYPE(args []string, builtInSet map[string]string) (string, error) {
	if len(args) == 1 {
		return "No args provided", nil
	}

	_, ok := builtInSet[args[1]]

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
			flags |= os.O_TRUNC
		}
		f, err := os.OpenFile(redirectTarget, flags, 0644)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if mode == 3 || mode == 4 {
			// child processes inherit this handle; O_APPEND grants only
			// FILE_APPEND_DATA on Windows, which most child CRTs can't write
			// through, so seek to EOF on a normally-opened handle instead.
			if _, err := f.Seek(0, io.SeekEnd); err != nil {
				return "", err
			}
		}

		if mode == 2 || mode == 4 {
			cmd.Stderr = f
		} else {
			cmd.Stdout = f
		}
	}

	err_cmd := cmd.Run()

	if err_cmd != nil {
		return "Error while executing file.", err_cmd
	}

	return "", nil

}

func main() {
	reader := bufio.NewReader(os.Stdin)

	builtInSet := map[string]string{
		"type": "get cmd type",
		"echo": "print",
		"exit": "exiting",
		"pwd":  "get working directory",
		"cd":   "change directory",
	}

	shellLoop:
	for {
		fmt.Print("$ ")

		args, InputErr := handleInput(reader)

		if InputErr != nil {
			fmt.Println(InputErr)
			return
		}

		if len(args) == 0 {
			fmt.Println()
			continue
		}

		cmdArgs, redirectTarget, redirErr , mode := extractRedirect(args)

		if redirErr != nil {
			fmt.Println(redirErr)
			continue
		}
		args = cmdArgs

		switch args[0] {
		case "exit":
			break shellLoop
		case "echo":

			cleanStr, _ := handleEcho(args)
			if mode == 1 || mode == 3 {
				writeOutput(redirectTarget, cleanStr,mode)
			} else {
				fmt.Println(cleanStr)
			}

		case "pwd":
			dirName, err := handlePWD(args)

			if err != nil {
				if mode == 2 || mode == 4{
					writeError(redirectTarget, err,mode)
				} else {
					fmt.Printf("Error printing the working directory %s", err)
				}
				break
			}

			if mode == 1 || mode == 3{
				writeOutput(redirectTarget, dirName, mode)
			} else {
				fmt.Println(dirName)
			}

		case "cd":
			errCD := handleCD(args)

			if errCD != nil {
				fmt.Print(errCD)
			}

		case "type":
			typeString, err := handleTYPE(args, builtInSet)
			if err != nil {
				if mode == 2 || mode == 4{
					writeError(redirectTarget, err,mode)
				} else {
					fmt.Print(err)
				}
				break
			}
			if mode == 1 || mode == 3 {
				writeOutput(redirectTarget, typeString,mode)
			} else {
				fmt.Println(typeString)
			}

		default:
			msg, err := handleExecFile(args, redirectTarget, mode)

			if msg != "" || err != nil {
				fmt.Print(err)
			}

		}
		fmt.Println()
	}
}
