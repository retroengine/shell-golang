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
	}{
		{
			name:  "single command",
			input: "pwd\n",
			want:  []string{"pwd"},
		},
		{
			name:  "command with one argument",
			input: "cd /tmp\n",
			want:  []string{"cd", "/tmp"},
		},
		{
			name:  "command with several arguments",
			input: "echo hello world\n",
			want:  []string{"echo", "hello", "world"},
		},
		{
			name:  "surrounding whitespace is trimmed",
			input: "   pwd   \n",
			want:  []string{"pwd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handleInput(bufio.NewReader(strings.NewReader(tt.input)))

			if err != nil {
				t.Fatalf("handleInput(%q)\n  got err: %v\n  want err: <nil>", tt.input, err)
			}
			if !equalArgs(got, tt.want) {
				t.Errorf("handleInput(%q)\n  got:  %#v\n  want: %#v", tt.input, got, tt.want)
			}
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
			name:  "repeated spaces produce empty arguments",
			input: "echo   hello\n",
			want:  []string{"echo", "", "", "hello"},
			why:   "strings.Split does not collapse runs of spaces the way a real shell does",
		},
		{
			name:  "empty line yields one empty argument",
			input: "\n",
			want:  []string{""},
			why:   "an empty line is not an error; it splits into a single empty string",
		},
		{
			name:  "whitespace-only line yields one empty argument",
			input: "    \n",
			want:  []string{""},
			why:   "trimmed to empty, then split",
		},
		{
			name:  "tab is not treated as a separator",
			input: "echo\thello\n",
			want:  []string{"echo\thello"},
			why:   "only the space character separates arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handleInput(bufio.NewReader(strings.NewReader(tt.input)))

			if err != nil {
				t.Fatalf("handleInput(%q)\n  got err: %v\n  want err: <nil>\n  note: %s", tt.input, err, tt.why)
			}
			if !equalArgs(got, tt.want) {
				t.Errorf("handleInput(%q)\n  got:  %#v\n  want: %#v\n  note: %s", tt.input, got, tt.want, tt.why)
			}
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
			got, err := handleInput(bufio.NewReader(strings.NewReader(tt.input)))

			if err == nil {
				t.Fatalf("handleInput(%q)\n  got:  %#v, err = <nil>\n  want: an error\n  note: %s", tt.input, got, tt.why)
			}
			if got != nil {
				t.Errorf("handleInput(%q)\n  got:  %#v\n  want: nil args alongside the error", tt.input, got)
			}
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
	}{
		{
			name: "single word",
			args: []string{"echo", "hello"},
			want: "hello",
		},
		{
			name: "several words are rejoined with single spaces",
			args: []string{"echo", "hello", "world"},
			want: "hello world",
		},
		{
			name: "single quotes are stripped",
			args: []string{"echo", "'hello'", "'world'"},
			want: "hello world",
		},
		{
			name: "mixed quoted and unquoted",
			args: []string{"echo", "'foo'", "bar"},
			want: "foo bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handleEcho(tt.args)

			if err != nil {
				t.Fatalf("handleEcho(%#v)\n  got err: %v\n  want err: <nil>", tt.args, err)
			}
			if got != tt.want {
				t.Errorf("handleEcho(%#v)\n  got:  %q\n  want: %q", tt.args, got, tt.want)
			}
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
			got, err := handleEcho(tt.args)

			if err != nil {
				t.Fatalf("handleEcho(%#v)\n  got err: %v\n  want err: <nil>\n  note: %s", tt.args, err, tt.why)
			}
			if got != tt.want {
				t.Errorf("handleEcho(%#v)\n  got:  %q\n  want: %q\n  note: %s", tt.args, got, tt.want, tt.why)
			}
		})
	}
}

