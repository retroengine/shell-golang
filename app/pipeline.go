package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// splitPipelineStages scans args for "|" tokens and splits it into two or
// more command segments, one per pipeline stage. isPipeline is false (with
// stages nil, err nil) when args contains no "|" at all, so callers can fall
// through to their normal non-pipeline handling. A leading/trailing "|" or
// two "|" back to back (an empty stage) is reported as a syntax error
// instead of silently guessed at; any other number of "|" tokens, with a
// non-empty segment on every side, is a valid pipeline of that many stages.
func splitPipelineStages(args []string) (stages [][]string, isPipeline bool, err error) {
	var current []string
	for _, a := range args {
		if a != "|" {
			current = append(current, a)
			continue
		}
		if len(current) == 0 {
			return nil, true, fmt.Errorf("syntax error near unexpected token `|'")
		}
		stages = append(stages, current)
		current = nil
	}

	if len(stages) == 0 { // no "|" seen at all
		return nil, false, nil
	}
	if len(current) == 0 {
		return nil, true, fmt.Errorf("syntax error near unexpected token `|'")
	}
	stages = append(stages, current)
	return stages, true, nil
}

// splitPipeline is the two-stage form of splitPipelineStages, kept for its
// existing callers/tests. A pipeline of anything other than exactly two
// stages is reported the same way it always has been: as a syntax error,
// not silently truncated or expanded.
func splitPipeline(args []string) (left, right []string, isPipeline bool, err error) {
	stages, isPipeline, err := splitPipelineStages(args)
	if !isPipeline {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, true, err
	}
	if len(stages) != 2 {
		return nil, nil, true, fmt.Errorf("syntax error: only a single pipe between two commands is supported")
	}
	return stages[0], stages[1], true, nil
}

// isBuiltin reports whether name is a recognised shell builtin, using the
// same membership main's dispatch switch and handleTYPE check against.
func isBuiltin(name string) bool {
	_, ok := builtinNames[name]
	return ok
}

// captureBuiltin runs a builtin in-process (no fork/exec) and returns what
// it would send to stdout, for when it is a producer stage of a pipeline.
// hasOutput mirrors main's own per-builtin rule for whether an empty result
// still counts as a line to emit: echo, pwd and type always print their
// result even when empty; jobs and complete suppress an empty result; cd
// never has stdout at all, only a possible error. "exit" is a no-op here —
// see handlePipelineStages' doc comment for why a builtin mid-pipeline must
// not tear down the shell loop.
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
		// no-op: see handlePipelineStages' doc comment
	}
	return nil
}

// handlePipeline is the two-stage form of handlePipelineStages, kept for its
// existing callers/tests.
func handlePipeline(leftArgs, rightArgs []string) (string, error) {
	return handlePipelineStages([][]string{leftArgs, rightArgs})
}

// pending describes what the stage after the one just processed must wire
// its stdin to. The zero value (isFirst) means "the shell's own stdin",
// which is only valid for stage 0.
type pipelinePending struct {
	pipeR         *os.File // read end of a still-open pipe from an external producer
	builtinOut    string   // captured output from a builtin producer
	hasBuiltinOut bool
	fromBuiltin   bool
	isFirst       bool
}

// handlePipelineStages connects an arbitrary chain of two or more pipeline
// stages, in order. Builtins in this shell are just Go functions with no
// process of their own, so they run in-process rather than through an OS
// pipe; external commands are wired together via os.Pipe() and fork/exec,
// exactly as a two-stage pipeline always has been.
//
// A builtin that is not the pipeline's final stage never sees the previous
// stage's data: none of this shell's builtins read stdin, so a builtin
// producer's output has nowhere to be consumed by the builtin that follows
// it, and an external producer feeding a builtin consumer is simply drained
// and discarded. Both are the correct stand-in for "piping into something
// that ignores its input". "exit" partway through a pipeline is treated as
// a no-op rather than ending the shellLoop in main: in real shells each
// non-final pipeline stage runs in a subshell, so "exit" there never kills
// the interactive shell either.
//
// Every external stage's command name is checked against PATH before any
// stage runs, so an unresolvable name later in the chain is reported
// without any builtin earlier in the chain (e.g. a producer "cd") having
// already taken effect — the same "validate both sides before starting
// either" rule a two-stage pipeline has always applied, extended to however
// many stages there are.
func handlePipelineStages(stages [][]string) (string, error) {
	for _, stage := range stages {
		if isBuiltin(stage[0]) {
			continue
		}
		if _, err := exec.LookPath(stage[0]); errors.Is(err, exec.ErrNotFound) {
			return fmt.Sprintf("%s: command not found", stage[0]), err
		} else if err != nil {
			return "Error while visiting file", err
		}
	}

	var (
		cmds    []*exec.Cmd // every non-final external command, started but not yet waited on
		drainWG sync.WaitGroup
	)
	cleanup := func() {
		for _, c := range cmds {
			c.Wait() // ignored: only the pipeline's final stage can fail the pipeline
		}
		drainWG.Wait()
	}

	cur := pipelinePending{isFirst: true}
	n := len(stages)

	for i, stage := range stages {
		isLast := i == n-1

		if isBuiltin(stage[0]) {
			switch {
			case cur.isFirst, cur.fromBuiltin:
				// nothing arriving through an OS pipe to dispose of
			case cur.pipeR != nil:
				r := cur.pipeR
				drainWG.Add(1)
				go func() {
					defer drainWG.Done()
					io.Copy(io.Discard, r)
					r.Close()
				}()
			}

			if isLast {
				err := printBuiltinInPipeline(stage)
				cleanup()
				return "", err
			}

			out, hasOut, err := captureBuiltin(stage)
			if err != nil {
				printLine(err.Error())
				hasOut = false
			}
			cur = pipelinePending{fromBuiltin: true, builtinOut: out, hasBuiltinOut: hasOut}
			continue
		}

		cmd := exec.Command(stage[0], stage[1:]...)
		cmd.Stderr = os.Stderr

		var feedW *os.File
		switch {
		case cur.isFirst:
			cmd.Stdin = os.Stdin
		case cur.fromBuiltin:
			r, w, err := os.Pipe()
			if err != nil {
				cleanup()
				return "", err
			}
			cmd.Stdin = r
			feedW = w
		default:
			cmd.Stdin = cur.pipeR
		}

		var outR, outW *os.File
		if isLast {
			cmd.Stdout = os.Stdout
		} else {
			r, w, err := os.Pipe()
			if err != nil {
				if feedW != nil {
					feedW.Close()
				}
				cleanup()
				return "", err
			}
			cmd.Stdout = w
			outR, outW = r, w
		}

		if err := cmd.Start(); err != nil {
			if feedW != nil {
				feedW.Close()
			}
			if outR != nil {
				outR.Close()
			}
			if outW != nil {
				outW.Close()
			}
			cleanup()
			return "Error while executing file.", err
		}

		if !cur.isFirst && !cur.fromBuiltin {
			cur.pipeR.Close() // the child keeps its own dup'd copy
		}
		if outW != nil {
			outW.Close() // same: the child owns the copy it inherited
		}
		if feedW != nil {
			if cur.hasBuiltinOut {
				io.WriteString(feedW, cur.builtinOut+"\n") // matches printLine's own trailing newline
			}
			feedW.Close() // signals EOF now that nothing more is coming
		}

		if isLast {
			err := cmd.Wait()
			cleanup()
			if err != nil {
				return "Error while executing file.", err
			}
			return "", nil
		}

		cmds = append(cmds, cmd)
		cur = pipelinePending{pipeR: outR}
	}

	return "", nil // unreachable: the loop above always returns on its last iteration
}
