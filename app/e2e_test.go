package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// End-to-end tests: the shell is compiled, commands are piped into its
// stdin, and its stdout is checked. Each test states the session it typed
// and the output it expected, so a failure reads as input / got / want.

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
	}{
		{
			name:    "prints its arguments",
			session: "echo hello world\n",
			want:    "hello world",
		},
		{
			name:    "strips single quotes",
			session: "echo 'hello' 'world'\n",
			want:    "hello world",
		},
		{
			name:    "echo with no arguments prints an empty line",
			session: "echo\n",
			want:    "$ \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContains(t, tt.session, got, tt.want)
		})
	}
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
	}{
		{
			name:    "non-existent directory",
			session: "cd /no/such/path/xyz987abc\n",
			want:    "No such",
		},
		{
			name:    "no path argument",
			session: "cd\n",
			want:    "missing operand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContains(t, tt.session, got, tt.want)
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

func assertContains(t *testing.T, session, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("shell session %q\n  got output:      %q\n  want containing: %q", session, got, want)
	}
}
