package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// builtinNames is the set of recognised shell builtins — keys are the
// builtin names, values unused (only membership is checked). It backs both
// main's dispatch switch and handleTYPE, and lets pipeline.go recognise a
// builtin appearing on either side of a "|" without hand-listing names again.
var builtinNames = map[string]string{
	"type":     "get cmd type",
	"echo":     "print",
	"exit":     "exiting",
	"pwd":      "get working directory",
	"cd":       "change directory",
	"complete": "registers autocompletion for given word",
	"jobs":     "to identify the bg task and more",
	"history":  "show previously executed commands",
}

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
func handleExecFile(args []string, redirectTarget string, mode int,jobArg bool) (string, error) {
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

	if jobArg {
		err_cmd := cmd.Start()

		if err_cmd != nil {
			return "",err_cmd
		}

		pid := cmd.Process.Pid
		number := nextJobNumber()

		job := &Job{
			Number:  number,
			PID:     pid,
			Command: strings.Join(args, " "),
			Status:  "Running",
			exited:  make(chan struct{}),
		}
		jobsList = append(jobsList, job)

		printLine(fmt.Sprintf("[%d] %d",number,pid))

		go func() {
			cmd.Wait()
			close(job.exited)
		}()

		return "",nil
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

	case "-r":
		if len(args) < 3 {
			return "In-Valid number of arguments"
		}
		delete(completeSet, args[2])
		return ""

	default:
		return fmt.Sprintf("complete: %s: no completion specification", args[1])
	}
}

// Job describes one background job tracked by the jobs builtin. exited is
// closed by handleExecFile's reaper goroutine once cmd.Wait() returns, so
// handleJobs can check completion without blocking (Go has no portable
// waitpid(WNOHANG) equivalent that would also work on Windows).
type Job struct {
	Number  int
	PID     int
	Command string
	Status  string
	exited  chan struct{}
}

var jobsList []*Job

// nextJobNumber returns the number to assign to a newly started background
// job: 1 if the table is empty, otherwise one more than the highest number
// currently in it. jobsList stays sorted ascending by Number (appended in
// order, and reapAndFormat's removal preserves relative order), so that's
// always its last element.
func nextJobNumber() int {
	if len(jobsList) == 0 {
		return 1
	}
	return jobsList[len(jobsList)-1].Number + 1
}

// reapAndFormat is the shared reaping logic for both call sites (the jobs
// builtin, and automatic pre-prompt reaping): it walks the table in order,
// formats a Done line for anything that has exited (dropping it from
// jobsList), and, when includeRunning is true, also formats a Running line
// for everything still there — interleaved in job-number order either way,
// since jobsList itself is always in that order.
func reapAndFormat(includeRunning bool) []string {
	lines := make([]string, 0, len(jobsList))
	remaining := make([]*Job, 0, len(jobsList))

	for i, job := range jobsList {
		marker := " "
		switch i {
		case len(jobsList) - 1:
			marker = "+"
		case len(jobsList) - 2:
			marker = "-"
		}

		exited := false
		select {
		case <-job.exited:
			exited = true
		default:
		}

		if exited {
			lines = append(lines, fmt.Sprintf("[%d]%s  %-24s%s", job.Number, marker, "Done", job.Command))
		} else {
			remaining = append(remaining, job)
			if includeRunning {
				lines = append(lines, fmt.Sprintf("[%d]%s  %-24s%s", job.Number, marker, job.Status, job.Command+" &"))
			}
		}
	}

	jobsList = remaining
	return lines
}

// handleJobs lists background jobs in the format "[N]<marker>  <status,
// padded to 24 chars><command>", reaping (and reporting as Done) any job
// that has exited since the last check.
func handleJobs(args []string) string {
	return strings.Join(reapAndFormat(true), "\n")
}

// reapCompletedJobs is called once per loop iteration, right before the
// prompt is printed: it reaps and prints only what just finished, without
// listing jobs that are still Running (unlike the jobs builtin).
func reapCompletedJobs() {
	for _, line := range reapAndFormat(false) {
		printLine(line)
	}
}

// historyList records every non-empty command line the shell has dispatched,
// in the order it was executed — including builtins, external commands (even
// ones that failed to run), and the history invocation itself.
var historyList []string

// recordHistory appends the raw dispatched tokens, rejoined with single
// spaces, to historyList. Called once per non-empty input line before
// redirect/pipeline/background parsing strips anything from args.
func recordHistory(args []string) {
	if len(args) == 0 {
		return
	}
	historyList = append(historyList, strings.Join(args, " "))
}

// handleHistory formats historyList as a numbered list ("%5d  %s" per line,
// joined with newlines), matching bash's history builtin output. With no
// args it lists every entry; args[1], if present, limits the listing to
// the last n entries (bash semantics: "last n", not zsh's "starting from
// line n") while each entry keeps its true original 1-based index into
// historyList — only the loop's starting point moves, never the index
// printed for a given line.
//
// args[1] == "-r" and args[1] == "-w" are different modes entirely: "-r"
// reads history from a file at args[2] and appends it to historyList; "-w"
// writes every entry currently in historyList to args[2], one per line
// with a trailing newline, creating the file if needed and overwriting it
// otherwise (matching real bash). Both print nothing of their own (the
// spec's examples show no output between the -r/-w invocation and the next
// prompt).
//
// A non-numeric or negative n isn't specified by the task, and falls back
// to showing everything (handleHistory must stay error-free and total for
// any input, per its existing callers/tests). n=0 does follow real bash:
// "the last zero commands" is an empty listing, not "everything" — so it
// is handled by the same len(historyList)-n arithmetic as any other n,
// clamped at 0 rather than special-cased.
func handleHistory(args []string) string {
	if len(args) > 1 {
		switch args[1] {
		case "-r":
			if len(args) > 2 {
				appendHistoryFromFile(args[2])
			}
			return ""
		case "-w":
			if len(args) > 2 {
				writeHistoryToFile(args[2])
			}
			return ""
		}
	}

	start := 0
	if len(args) > 1 {
		if n, err := strconv.Atoi(args[1]); err == nil && n >= 0 {
			start = len(historyList) - n
			if start < 0 {
				start = 0
			}
		}
	}

	lines := make([]string, 0, len(historyList)-start)
	for i := start; i < len(historyList); i++ {
		lines = append(lines, fmt.Sprintf("%5d  %s", i+1, historyList[i]))
	}
	return strings.Join(lines, "\n")
}

// appendHistoryFromFile reads path and appends each of its non-empty lines
// (trailing \r stripped, for files written on Windows) to historyList, in
// file order, after whatever is already recorded. A missing or unreadable
// file is silently ignored — handleHistory must stay error-free for any
// input, matching its existing callers/tests.
func appendHistoryFromFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			historyList = append(historyList, line)
		}
	}
}

// writeHistoryToFile writes every entry in historyList to path, one per
// line, with a trailing newline — creating the file if it doesn't exist
// and truncating it if it does (matching real bash's history -w, which
// overwrites the target rather than appending to it). Write failures are
// silently ignored — handleHistory must stay error-free for any input,
// matching its existing callers/tests.
func writeHistoryToFile(path string) {
	var b strings.Builder
	for _, cmd := range historyList {
		b.WriteString(cmd)
		b.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(b.String()), 0644)
}