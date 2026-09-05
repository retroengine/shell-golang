package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/term"
)

// The words <TAB> is allowed to complete.
var autocompleteCommands = []string{
"alias", "apropos", "awk", "basename", "bash", "bc", "bg", "bind", "break", "builtin", 
"caller", "cat", "cd", "chgrp", "chmod", "chown", "cksum", "clear", "cmp", "comm", "command", 
"compgen", "complete", "continue", "cp", "cron", "cut", "date", "dd", "declare", "df", "diff", 
"dirname", "dirs", "disown", "du", "echo", "egrep", "enable", "env", "eval", "exec", "exit", 
"export", "false", "fg", "fgrep", "file", "find", "fold", "for", "free", "getopts", "grep", 
"groups", "gunzip", "gzip", "head", "help", "history", "hostname", "id", "if", "jobs", "join", 
"kill", "killall", "less", "let", "ln", "locate", "logout", "ls", "lsof", "make", "man", "mkdir", 
"mkfifo", "more", "mount", "mv", "nice", "nohup", "passwd", "paste", "pathchk", "ping", "printf", 
"ps", "pwd", "read", "readlink", "readonly", "realpath", "renice", "return", "rm", "rmdir", "sed",
"seq", "set", "shift", "shopt", "shutdown", "sleep", "sort", "source", "split", "ssh", "stat", "strings", 
"su", "sudo", "tail", "tar", "tee", "test", "time", "timeout", "top", "touch", "tr", "trap", "true", 
"type", "ulimit", "umask", "unalias", "uname", "uniq", "unset", "unzip", "uptime", "users", "wc", "whereis",
"which", "who", "whoami", "xargs", "yes", "zip", "jobs"}

// handleAutocomplete returns partial's match completed with a trailing space, or "" if none match.
func handleAutocomplete(partial string) string {
	if partial == "" {
		return ""
	}
	for _, cmd := range autocompleteCommands {
		if strings.HasPrefix(cmd, partial) {
			return cmd + " "
		}
	}
	return ""
}

func matchingExecutables(partial string) []string {
	if partial == "" {
		return nil
	}

	seen := make(map[string]struct{})

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // PATH may list directories that don't exist on disk
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasPrefix(entry.Name(), partial) {
				seen[entry.Name()] = struct{}{}
			}
		}
	}

	matches := make([]string, 0, len(seen))
	for name := range seen {
		matches = append(matches, name)
	}
	sort.Strings(matches)
	return matches
}

func handleAutoCompleteExe(partial string) (string, error) {
	matches := matchingExecutables(partial)
	if len(matches) != 1 {
		return "", nil
	}
	return matches[0] + " ", nil
}

