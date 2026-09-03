package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Every assertion in this package goes through report, so a run reads the
// same way whether a case passed or failed: the call that was made, what was
// expected, what came back. A pass is marked with a green tick, a failure
// with a red cross plus the why note explaining which rule was broken.
//
// Passing cases are logged with t.Logf, which go test only prints under -v.
// Every mode in test.sh / test.ps1 passes -v for exactly that reason.

var markPass, markFail = marks()

// marks picks the pass/fail symbols. SHELL_TEST_ASCII=1 gives [PASS]/[FAIL]
// for consoles that cannot render UTF-8; NO_COLOR=1 keeps the symbols but
// drops the ANSI escapes. Only the symbol is coloured, so redirected output
// and diffs stay clean.
func marks() (string, string) {
	if os.Getenv("SHELL_TEST_ASCII") != "" {
		return "[PASS]", "[FAIL]"
	}
	if os.Getenv("NO_COLOR") != "" {
		return "✓", "✗"
	}
	return "\033[32m✓\033[0m", "\033[31m✗\033[0m"
}

// report renders one case. A pass is logged; a failure fails the subtest and
// appends the why note. call, expected and received are already rendered by
// the caller, which knows whether the value is a string (%q) or a slice (%#v).
func report(t *testing.T, ok bool, call, expected, received, why string) {
	t.Helper()
	if ok {
		t.Logf("%s %s\n    expected: %s\n    received: %s", markPass, call, expected, received)
		return
	}
	// t.Error, not t.Errorf: go vet rejects a non-constant format string.
	t.Error(failLine(call, expected, received, why))
}

// failLine builds the failure block. Split out from report so the must*
// helpers and the fuzz targets can hand it to t.Fatal instead.
func failLine(call, expected, received, why string) string {
	msg := fmt.Sprintf("%s %s\n    expected: %s\n    received: %s", markFail, call, expected, received)
	if why != "" {
		msg += "\n    why:      " + why
	}
	return msg
}

// ============================================================
// rendering
//
// Values print the way you would read them off a terminal — bare, no Go
// syntax. Quoting only appears when the bare form would hide something:
// an empty string, or edge whitespace and control characters you could
// not otherwise see.
// ============================================================

// show renders one value for the expected/received lines. It quotes when the
// bare form would hide something: edge whitespace, control characters, or a
// run of spaces you could see but not count.
func show(s string) string {
	if s == "" {
		return "(empty)"
	}
	if s != strings.TrimSpace(s) || strings.ContainsAny(s, "\t\n\r\x00") || strings.Contains(s, "  ") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// showArgs renders a parsed argument list, one bracketed word per argument,
// so an empty argument is visible as a gap rather than vanishing.
func showArgs(args []string) string {
	if len(args) == 0 {
		return "(no arguments)"
	}
	words := make([]string, len(args))
	for i, a := range args {
		words[i] = "[" + show(a) + "]"
	}
	return strings.Join(words, " ")
}

// cmdLine reconstructs the command line a case stands for, the way it would
// have been typed at the prompt: []string{"echo", "saikiran"} -> echo saikiran.
// Use it for the call label so a run reads as a shell session.
func cmdLine(args []string) string {
	if len(args) == 0 {
		return "(no command)"
	}
	return strings.Join(args, " ")
}

// typed labels a raw stdin line. The trailing newline is dropped, since every
// typed line has one; its absence is called out because that is what the
// case is testing.
func typed(input string) string {
	if input == "" {
		return "(nothing typed)"
	}
	if line, ok := strings.CutSuffix(input, "\n"); ok {
		if line == "" {
			return "(empty line)"
		}
		return show(line)
	}
	return show(input) + " (no trailing newline)"
}

// typedSession labels a whole e2e session, joining its commands so the label
// reads as the sequence that was typed. Not called `session`: e2e_test.go
// uses that name for locals all over, which would shadow it.
func typedSession(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "(empty session)"
	}
	return strings.Join(lines, " ; ")
}

// ============================================================
// value assertions — these report and continue
// ============================================================

// wantEqual compares two strings.
func wantEqual(t *testing.T, call, got, want, why string) {
	t.Helper()
	report(t, got == want, call, show(want), show(got), why)
}

// wantArgs compares two argument slices.
func wantArgs(t *testing.T, call string, got, want []string, why string) {
	t.Helper()
	report(t, equalArgs(got, want), call, showArgs(want), showArgs(got), why)
}

// wantContains checks that got contains want, for the cases where the exact
// output is OS-specific (a PATH lookup, a whole shell session).
func wantContains(t *testing.T, call, got, want, why string) {
	t.Helper()
	report(t, strings.Contains(got, want), call,
		"output containing "+show(want), show(got), why)
}

// wantSameDir compares two directory paths after resolving symlinks, so that
// /var vs /private/var (macOS) and short vs long paths (Windows) match.
func wantSameDir(t *testing.T, call, got, want, why string) {
	t.Helper()
	report(t, sameDir(got, want), call, show(want), show(got), why)
}

// wantErrContains checks that a non-nil error's message contains want.
func wantErrContains(t *testing.T, call string, err error, want, why string) {
	t.Helper()
	received := "no error"
	if err != nil {
		received = show(err.Error())
	}
	ok := err != nil && strings.Contains(err.Error(), want)
	report(t, ok, call, "error containing "+show(want), received, why)
}

// ============================================================
// error-state assertions — these stop the subtest when they fail
//
// A wrong error state means the value that follows is meaningless, so these
// call t.Fatal. Fatal only ends the enclosing subtest, so a table loop built
// on t.Run still reports every one of its rows.
// ============================================================

// mustNoErr requires err to be nil.
func mustNoErr(t *testing.T, call string, err error, why string) {
	t.Helper()
	if err == nil {
		t.Logf("%s %s\n    expected: no error\n    received: no error", markPass, call)
		return
	}
	t.Fatal(failLine(call, "no error", show(err.Error()), why))
}

// mustErr requires err to be non-nil.
func mustErr(t *testing.T, call string, err error, why string) {
	t.Helper()
	if err != nil {
		t.Logf("%s %s\n    expected: an error\n    received: %s", markPass, call, show(err.Error()))
		return
	}
	t.Fatal(failLine(call, "an error", "no error", why))
}
