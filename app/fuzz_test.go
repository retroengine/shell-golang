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
		args, err := handleInput(bufio.NewReader(strings.NewReader(line)))

		// When there is no error there must be usable args, because main
		// indexes args[0] immediately without checking.
		if err == nil && len(args) == 0 {
			t.Fatalf("handleInput(%q)\n  got:  empty args with a nil error\n  want: at least one element, since callers index args[0]", line)
		}
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
		got, err := handleEcho([]string{"echo", word})

		if err != nil {
			t.Fatalf("handleEcho([echo %q])\n  got err: %v\n  want err: <nil> for every input", word, err)
		}
		if strings.Contains(got, "'") {
			t.Fatalf("handleEcho([echo %q])\n  got:  %q\n  want: no single quotes left in the result", word, got)
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

		if err != nil {
			// A real lookup error is acceptable; an empty message is not.
			if got == "" {
				t.Fatalf("handleTYPE([type %q])\n  got:  empty message alongside err %v\n  want: a printable message", name, err)
			}
			return
		}
		if got == "" {
			t.Fatalf("handleTYPE([type %q])\n  got:  %q\n  want: a non-empty message when err is nil", name, got)
		}
	})
}
