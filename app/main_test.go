package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests are organised per function, and within each function into three
// tables: VALID (happy path), EDGE (surprising-but-correct), MUST FAIL
// (the function is expected to report a failure).
//
// Every case carries a `why` note and every assertion goes through a helper
// from report_test.go, so each case prints the same expected/received block
// whether it passed or failed. Never write a bare t.Errorf/t.Fatalf inside a
// table — use wantEqual / wantArgs / wantContains / wantSameDir /
// wantErrContains / mustNoErr / mustErr.
//
// Note on "failure": these functions report failure two different ways.
// handleCD / handleExecFile return a real error. handleTYPE reports a
// missing command with a *string* ("x: not found") and a nil error. The
// tables below assert whichever one the function actually contracts for.
//
// "go" is used wherever a test needs a real external command: it is on
// PATH in every shell that can run `go test`, unlike `ls` which does not
// exist on Windows outside Git Bash.

// ============================================================
// handleInput
// ============================================================

func TestHandleInput_Valid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
		why   string
	}{
		{
			name:  "single command",
			input: "pwd\n",
			want:  []string{"pwd"},
			why:   "a bare command becomes a one-element argument list",
		},
		{
			name:  "command with one argument",
			input: "cd /tmp\n",
			want:  []string{"cd", "/tmp"},
			why:   "the space separates the command from its argument",
		},
		{
			name:  "command with several arguments",
			input: "echo hello world\n",
			want:  []string{"echo", "hello", "world"},
			why:   "every space-separated word becomes its own element",
		},
		{
			name:  "surrounding whitespace is trimmed",
			input: "   pwd   \n",
			want:  []string{"pwd"},
			why:   "the line is trimmed before it is split",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := typed(tt.input)
			got, err := handleInput(bufio.NewReader(strings.NewReader(tt.input)))

			mustNoErr(t, call, err, tt.why)
			wantArgs(t, call, got, tt.want, tt.why)
		})
	}
}

func TestHandleInput_Edge(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
		why   string
	}{
		{
			name:  "repeated spaces collapse into one boundary",
			input: "echo   hello\n",
			want:  []string{"echo", "hello"},
			why:   "spec: consecutive whitespace is collapsed outside quotes",
		},
		{
			name:  "empty line yields no arguments",
			input: "\n",
			want:  nil,
			why:   "an empty line has no words on it, so it tokenizes to zero arguments",
		},
		{
			name:  "whitespace-only line yields no arguments",
			input: "    \n",
			want:  nil,
			why:   "trimmed to empty, then tokenizes to zero arguments",
		},
		{
			name:  "tab is treated as a separator like space",
			input: "echo\thello\n",
			want:  []string{"echo", "hello"},
			why:   "spec: tab is whitespace just like space outside quotes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := typed(tt.input)
			got, err := handleInput(bufio.NewReader(strings.NewReader(tt.input)))

			mustNoErr(t, call, err, tt.why)
			wantArgs(t, call, got, tt.want, tt.why)
		})
	}
}

func TestHandleInput_MustFail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		why   string
	}{
		{
			name:  "empty input hits EOF immediately",
			input: "",
			why:   "nothing to read at all",
		},
		{
			name:  "line without a trailing newline is rejected",
			input: "echo hello",
			why:   "ReadString returns the data AND io.EOF; the error is checked first, so the data is discarded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := typed(tt.input)
			got, err := handleInput(bufio.NewReader(strings.NewReader(tt.input)))

			mustErr(t, call, err, tt.why)
			wantArgs(t, call, got, nil, "the error must come with nil args, not partial data")
		})
	}
}

// ============================================================
// handleInput — single quotes
// ============================================================

func TestHandleInput_SingleQuotes_Valid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
		why   string
	}{
		{
			name:  "spaces are preserved within quotes",
			input: "echo 'hello    world'\n",
			want:  []string{"echo", "hello    world"},
			why:   "spec: spaces are preserved within quotes",
		},
		{
			name:  "consecutive unquoted spaces collapse",
			input: "echo hello    world\n",
			want:  []string{"echo", "hello", "world"},
			why:   "spec: consecutive spaces are collapsed unless quoted",
		},
		{
			name:  "adjacent quoted strings concatenate",
			input: "echo 'hello''world'\n",
			want:  []string{"echo", "helloworld"},
			why:   "spec: adjacent quoted strings 'hello' and 'world' are concatenated",
		},
		{
			name:  "empty quotes next to unquoted text are ignored",
			input: "echo hello''world\n",
			want:  []string{"echo", "helloworld"},
			why:   "spec: empty quotes '' are ignored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := typed(tt.input)
			got, err := handleInput(bufio.NewReader(strings.NewReader(tt.input)))

			mustNoErr(t, call, err, tt.why)
			wantArgs(t, call, got, tt.want, tt.why)
		})
	}
}