// handleEcho has no failure mode: it cannot return a non-nil error for any
// input. Rather than fake a must-fail table, this pins that contract down.
func TestHandleEcho_NeverErrors(t *testing.T) {
	inputs := [][]string{
		nil,
		{},
		{"echo"},
		{"echo", ""},
		{"echo", "'"},
		{"echo", strings.Repeat("x", 10000)},
	}

	for _, args := range inputs {
		if _, err := handleEcho(args); err != nil {
			t.Errorf("handleEcho(%#v)\n  got err: %v\n  want err: <nil> for every input", args, err)
		}
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

	got, err := handlePWD([]string{"pwd"})
	if err != nil {
		t.Fatalf("handlePWD([pwd])\n  got err: %v\n  want err: <nil>", err)
	}
	if got != want {
		t.Errorf("handlePWD([pwd])\n  got:  %q\n  want: %q", got, want)
	}
}

func TestHandlePWD_Edge_IgnoresArgs(t *testing.T) {
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory for the test: %v", err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "nil args", args: nil},
		{name: "empty args", args: []string{}},
		{name: "unexpected extra args", args: []string{"pwd", "ignored", "also-ignored"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handlePWD(tt.args)

			if err != nil {
				t.Fatalf("handlePWD(%#v)\n  got err: %v\n  want err: <nil>", tt.args, err)
			}
			if got != want {
				t.Errorf("handlePWD(%#v)\n  got:  %q\n  want: %q\n  note: args are ignored entirely", tt.args, got, want)
			}
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

	got, err := handlePWD([]string{"pwd"})
	if err != nil {
		t.Fatalf("handlePWD([pwd]) after chdir\n  got err: %v\n  want err: <nil>", err)
	}
	// macOS reports /private/var for /var, so resolve both sides before comparing.
	want, _ := filepath.EvalSymlinks(tmp)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != want {
		t.Errorf("handlePWD([pwd]) after chdir to %q\n  got:  %q\n  want: %q", tmp, gotResolved, want)
	}
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

		if err := handleCD([]string{"cd", tmp}); err != nil {
			t.Fatalf("handleCD([cd %s])\n  got err: %v\n  want err: <nil>", tmp, err)
		}

		got, _ := os.Getwd()
		if !sameDir(got, tmp) {
			t.Errorf("handleCD([cd %s])\n  got cwd:  %q\n  want cwd: %q", tmp, got, tmp)
		}
	})

	t.Run("tilde expands to HOME", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home) // set explicitly so the test does not depend on the machine
		t.Cleanup(func() { os.Chdir(original) })

		if err := handleCD([]string{"cd", "~"}); err != nil {
			t.Fatalf("handleCD([cd ~]) with HOME=%s\n  got err: %v\n  want err: <nil>", home, err)
		}

		got, _ := os.Getwd()
		if !sameDir(got, home) {
			t.Errorf("handleCD([cd ~]) with HOME=%s\n  got cwd:  %q\n  want cwd: %q", home, got, home)
		}
	})
}

func TestHandleCD_Edge(t *testing.T) {
	original := chdirGuard(t)

	t.Run("changing to the current directory is a no-op", func(t *testing.T) {
		if err := handleCD([]string{"cd", "."}); err != nil {
			t.Fatalf("handleCD([cd .])\n  got err: %v\n  want err: <nil>", err)
		}

		got, _ := os.Getwd()
		if !sameDir(got, original) {
			t.Errorf("handleCD([cd .])\n  got cwd:  %q\n  want cwd: %q (unchanged)", got, original)
		}
	})

	t.Run("extra arguments after the path are ignored", func(t *testing.T) {
		tmp := t.TempDir()
		t.Cleanup(func() { os.Chdir(original) })

		if err := handleCD([]string{"cd", tmp, "unexpected", "extra"}); err != nil {
			t.Fatalf("handleCD([cd %s unexpected extra])\n  got err: %v\n  want err: <nil>", tmp, err)
		}

		got, _ := os.Getwd()
		if !sameDir(got, tmp) {
			t.Errorf("handleCD([cd %s unexpected extra])\n  got cwd:  %q\n  want cwd: %q\n  note: only args[1] is read", tmp, got, tmp)
		}
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
		home        string // when non-empty, HOME is set to this for the subtest
		setEmptyEnv bool   // when true, HOME is explicitly cleared
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
			if tt.setEmptyEnv {
				t.Setenv("HOME", "")
			}

			err := handleCD(tt.args)

			if err == nil {
				cwd, _ := os.Getwd()
				os.Chdir(original)
				t.Fatalf("handleCD(%#v)\n  got err: <nil> (cwd is now %q)\n  want err: an error\n  note: %s", tt.args, cwd, tt.why)
			}
			if tt.wantContain != "" && !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("handleCD(%#v)\n  got err:  %q\n  want err containing: %q\n  note: %s", tt.args, err.Error(), tt.wantContain, tt.why)
			}

			// A failed cd must not have moved the process.
			cwd, _ := os.Getwd()
			if !sameDir(cwd, original) {
				t.Errorf("handleCD(%#v) failed but still changed directory\n  got cwd:  %q\n  want cwd: %q", tt.args, cwd, original)
				os.Chdir(original)
			}
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
	}{
		{name: "builtin echo", args: []string{"type", "echo"}, wantContain: "echo is a shell builtin"},
		{name: "builtin pwd", args: []string{"type", "pwd"}, wantContain: "pwd is a shell builtin"},
		{name: "builtin cd", args: []string{"type", "cd"}, wantContain: "cd is a shell builtin"},
		{name: "builtin type", args: []string{"type", "type"}, wantContain: "type is a shell builtin"},
		{name: "builtin exit", args: []string{"type", "exit"}, wantContain: "exit is a shell builtin"},
		{
			// "go" rather than "ls": present on PATH in every shell that can run go test.
			name:        "external command resolves to a path",
			args:        []string{"type", "go"},
			wantContain: "go is ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handleTYPE(tt.args, testBuiltins())

			if err != nil {
				t.Fatalf("handleTYPE(%#v)\n  got err: %v\n  want err: <nil>", tt.args, err)
			}
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("handleTYPE(%#v)\n  got:  %q\n  want containing: %q", tt.args, got, tt.wantContain)
			}
		})
	}
}

