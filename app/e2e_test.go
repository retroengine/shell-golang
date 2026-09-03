package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// End-to-end tests: the shell is compiled, commands are piped into its
// stdin, and its stdout is checked. Every case reports through
// assertContains / assertContainsWhy, which route to the shared reporter in
// report_test.go, so each one prints the session that was typed, what was
// expected and what came back — whether it passed or failed.

// buildTestBinary compiles the shell into a temp directory that Go removes
// when the test finishes.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	binary := t.TempDir() + "/shell"
	if runtime.GOOS == "windows" {
		// Windows cannot exec a file without a recognised extension, even
		// when given its full path.
		binary += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("could not build the shell under test: %v\n%s", err, out)
	}
	return binary
}

// runShell feeds session into the shell's stdin and returns everything it
// wrote to stdout. The shell has no working exit path — it panics on EOF
// once stdin runs dry — so a non-zero exit is expected and ignored; the
// timeout is the backstop in case it ever blocks instead.
func runShell(t *testing.T, binary, session string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary)
	cmd.Stdin = strings.NewReader(session)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil // discard the EOF panic trace

	cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("shell did not exit within 5s\n  session: %q\n  output so far: %q", session, stdout.String())
	}
	return stdout.String()
}

// ============================================================
// echo
// ============================================================

func TestE2E_Echo(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "prints its arguments",
			session: "echo hello world\n",
			want:    "hello world",
			why:     "echo writes its arguments back out, separated by single spaces",
		},
		{
			name:    "strips single quotes",
			session: "echo 'hello' 'world'\n",
			want:    "hello world",
			why:     "quotes delimit an argument, they are not part of it",
		},
		{
			name:    "echo with no arguments prints an empty line",
			session: "echo\n",
			want:    "$ \n",
			why:     "with nothing to print the prompt is followed straight by a newline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContainsWhy(t, tt.session, got, tt.want, tt.why)
		})
	}
}

// ============================================================
// single quotes
// ============================================================

func TestE2E_SingleQuotes(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "spaces preserved within quotes",
			session: "echo 'hello    world'\n",
			want:    "hello    world",
			why:     "spec: spaces are preserved within quotes",
		},
		{
			name:    "consecutive unquoted spaces collapse",
			session: "echo hello    world\n",
			want:    "hello world",
			why:     "spec: consecutive spaces are collapsed unless quoted",
		},
		{
			name:    "adjacent quoted strings concatenate",
			session: "echo 'hello''world'\n",
			want:    "helloworld",
			why:     "spec: adjacent quoted strings are concatenated",
		},
		{
			name:    "empty quotes are ignored",
			session: "echo hello''world\n",
			want:    "helloworld",
			why:     "spec: empty quotes '' are ignored",
		},
		{
			name:    "special characters lose meaning inside quotes",
			session: "echo '$HOME'\n",
			want:    "$HOME",
			why:     "spec: characters inside single quotes, including $, lose their special meaning and are treated literally",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContainsWhy(t, tt.session, got, tt.want, tt.why)
		})
	}
}

// ============================================================
// single quotes — arguments to external commands
// ============================================================

func TestE2E_SingleQuotes_ExternalCommand(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := t.TempDir()

	file1 := filepath.Join(dir, "file name")
	file2 := filepath.Join(dir, "file name with spaces")

	if err := os.WriteFile(file1, []byte("content1 "), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file1, err)
	}
	if err := os.WriteFile(file2, []byte("content2"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file2, err)
	}

	session := fmt.Sprintf("cat '%s' '%s'\n", file1, file2)
	want := "content1 content2"
	why := "spec: quoted filenames are passed to external commands as separate arguments, with the spaces inside each name preserved"

	got := runShell(t, binary, session)
	assertContainsWhy(t, session, got, want, why)
}

// ============================================================
// pwd and cd
// ============================================================