func TestHandleInput_SingleQuotes_Edge(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
		why   string
	}{
		{
			name:  "special characters lose meaning inside quotes",
			input: "echo '$HOME * ~'\n",
			want:  []string{"echo", "$HOME * ~"},
			why:   "spec: characters inside single quotes, including $, *, and ~, lose their special meaning and are treated literally",
		},
		{
			name:  "whitespace outside quotes still delimits quoted arguments",
			input: "echo 'a'   'b'\n",
			want:  []string{"echo", "a", "b"},
			why:   "spec: consecutive whitespace outside quotes is collapsed, but still separates arguments",
		},
		{
			name:  "a quoted segment merges with adjacent unquoted text",
			input: "echo 'hello'world\n",
			want:  []string{"echo", "helloworld"},
			why:   "spec: quoted strings placed next to other text form a single argument",
		},
		{
			name:  "a lone pair of empty quotes is still an argument boundary",
			input: "echo '' next\n",
			want:  []string{"echo", "", "next"},
			why:   "spec: empty quotes only vanish when adjacent to other text with no separating whitespace; surrounded by spaces they still form an argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := typed(tt.input)
			got, err := handleInput(bufio.NewReader(strings.NewReader(tt.input)))

			mustNoErr(t, call, err, tt.why)
			wantArgs(t, call, got, tt.want, tt.why)
		})
	}
}

// ============================================================
// handleInput — single quotes with external-command arguments
// ============================================================

func TestHandleInput_SingleQuotes_MultipleFileArguments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
		why   string
	}{
		{
			name:  "two quoted arguments each preserve their internal spaces",
			input: "cat '/tmp/file name' '/tmp/file name with spaces'\n",
			want:  []string{"cat", "/tmp/file name", "/tmp/file name with spaces"},
			why:   "spec: single quotes preserve whitespace inside them, and unquoted whitespace between quoted strings still separates them into distinct arguments",
		},
		{
			name:  "three quoted arguments stay distinct",
			input: "echo 'a b' 'c d' 'e f'\n",
			want:  []string{"echo", "a b", "c d", "e f"},
			why:   "spec: each quoted string is its own argument regardless of how many appear on the line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := typed(tt.input)
			got, err := handleInput(bufio.NewReader(strings.NewReader(tt.input)))

			mustNoErr(t, call, err, tt.why)
			wantArgs(t, call, got, tt.want, tt.why)
		})
	}
}

// ============================================================
// handleEcho
// ============================================================

func TestHandleEcho_Valid(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
		why  string
	}{
		{
			name: "single word",
			args: []string{"echo", "hello"},
			want: "hello",
			why:  "the command name is dropped and the rest is printed",
		},
		{
			name: "several words are rejoined with single spaces",
			args: []string{"echo", "hello", "world"},
			want: "hello world",
			why:  "arguments are joined back together with one space each",
		},
		{
			name: "single quotes are stripped",
			args: []string{"echo", "'hello'", "'world'"},
			want: "hello world",
			why:  "quotes delimit an argument, they are not part of it",
		},
		{
			name: "mixed quoted and unquoted",
			args: []string{"echo", "'foo'", "bar"},
			want: "foo bar",
			why:  "quoting one argument does not change the others",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := cmdLine(tt.args)
			got, err := handleEcho(tt.args)

			mustNoErr(t, call, err, tt.why)
			wantEqual(t, call, got, tt.want, tt.why)
		})
	}
}