func TestHandleTYPE_Edge(t *testing.T) {
	t.Run("nil builtin map falls through to PATH lookup", func(t *testing.T) {
		args := []string{"type", "go"}

		got, err := handleTYPE(args, nil)

		if err != nil {
			t.Fatalf("handleTYPE(%#v, nil)\n  got err: %v\n  want err: <nil>\n  note: reading a nil map is legal in Go", args, err)
		}
		if !strings.Contains(got, "go is ") {
			t.Errorf("handleTYPE(%#v, nil)\n  got:  %q\n  want containing: %q", args, got, "go is ")
		}
	})

	t.Run("builtin wins over a real binary of the same name", func(t *testing.T) {
		args := []string{"type", "go"}
		builtins := map[string]string{"go": "pretend builtin"}

		got, err := handleTYPE(args, builtins)

		if err != nil {
			t.Fatalf("handleTYPE(%#v)\n  got err: %v\n  want err: <nil>", args, err)
		}
		want := "go is a shell builtin"
		if got != want {
			t.Errorf("handleTYPE(%#v) with go registered as a builtin\n  got:  %q\n  want: %q\n  note: the builtin table is consulted before PATH", args, got, want)
		}
	})

	t.Run("empty command name is reported as not found", func(t *testing.T) {
		args := []string{"type", ""}

		got, err := handleTYPE(args, testBuiltins())

		if err != nil {
			t.Fatalf("handleTYPE(%#v)\n  got err: %v\n  want err: <nil>", args, err)
		}
		if !strings.Contains(got, "not found") {
			t.Errorf("handleTYPE(%#v)\n  got:  %q\n  want containing: %q", args, got, "not found")
		}
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
			got, err := handleTYPE(tt.args, testBuiltins())

			if err != nil {
				t.Fatalf("handleTYPE(%#v)\n  got err: %v\n  want err: <nil>\n  note: %s", tt.args, err, tt.why)
			}
			if got != tt.want {
				t.Errorf("handleTYPE(%#v)\n  got:  %q\n  want: %q\n  note: %s", tt.args, got, tt.want, tt.why)
			}
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
	}{
		{name: "command with an argument", args: []string{"go", "version"}},
		{name: "command with several arguments", args: []string{"go", "env", "GOOS"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := handleExecFile(tt.args)

			if err != nil {
				t.Fatalf("handleExecFile(%#v)\n  got err: %v\n  want err: <nil>", tt.args, err)
			}
			if msg != "" {
				t.Errorf("handleExecFile(%#v)\n  got msg:  %q\n  want msg: %q on success", tt.args, msg, "")
			}
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
			msg, err := handleExecFile(tt.args)

			if err == nil {
				t.Fatalf("handleExecFile(%#v)\n  got:  msg = %q, err = <nil>\n  want: an error\n  note: %s", tt.args, msg, tt.why)
			}
			if msg != tt.wantMsg {
				t.Errorf("handleExecFile(%#v)\n  got msg:  %q\n  want msg: %q\n  got err:  %v\n  note: %s", tt.args, msg, tt.wantMsg, err, tt.why)
			}
		})
	}
}

// ============================================================
// helpers
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
