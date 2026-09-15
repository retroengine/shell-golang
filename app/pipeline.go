package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// splitPipeline scans args for a single "|" token and splits it into the two
// command segments on either side. isPipeline is false (with left/right nil,
// err nil) when args contains no "|" at all, so callers can fall through to
// their normal non-pipeline handling. This stage only supports a pipeline of
// exactly two commands, so a leading/trailing "|" (an empty side) or more
// than one "|" is reported as a syntax error instead of silently guessed at.
func splitPipeline(args []string) (left, right []string, isPipeline bool, err error) {
	idx := -1
	count := 0
	for i, a := range args {
		if a == "|" {
			count++
			if idx == -1 {
				idx = i
			}
		}
	}

	if count == 0 {
		return nil, nil, false, nil
	}
	if count > 1 {
		return nil, nil, true, fmt.Errorf("syntax error: only a single pipe between two commands is supported")
	}

	left = args[:idx]
	right = args[idx+1:]

	if len(left) == 0 || len(right) == 0 {
		return nil, nil, true, fmt.Errorf("syntax error near unexpected token `|'")
	}

	return left, right, true, nil
}

// isBuiltin reports whether name is a recognised shell builtin, using the
// same membership main's dispatch switch and handleTYPE check against.
func isBuiltin(name string) bool {
	_, ok := builtinNames[name]
	return ok
}

// captureBuiltin runs a builtin in-process (no fork/exec) and returns what
// it would send to stdout, for when it is the producer half of a pipeline.
// hasOutput mirrors main's own per-builtin rule for whether an empty result
// still counts as a line to emit: echo, pwd and type always print their
// result even when empty; jobs and complete suppress an empty result; cd
// never has stdout at all, only a possible error. "exit" is a no-op here —
// see handlePipeline's doc comment for why a builtin mid-pipeline must not
// tear down the shell loop.
func captureBuiltin(args []string) (output string, hasOutput bool, err error) {
	switch args[0] {
	case "echo":
		s, _ := handleEcho(args) // never errors
		return s, true, nil
	case "pwd":
		s, err := handlePWD(args)
		return s, err == nil, err
	case "cd":
		return "", false, handleCD(args)
	case "type":
		s, err := handleTYPE(args, builtinNames)
		return s, err == nil, err
	case "jobs":
		s := handleJobs(args)
		return s, s != "", nil
	case "complete":
		s := handleComplete(args)
		return s, s != "", nil
	case "exit":
		return "", false, nil
	default:
		return "", false, fmt.Errorf("%s: not a builtin", args[0])
	}
}

// printBuiltinInPipeline runs a builtin in-process as the final stage of a
// pipeline and prints its result exactly as main's non-pipeline dispatch
// would — there is no redirect handling here because a pipeline segment has
// no redirect target of its own in this stage. The returned error is only
// ever the builtin's own operation failing (e.g. cd into a missing
// directory); a successful "not found" result from type is printed, not
// returned as an error, matching main.go.
func printBuiltinInPipeline(args []string) error {
	switch args[0] {
	case "echo":
		s, _ := handleEcho(args)
		printLine(s)
	case "pwd":
		s, err := handlePWD(args)
		if err != nil {
			return err
		}
		printLine(s)
	case "cd":
		return handleCD(args)
	case "type":
		s, err := handleTYPE(args, builtinNames)
		if err != nil {
			return err
		}
		printLine(s)
	case "jobs":
		if s := handleJobs(args); s != "" {
			printLine(s)
		}
	case "complete":
		if s := handleComplete(args); s != "" {
			printLine(s)
		}
	case "exit":
		// no-op: see handlePipeline's doc comment
	}
	return nil
}

// handlePipeline connects leftArgs to rightArgs, one of four ways depending
// on which side(s) are shell builtins. Builtins in this shell are just Go
// functions with no process of their own, so they run in-process rather
// than through the OS pipe; external commands are unaffected, still
// wired together exactly as before builtins were supported.
//
// A builtin that is not the pipeline's final stage never sees the other
// side's data: none of this shell's builtins read stdin, so a builtin
// producer's output has nowhere to be consumed, and an external producer
// feeding a builtin consumer is simply drained and discarded. Both are the
// correct stand-in for "piping into something that ignores its input" —
// matching the spec's "ls | type exit" example, where ls's listing must not
// reach the terminal. "exit" partway through a pipeline is treated as a
// no-op rather than ending the shellLoop in main: in real shells each
// non-final pipeline stage runs in a subshell, so "exit" there never kills
// the interactive shell either.
func handlePipeline(leftArgs, rightArgs []string) (string, error) {
	leftBuiltin := isBuiltin(leftArgs[0])
	rightBuiltin := isBuiltin(rightArgs[0])

	switch {
	case leftBuiltin && rightBuiltin:
		return pipelineBuiltinToBuiltin(leftArgs, rightArgs)
	case leftBuiltin:
		return pipelineBuiltinToExternal(leftArgs, rightArgs)
	case rightBuiltin:
		return pipelineExternalToBuiltin(leftArgs, rightArgs)
	default:
		return pipelineExternalToExternal(leftArgs, rightArgs)
	}
}