func TestHandleEcho_Edge(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
		why  string
	}{
		{
			name: "no words after echo",
			args: []string{"echo"},
			want: "",
			why:  "args[1:] on a one-element slice is empty, not a panic",
		},
		{
			name: "empty args slice",
			args: []string{},
			want: "",
			why:  "guarded; would otherwise panic on args[1:]",
		},
		{
			name: "quote inside a word is also stripped",
			args: []string{"echo", "it's"},
			want: "its",
			why:  "stripping is a blunt ReplaceAll, not real shell quote parsing",
		},
		{
			name: "a word of only quotes collapses to empty",
			args: []string{"echo", "''"},
			want: "",
			why:  "both quotes removed, nothing left",
		},
		{
			name: "double quotes are left alone",
			args: []string{"echo", `"hello"`},
			want: `"hello"`,
			why:  "only the single-quote character is stripped",
		},
		{
			name: "empty arguments are preserved as empty positions",
			args: []string{"echo", "a", "", "b"},
			want: "a  b",
			why:  "Join keeps the empty element, producing a double space",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := cmdLine(tt.args)
			got, err := handleEcho(tt.args)

			mustNoErr(t, call, err, tt.why)
			wantEqual(t, call, got, tt.want, tt.why)
		})
	}
}

// handleEcho has no failure mode: it cannot return a non-nil error for any
// input. Rather than fake a must-fail table, this pins that contract down.
func TestHandleEcho_NeverErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "nil args", args: nil},
		{name: "empty args", args: []string{}},
		{name: "command name only", args: []string{"echo"}},
		{name: "empty argument", args: []string{"echo", ""}},
		{name: "unbalanced quote", args: []string{"echo", "'"}},
		{name: "very long argument", args: []string{"echo", strings.Repeat("x", 10000)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := cmdLine(tt.args)
			_, err := handleEcho(tt.args)

			mustNoErr(t, call, err, "handleEcho must return a nil error for every input")
		})
	}
}

// ============================================================
// handlePWD
// ============================================================

func TestHandlePWD_Valid(t *testing.T) {
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory for the test: %v", err)
	}

	call := "pwd"
	got, err := handlePWD([]string{"pwd"})

	mustNoErr(t, call, err, "pwd on a live working directory cannot fail")
	wantEqual(t, call, got, want, "pwd reports the process working directory verbatim")
}

func TestHandlePWD_Edge_IgnoresArgs(t *testing.T) {
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory for the test: %v", err)
	}

	tests := []struct {
		name string
		args []string
		why  string
	}{
		{name: "nil args", args: nil, why: "args are never read, so nil is safe"},
		{name: "empty args", args: []string{}, why: "args are never read, so an empty slice is safe"},
		{name: "unexpected extra args", args: []string{"pwd", "ignored", "also-ignored"}, why: "args are ignored entirely"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := cmdLine(tt.args)
			got, err := handlePWD(tt.args)

			mustNoErr(t, call, err, tt.why)
			wantEqual(t, call, got, want, tt.why)
		})
	}
}

func TestHandlePWD_TracksDirectoryChanges(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory for the test: %v", err)
	}

	// TempDir first, so its removal cleanup is registered before the chdir
	// restore and therefore runs after it (cleanups are LIFO). Windows
	// cannot delete a directory that is still the process's cwd.
	tmp := t.TempDir()
	t.Cleanup(func() { os.Chdir(original) })

	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("setup: cannot chdir to %q: %v", tmp, err)
	}

	call := "pwd (after chdir to " + tmp + ")"
	got, err := handlePWD([]string{"pwd"})

	mustNoErr(t, call, err, "the directory exists, so Getwd succeeds")
	wantSameDir(t, call, got, tmp, "pwd reads the current directory on each call, it does not cache it")
}

// handlePWD has no portable failure mode: os.Getwd only fails in situations
// (deleted cwd, revoked permissions) that cannot be provoked reliably on
// Windows, so no must-fail table exists for it.

// ============================================================
// handleCD
//
// These subtests mutate the process working directory.
// Do not add t.Parallel anywhere in this section.
// ============================================================

func TestHandleCD_Valid(t *testing.T) {
	original := chdirGuard(t)

	t.Run("changes into an existing directory", func(t *testing.T) {
		tmp := t.TempDir()
		// Registered after TempDir so it runs before TempDir's removal.
		t.Cleanup(func() { os.Chdir(original) })

		call := "cd " + tmp
		err := handleCD([]string{"cd", tmp})

		mustNoErr(t, call, err, "the target directory exists")

		got, _ := os.Getwd()
		wantSameDir(t, "pwd", got, tmp, "a successful cd moves the process")
	})

	t.Run("tilde expands to HOME", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home) // set explicitly so the test does not depend on the machine
		t.Cleanup(func() { os.Chdir(original) })

		call := "cd ~ (with HOME=" + home + ")"
		err := handleCD([]string{"cd", "~"})

		mustNoErr(t, call, err, "HOME points at a real directory")

		got, _ := os.Getwd()
		wantSameDir(t, "pwd", got, home, "~ resolves to $HOME")
	})
}

