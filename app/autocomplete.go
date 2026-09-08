package main

import (
	"bufio"
	"fmt"
	"os"
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

// matchingExecutables scans every directory on PATH for file names starting with partial.
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

// matchingCWDEntries returns entries in the current directory whose name starts with partial (os.ReadDir already returns them sorted by name).
func matchingCWDEntries(partial string) []os.DirEntry {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil
	}

	matches := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), partial) {
			matches = append(matches, entry)
		}
	}
	return matches
}

// handleAutoCompleteExe completes partial to the single matching PATH executable, or does nothing if zero or several match.
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

// readLine reads one line a byte at a time, handling Enter, Backspace, and Tab completion itself when stdin is a real terminal.
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
		case '\r', '\n': // Enter : user want to end his input
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
			if cycleMatches != nil {
				cycleIndex = (cycleIndex + 1) % len(cycleMatches)
				input = []byte(cycleMatches[cycleIndex])
				break
			}

			consecutiveTabs++

			// Split input into the already-finished part (prefix, carried through untouched) and
			// the word actually being completed (word): no space means the whole buffer is the
			// first word (the command itself); a space means word is whatever follows the last one.
			prefix := ""
			word := string(input)
			if idx := strings.LastIndex(string(input), " "); idx != -1 {
				prefix = string(input[:idx+1])
				word = string(input[idx+1:])
			}

			if prefix == "" {
				// First word: complete against builtin names, then PATH executables (unchanged from before).
				cmd := handleAutocomplete(word)
				if cmd != "" {
					input = []byte(cmd)
					consecutiveTabs = 0
					break
				}

				matches := matchingExecutables(word)
				switch len(matches) {
				case 0:
					fmt.Print("\x07") // \x07 is the ASCII BEL char, beeps the terminal; no match, input unchanged
					consecutiveTabs = 0
				case 1:
					input = []byte(matches[0] + " ")
					consecutiveTabs = 0
				default: // 2+ matches
					if lcp := longestCommonPrefix(matches); len(lcp) > len(word) {
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
				break
			}

			// Later argument: complete against entries in the current working directory.
			entries := matchingCWDEntries(word)
			switch len(entries) {
			case 0:
				fmt.Print("\x07") // no match, input unchanged
				consecutiveTabs = 0
			case 1:
				separator := " " // a file is a finished argument; a directory invites typing further into it
				if entries[0].IsDir() {
					separator = "/"
				}
				input = []byte(prefix + entries[0].Name() + separator)
				consecutiveTabs = 0
			default: // 2+ matches
				names := make([]string, len(entries))
				for i, e := range entries {
					names[i] = e.Name()
				}

				if lcp := longestCommonPrefix(names); len(lcp) > len(word) {
					input = []byte(prefix + lcp) // entries share a longer prefix than what's typed: complete up to it, no bell/list yet
					consecutiveTabs = 0
					break
				}

				if consecutiveTabs < 2 {
					fmt.Print("\x07") // first tab on an ambiguous prefix just beeps, input unchanged
					break
				}

				// display shows directories the usual "ls" way (trailing slash); full carries the
				// untouched prefix along so cycling doesn't erase the earlier arguments already typed.
				display := make([]string, len(entries))
				full := make([]string, len(entries))
				for i, e := range entries {
					display[i] = e.Name()
					if e.IsDir() {
						display[i] += "/"
					}
					full[i] = prefix + e.Name()
				}

				if isTerm {
					fmt.Printf("\r\n%s\r\n\033[K$ %s", strings.Join(display, "  "), string(input))
				} else {
					fmt.Printf("\n%s\n$ %s", strings.Join(display, "  "), string(input))
				}
				consecutiveTabs = 0
				cycleMatches = full
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
