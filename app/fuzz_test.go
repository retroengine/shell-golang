package main

import (
	"bufio"
	"strings"
	"testing"
)

// Fuzz targets. The table tests above check known inputs; these check the
// inputs nobody thought of. The contract being enforced is simply: parsing
// must never panic, whatever arrives on stdin.
//
// Run the seed corpus (fast, part of a normal `go test`):
//     go test -run Fuzz ./...
// Actually fuzz for 30 seconds:
//     go test -fuzz FuzzHandleInput -fuzztime 30s ./...
//
// A crash is written to testdata/fuzz/ and then replays as a normal test
// case forever after, so a bug found once cannot come back unnoticed.
//
// These use failLine (report_test.go) so a fuzz failure reads exactly like a
// table failure. They deliberately do NOT call report: fuzzing runs millions
// of inputs and a tick per input would bury the one that mattered.

func FuzzHandleInput(f *testing.F) {
	f.Add("echo hello\n")
	f.Add("\n")
	f.Add("   \n")
	f.Add("echo   spaced   out\n")
	f.Add("cd ~\n")
	f.Add("")
	f.Add("no trailing newline")
	f.Add("echo '\n")
	f.Add("\x00\n")

	f.Fuzz(func(t *testing.T, line string) {
		// The contract is simply: parsing must never panic. A blank or
		// whitespace-only line legitimately produces zero arguments now
		// that main() guards for that case before indexing args[0].
		handleInput(bufio.NewReader(strings.NewReader(line)))
	})
}

func FuzzHandleEcho(f *testing.F) {
	f.Add("hello")
	f.Add("'quoted'")
	f.Add("")
	f.Add("'")
	f.Add("it's")
	f.Add("\x00")

	f.Fuzz(func(t *testing.T, word string) {
		// Exercised the way main calls it: the command name plus one argument.
		// Quote stripping happens upstream in handleInput's parser; by the
		// time an arg reaches handleEcho, any quote characters left in it are
		// literal content (e.g. from echo "it's"), so handleEcho must join
		// args verbatim rather than interpreting them.
		got, err := handleEcho([]string{"echo", word})

		call := cmdLine([]string{"echo", word})

		if err != nil {
			t.Fatal(failLine(call, "no error", show(err.Error()),
				"handleEcho must return a nil error for every input"))
		}
		if got != word {
			t.Fatal(failLine(call, show(word), show(got),
				"handleEcho joins already-parsed args verbatim; it does not interpret quotes"))
		}
	})
}

// handleTYPE must not panic regardless of what it is asked about, and must
// always return something printable.
func FuzzHandleTYPE(f *testing.F) {
	f.Add("echo")
	f.Add("go")
	f.Add("")
	f.Add("nosuchcmd12345")
	f.Add("../../etc/passwd")

	f.Fuzz(func(t *testing.T, name string) {
		got, err := handleTYPE([]string{"type", name}, testBuiltins())

		call := cmdLine([]string{"type", name})

		if err != nil {
			// A real lookup error is acceptable; an empty message is not.
			if got == "" {
				t.Fatal(failLine(call, "a printable message",
					"nothing printable, alongside error "+show(err.Error()),
					"whatever happens, the shell has something to print"))
			}
			return
		}
		if got == "" {
			t.Fatal(failLine(call, "a non-empty message", show(got),
				"a nil error means the lookup completed, so it must have said something"))
		}
	})
}