func TestHandleCD_Edge(t *testing.T) {
	original := chdirGuard(t)

	t.Run("changing to the current directory is a no-op", func(t *testing.T) {
		call := "cd ."
		err := handleCD([]string{"cd", "."})

		mustNoErr(t, call, err, ". is always a valid directory")

		got, _ := os.Getwd()
		wantSameDir(t, "pwd", got, original, "cd . leaves the process where it was")
	})

	t.Run("extra arguments after the path are ignored", func(t *testing.T) {
		tmp := t.TempDir()
		t.Cleanup(func() { os.Chdir(original) })

		call := "cd " + tmp + " unexpected extra"
		err := handleCD([]string{"cd", tmp, "unexpected", "extra"})

		mustNoErr(t, call, err, "only args[1] is read, the extra words cannot make it fail")

		got, _ := os.Getwd()
		wantSameDir(t, "pwd", got, tmp, "only args[1] is read")
	})
}

func TestHandleCD_MustFail(t *testing.T) {
	original := chdirGuard(t)

	// A regular file, used to prove cd rejects non-directories.
	regularFile := filepath.Join(t.TempDir(), "not-a-directory.txt")
	if err := os.WriteFile(regularFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: cannot create test file: %v", err)
	}

	tests := []struct {
		name        string
		args        []string
		setEmptyEnv bool // when true, HOME is explicitly cleared
		wantContain string
		why         string
	}{
		{
			name:        "path does not exist",
			args:        []string{"cd", filepath.Join(string(filepath.Separator), "no", "such", "path", "xyz987abc")},
			wantContain: "No such",
			why:         "os.Chdir reports fs.ErrNotExist, which the function rewrites",
		},
		{
			name:        "path is a file, not a directory",
			args:        []string{"cd", regularFile},
			wantContain: "",
			why:         "os.Chdir refuses a non-directory; the exact message is OS-specific",
		},
		{
			name:        "no path argument at all",
			args:        []string{"cd"},
			wantContain: "missing operand",
			why:         "guarded; would otherwise panic indexing args[1]",
		},
		{
			name:        "tilde with HOME unset",
			args:        []string{"cd", "~"},
			setEmptyEnv: true,
			wantContain: "",
			why:         "HOME resolves to the empty string, which is not a valid directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A cd that unexpectedly succeeds must not leak into the next row,
			// which asserts the process has not moved.
			t.Cleanup(func() { os.Chdir(original) })

			if tt.setEmptyEnv {
				t.Setenv("HOME", "")
			}

			call := cmdLine(tt.args)
			err := handleCD(tt.args)

			mustErr(t, call, err, tt.why)
			if tt.wantContain != "" {
				wantErrContains(t, call, err, tt.wantContain, tt.why)
			}

			// A failed cd must not have moved the process.
			cwd, _ := os.Getwd()
			wantSameDir(t, "pwd", cwd, original, "a cd that fails must leave the process where it was")
		})
	}
}

// ============================================================
// handleTYPE
// ============================================================

func testBuiltins() map[string]string {
	return map[string]string{
		"echo": "print",
		"pwd":  "get working directory",
		"type": "get cmd type",
		"exit": "exiting",
		"cd":   "change directory",
	}
}

func TestHandleTYPE_Valid(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantContain string
		why         string
	}{
		{name: "builtin echo", args: []string{"type", "echo"}, wantContain: "echo is a shell builtin", why: "echo is in the builtin table"},
		{name: "builtin pwd", args: []string{"type", "pwd"}, wantContain: "pwd is a shell builtin", why: "pwd is in the builtin table"},
		{name: "builtin cd", args: []string{"type", "cd"}, wantContain: "cd is a shell builtin", why: "cd is in the builtin table"},
		{name: "builtin type", args: []string{"type", "type"}, wantContain: "type is a shell builtin", why: "type reports itself as a builtin"},
		{name: "builtin exit", args: []string{"type", "exit"}, wantContain: "exit is a shell builtin", why: "exit is in the builtin table"},
		{
			// "go" rather than "ls": present on PATH in every shell that can run go test.
			name:        "external command resolves to a path",
			args:        []string{"type", "go"},
			wantContain: "go is ",
			why:         "a non-builtin is looked up on PATH; the path itself is machine-specific",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := cmdLine(tt.args)
			got, err := handleTYPE(tt.args, testBuiltins())

			mustNoErr(t, call, err, tt.why)
			wantContains(t, call, got, tt.wantContain, tt.why)
		})
	}
}