func TestE2E_PWD(t *testing.T) {
	binary := buildTestBinary(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory for the test: %v", err)
	}

	session := "pwd\n"
	got := runShell(t, binary, session)
	assertContains(t, session, got, cwd)
}

func TestE2E_CD_ThenPWD(t *testing.T) {
	binary := buildTestBinary(t)
	tmp := t.TempDir()

	session := fmt.Sprintf("cd %s\npwd\n", tmp)
	got := runShell(t, binary, session)
	assertContains(t, session, got, tmp)
}

func TestE2E_CD_MustFail(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "non-existent directory",
			session: "cd /no/such/path/xyz987abc\n",
			want:    "No such",
			why:     "a missing target is reported, not silently ignored",
		},
		{
			name:    "no path argument",
			session: "cd\n",
			want:    "missing operand",
			why:     "cd with no operand is an error the shell must report",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContainsWhy(t, tt.session, got, tt.want, tt.why)
		})
	}
}

// A failed cd must leave the shell where it was.
func TestE2E_CD_FailureDoesNotMoveShell(t *testing.T) {
	binary := buildTestBinary(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory for the test: %v", err)
	}

	session := "cd /no/such/path/xyz987abc\npwd\n"
	got := runShell(t, binary, session)
	assertContains(t, session, got, cwd)
}

// ============================================================
// type
// ============================================================

func TestE2E_Type_Builtins(t *testing.T) {
	binary := buildTestBinary(t)

	for _, builtin := range []string{"echo", "pwd", "cd", "type", "exit"} {
		t.Run(builtin, func(t *testing.T) {
			session := fmt.Sprintf("type %s\n", builtin)
			got := runShell(t, binary, session)
			assertContains(t, session, got, builtin+" is a shell builtin")
		})
	}
}

func TestE2E_Type_ExternalCommand(t *testing.T) {
	binary := buildTestBinary(t)

	// "go" rather than "ls": on PATH in every shell that can run go test.
	session := "type go\n"
	got := runShell(t, binary, session)
	assertContains(t, session, got, "go is ")
}

func TestE2E_Type_MustFail(t *testing.T) {
	binary := buildTestBinary(t)

	session := "type nosuchcmd12345\n"
	got := runShell(t, binary, session)
	assertContains(t, session, got, "nosuchcmd12345: not found")
}

// ============================================================
// running external programs
// ============================================================

func TestE2E_RunsExternalCommand(t *testing.T) {
	binary := buildTestBinary(t)

	session := "go version\n"
	got := runShell(t, binary, session)
	assertContains(t, session, got, "go version")
}

func TestE2E_UnknownCommand_MustFail(t *testing.T) {
	binary := buildTestBinary(t)

	session := "nosuchcmd12345\n"
	got := runShell(t, binary, session)
	assertContains(t, session, got, "not found")
}

// ============================================================
// the session loop itself
// ============================================================

func TestE2E_MultipleCommandsInOneSession(t *testing.T) {
	binary := buildTestBinary(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine working directory for the test: %v", err)
	}

	session := "echo first\npwd\necho second\n"
	got := runShell(t, binary, session)

	for _, want := range []string{"first", cwd, "second"} {
		assertContains(t, session, got, want)
	}
}

func TestE2E_PromptIsPrinted(t *testing.T) {
	binary := buildTestBinary(t)

	session := "echo hi\n"
	got := runShell(t, binary, session)
	assertContains(t, session, got, "$ ")
}

// ============================================================
// helpers
// ============================================================

// assertContains reports through the shared reporter in report_test.go, so a
// shell session prints the same expected/received block as a unit test.
func assertContains(t *testing.T, typedIn, got, want string) {
	t.Helper()
	assertContainsWhy(t, typedIn, got, want, "")
}

// assertContainsWhy is assertContains with a spec rule to quote when the case
// fails. Prefer it wherever the table has a why to give.
func assertContainsWhy(t *testing.T, typedIn, got, want, why string) {
	t.Helper()
	wantContains(t, typedSession(typedIn), got, want, why)
}