// longestCommonPrefix returns the longest prefix shared by every string in strs (strs must be non-empty).
func longestCommonPrefix(strs []string) string {
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// isBareCommandPrefix reports whether input is still a single word (no space, quote, or backslash yet).
func isBareCommandPrefix(input []byte) bool {
	return !strings.ContainsAny(string(input), " \t'\"\\")
}

func readLine(reader *bufio.Reader) (string, error) {
	fd := int(os.Stdin.Fd())
	isTerm := term.IsTerminal(fd)

	if isTerm {
		// Real terminal: switch to raw mode so we get every keystroke immediately instead of a whole buffered line.
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			isTerm = false
		} else {
			defer term.Restore(fd, oldState) // restore normal terminal settings once we return
		}
	}

	var input []byte // the line, built up one byte at a time

	consecutiveTabs := 0 // consecutive <TAB> presses on a bare command prefix, so the second one can list ambiguous matches

	var cycleMatches []string // ambiguous matches currently being cycled through, once the list has been shown
	cycleIndex := 0

	for {
		b, err := reader.ReadByte()
		if err != nil {
			return string(input), err // stdin closed / read failed
		}

		switch b {
		case '\r', '\n': // Enter
			if isTerm {
				fmt.Print("\r\n")
			}
			return string(input), nil

		case 127, 8: // Backspace
			consecutiveTabs = 0
			cycleMatches = nil
			if len(input) > 0 {
				input = input[:len(input)-1]
			}

		case '\t':
			// Tab: complete an unambiguous match, bell on no/first-ambiguous match, list on the 2nd press, then cycle (PowerShell-style) after that.
			if !isBareCommandPrefix(input) {
				consecutiveTabs = 0
				cycleMatches = nil
				input = append(input, b)
				break
			}

			if cycleMatches != nil {
				cycleIndex = (cycleIndex + 1) % len(cycleMatches)
				input = []byte(cycleMatches[cycleIndex])
				break
			}

			consecutiveTabs++

			cmd := handleAutocomplete(string(input));
			if cmd != "" {
				input = []byte(cmd)
				consecutiveTabs = 0
				break
			}

			matches := matchingExecutables(string(input))
			switch len(matches) {
			case 0:
				fmt.Print("\x07") // \x07 is the ASCII BEL char, beeps the terminal; no match, input unchanged
				consecutiveTabs = 0
			case 1:
				input = []byte(matches[0] + " ")
				consecutiveTabs = 0
			default: // 2+ matches
				if lcp := longestCommonPrefix(matches); len(lcp) > len(input) {
					input = []byte(lcp) // matches share a longer prefix than what's typed: complete up to it, no bell/list yet
					consecutiveTabs = 0
					break
				}

				if consecutiveTabs < 2 {
					fmt.Print("\x07") // \x07 (BEL): first tab on an ambiguous prefix just beeps, input unchanged
					break
				}

				if isTerm {
					// \r\n = drop to a fresh line, \033[K = ANSI "erase to end of line", then redraw "$ " + input below the listed matches
					fmt.Printf("\r\n%s\r\n\033[K$ %s", strings.Join(matches, "  "), string(input))
				} else {
					fmt.Printf("\n%s\n$ %s", strings.Join(matches, "  "), string(input))
				}
				consecutiveTabs = 0
				cycleMatches = matches
				cycleIndex = -1 // the next Tab press lands on index 0
				continue        // prompt already redrawn above; skip the redraw below
			}

		default: // ordinary character
			consecutiveTabs = 0
			cycleMatches = nil
			input = append(input, b)
		}

		if isTerm {
			fmt.Printf("\r\033[K$ %s", string(input)) // \r = cursor to line start, \033[K = erase it, then redraw "$ " + input
		}
	}
}

func handleInput(reader *bufio.Reader) ([]string, error) {
	line, err := readLine(reader)

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

// extractRedirect pulls a trailing redirect operator out of args; mode is 1=stdout truncate, 2=stderr truncate, 3=stdout append, 4=stderr append.
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

// stdoutIsTerm is true only when stdout is a real console, not a pipe (as in the e2e tests, which expect plain "\n" output).
var stdoutIsTerm = term.IsTerminal(int(os.Stdout.Fd()))

// printLine prints s plus "\r\n" (carriage return + line feed) on a real terminal, or plain "\n" otherwise — Windows doesn't translate a bare "\n" and every line staircases right without this.
func printLine(s string) {
	if stdoutIsTerm {
		fmt.Print(s + "\r\n")
	} else {
		fmt.Println(s)
	}
}

func writeOutput(target, s string , mode int) error {

	if target == "" {
		printLine(s)
		return nil
	}

	if mode == 1 { // truncate
		return os.WriteFile(target, []byte(s+"\n"), 0644)
	} else { // append
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
		printLine(err.Error())
		return nil
	}

	if mode == 2 { // truncate
		return os.WriteFile(target, []byte(err.Error()+"\n"), 0644)
	} else { // append
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
			printLine(InputErr.Error())
			return
		}

		if len(args) == 0 {
			continue
		}

		cmdArgs, redirectTarget, redirErr , mode := extractRedirect(args)

		if redirErr != nil {
			printLine(redirErr.Error())
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
				printLine(cleanStr)
			}

		case "pwd":
			dirName, err := handlePWD(args)

			if err != nil {
				if mode == 2 || mode == 4{
					writeError(redirectTarget, err,mode)
				} else {
					printLine(fmt.Sprintf("Error printing the working directory %s", err))
				}
				break
			}

			if mode == 1 || mode == 3{
				writeOutput(redirectTarget, dirName, mode)
			} else {
				printLine(dirName)
			}

		case "cd":
			errCD := handleCD(args)

			if errCD != nil {
				printLine(errCD.Error())
			}

		case "type":
			typeString, err := handleTYPE(args, builtInSet)
			if err != nil {
				if mode == 2 || mode == 4{
					writeError(redirectTarget, err,mode)
				} else {
					printLine(err.Error())
				}
				break
			}
			if mode == 1 || mode == 3 {
				writeOutput(redirectTarget, typeString,mode)
			} else {
				printLine(typeString)
			}

		default:
			msg, err := handleExecFile(args, redirectTarget, mode)

			if msg != "" || err != nil {
				printLine(err.Error())
			}

		}
	}
}