func TestHandleTYPE_Edge(t *testing.T) {
	t.Run("nil builtin map falls through to PATH lookup", func(t *testing.T) {
		args := []string{"type", "go"}
		call := cmdLine(args) + " (with no builtin table)"

		got, err := handleTYPE(args, nil)

		mustNoErr(t, call, err, "reading a nil map is legal in Go")
		wantContains(t, call, got, "go is ", "with no builtin table every name falls through to PATH")
	})

	t.Run("builtin wins over a real binary of the same name", func(t *testing.T) {
		args := []string{"type", "go"}
		builtins := map[string]string{"go": "pretend builtin"}
		call := cmdLine(args) + " (with go registered as a builtin)"

		got, err := handleTYPE(args, builtins)

		mustNoErr(t, call, err, "a builtin hit never touches PATH")
		wantEqual(t, call, got, "go is a shell builtin", "the builtin table is consulted before PATH")
	})

	t.Run("empty command name is reported as not found", func(t *testing.T) {
		args := []string{"type", ""}
		call := cmdLine(args)

		got, err := handleTYPE(args, testBuiltins())

		mustNoErr(t, call, err, "an empty name is a miss, not an error")
		wantContains(t, call, got, "not found", "the empty string is in neither the builtin table nor PATH")
	})
}

// handleTYPE reports a missing command through its return STRING, not an
// error: the error stays nil. These assert that exact contract.
func TestHandleTYPE_MustFail(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
		why  string
	}{
		{
			name: "unknown command",
			args: []string{"type", "nosuchcmd12345"},
			want: "nosuchcmd12345: not found",
			why:  "exec.LookPath returns ErrNotFound, reported as a string with a nil error",
		},
		{
			name: "no command name given",
			args: []string{"type"},
			want: "No args provided",
			why:  "len(args) == 1 is handled before any lookup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := cmdLine(tt.args)
			got, err := handleTYPE(tt.args, testBuiltins())

			mustNoErr(t, call, err, tt.why)
			wantEqual(t, call, got, tt.want, tt.why)
		})
	}
}

// ============================================================
// handleExecFile
//
// Output of the launched command goes straight to os.Stdout, so these
// tests assert on the returned (message, error) pair only. Commands are
// chosen to be quiet: `go version` prints one line.
// ============================================================

func TestHandleExecFile_Valid(t *testing.T) {
	tests := []struct {
		name string
		args []string
		why  string
	}{
		{name: "command with an argument", args: []string{"go", "version"}, why: "go is on PATH and `go version` exits zero"},
		{name: "command with several arguments", args: []string{"go", "env", "GOOS"}, why: "every argument after the command name is forwarded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := cmdLine(tt.args)
			msg, err := handleExecFile(tt.args)

			mustNoErr(t, call, err, tt.why)
			wantEqual(t, call, msg, "", "a successful run returns an empty message; the output went to stdout")
		})
	}
}

func TestHandleExecFile_MustFail(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
		why     string
	}{
		{
			name:    "command is not on PATH",
			args:    []string{"nosuchcmd12345"},
			wantMsg: "nosuchcmd12345: command not found",
			why:     "LookPath fails before anything is executed",
		},
		{
			name:    "command exists but exits non-zero",
			args:    []string{"go", "definitely-not-a-subcommand"},
			wantMsg: "Error while executing file.",
			why:     "a distinct failure mode: the binary was found, running it failed",
		},
		{
			name:    "no command given",
			args:    []string{},
			wantMsg: "",
			why:     "guarded; would otherwise panic indexing args[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := cmdLine(tt.args)
			msg, err := handleExecFile(tt.args)

			mustErr(t, call, err, tt.why)
			wantEqual(t, call, msg, tt.wantMsg, tt.why)
		})
	}
}

// ============================================================
// helpers
//
// The assertion helpers these feed live in report_test.go.
// ============================================================

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// sameDir compares two directory paths after resolving symlinks, so that
// /var vs /private/var (macOS) and short vs long paths (Windows) match.
func sameDir(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return strings.EqualFold(ra, rb)
}

// chdirGuard records the working directory and restores it once the test
// finishes, so a cd test can never leak into another test.
func chdirGuard(t *testing.T) string {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory for the test: %v", err)
	}
	t.Cleanup(func() { os.Chdir(original) })
	return original
}