// pipelineBuiltinToBuiltin runs both sides in-process; see handlePipeline's
// doc comment for why the left side's output is discarded rather than fed
// to the right side.
func pipelineBuiltinToBuiltin(leftArgs, rightArgs []string) (string, error) {
	if _, _, err := captureBuiltin(leftArgs); err != nil {
		printLine(err.Error())
	}
	return "", printBuiltinInPipeline(rightArgs)
}

// pipelineBuiltinToExternal runs leftArgs in-process and feeds whatever it
// would have printed into rightArgs, an external command, through an OS
// pipe — the same wiring pipelineExternalToExternal gives two external
// commands, just with the write side driven by a Go string instead of a
// child process.
func pipelineBuiltinToExternal(leftArgs, rightArgs []string) (string, error) {
	if _, err := exec.LookPath(rightArgs[0]); errors.Is(err, exec.ErrNotFound) {
		return fmt.Sprintf("%s: command not found", rightArgs[0]), err
	} else if err != nil {
		return "Error while visiting file", err
	}

	leftOutput, hasOutput, leftErr := captureBuiltin(leftArgs)
	if leftErr != nil {
		printLine(leftErr.Error())
		hasOutput = false
	}

	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}

	right := exec.Command(rightArgs[0], rightArgs[1:]...)
	right.Stdin = r
	right.Stdout = os.Stdout
	right.Stderr = os.Stderr

	if err := right.Start(); err != nil {
		r.Close()
		w.Close()
		return "Error while executing file.", err
	}
	r.Close() // the child keeps its own dup'd copy

	if hasOutput {
		io.WriteString(w, leftOutput+"\n") // matches printLine's own trailing newline
	}
	w.Close() // signals EOF now that nothing more is coming

	if err := right.Wait(); err != nil {
		return "Error while executing file.", err
	}
	return "", nil
}

// pipelineExternalToBuiltin runs leftArgs as an external command and, once
// it finishes, runs rightArgs in-process. leftArgs' stdout is drained
// through an OS pipe into io.Discard concurrently with it running — see
// handlePipeline's doc comment for why discarding, not connecting it to the
// builtin, is correct — which also prevents a chatty producer (e.g. ls on a
// large directory) from blocking once the pipe's OS buffer fills.
func pipelineExternalToBuiltin(leftArgs, rightArgs []string) (string, error) {
	if _, err := exec.LookPath(leftArgs[0]); errors.Is(err, exec.ErrNotFound) {
		return fmt.Sprintf("%s: command not found", leftArgs[0]), err
	} else if err != nil {
		return "Error while visiting file", err
	}

	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}

	left := exec.Command(leftArgs[0], leftArgs[1:]...)
	left.Stdin = os.Stdin
	left.Stdout = w
	left.Stderr = os.Stderr

	if err := left.Start(); err != nil {
		r.Close()
		w.Close()
		return "Error while executing file.", err
	}
	w.Close() // the child keeps its own dup'd copy

	drained := make(chan struct{})
	go func() {
		io.Copy(io.Discard, r)
		close(drained)
	}()

	left.Wait() // a failing producer is not a pipeline-level error, same as pipelineExternalToExternal
	<-drained
	r.Close()

	return "", printBuiltinInPipeline(rightArgs)
}

// pipelineExternalToExternal connects leftArgs' standard output to
// rightArgs' standard input through an OS pipe, running both as external
// commands concurrently (Start, not Run, on each) so streaming data — e.g.
// tail -f — reaches the second command as it arrives rather than only once
// the first command exits. leftArgs inherits the shell's stdin; both
// commands inherit the shell's stderr; rightArgs' stdout goes to the
// shell's stdout.
func pipelineExternalToExternal(leftArgs, rightArgs []string) (string, error) {
	if _, err := exec.LookPath(leftArgs[0]); errors.Is(err, exec.ErrNotFound) {
		return fmt.Sprintf("%s: command not found", leftArgs[0]), err
	} else if err != nil {
		return "Error while visiting file", err
	}

	if _, err := exec.LookPath(rightArgs[0]); errors.Is(err, exec.ErrNotFound) {
		return fmt.Sprintf("%s: command not found", rightArgs[0]), err
	} else if err != nil {
		return "Error while visiting file", err
	}

	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}

	left := exec.Command(leftArgs[0], leftArgs[1:]...)
	left.Stdin = os.Stdin
	left.Stdout = w
	left.Stderr = os.Stderr

	right := exec.Command(rightArgs[0], rightArgs[1:]...)
	right.Stdin = r
	right.Stdout = os.Stdout
	right.Stderr = os.Stderr

	if err := left.Start(); err != nil {
		r.Close()
		w.Close()
		return "Error while executing file.", err
	}
	if err := right.Start(); err != nil {
		w.Close()
		r.Close()
		left.Wait()
		return "Error while executing file.", err
	}

	// The parent's own copies must close right after Start(): the child
	// processes keep their own dup'd handles, and until every write-end
	// copy is closed, the reader never sees EOF (or, for a stalled writer
	// like tail -f, it never sees its pipe's read side go away either).
	w.Close()
	r.Close()

	left.Wait() // exit status of a pipeline is the last command's; a producer killed by SIGPIPE once its reader exits is expected, not a shell-level error

	if err := right.Wait(); err != nil {
		return "Error while executing file.", err
	}

	return "", nil
}
