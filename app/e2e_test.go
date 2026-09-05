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
// double quotes
// ============================================================

func TestE2E_DoubleQuotes(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "spaces preserved within double quotes",
			session: "echo \"hello    world\"\n",
			want:    "hello    world",
			why:     "spec: consecutive whitespaces (spaces, tabs) must be preserved inside double quotes",
		},
		{
			name:    "adjacent double-quoted strings concatenate",
			session: "echo \"hello\"\"world\"\n",
			want:    "helloworld",
			why:     "spec: double-quoted strings placed next to each other concatenate into one argument",
		},
		{
			name:    "quoted and unquoted text concatenate",
			session: "echo \"hello\"world\n",
			want:    "helloworld",
			why:     "spec: quoted and unquoted strings next to each other also concatenate",
		},
		{
			name:    "separate double-quoted arguments stay distinct",
			session: "echo \"hello\" \"world\"\n",
			want:    "hello world",
			why:     "spec: double-quoted strings are separate arguments unless directly adjacent, so echo joins them back with a single space",
		},
		{
			name:    "single quotes inside double quotes are literal",
			session: "echo \"shell's test\"\n",
			want:    "shell's test",
			why:     "spec: characters lose their special meaning inside double quotes, so the embedded single quote is literal text, not a delimiter",
		},
		{
			name:    "tester case: internal whitespace preserved, quoted args stay distinct",
			session: "echo \"quz  hello\"  \"bar\"\n",
			want:    "quz  hello bar",
			why:     "tester case: internal double-quoted whitespace is preserved and separate quoted arguments remain distinct, joined by echo with one space",
		},
		{
			name:    "tester case: three quoted args, literal apostrophe in one",
			session: "echo \"bar\"  \"shell's\"  \"foo\"\n",
			want:    "bar shell's foo",
			why:     "tester case: three separate double-quoted arguments stay distinct, and the single quote inside one of them is literal, not a delimiter",
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
// double quotes — arguments to external commands
// ============================================================

func TestE2E_DoubleQuotes_ExternalCommand(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := t.TempDir()

	file1 := filepath.Join(dir, "file name")
	file2 := filepath.Join(dir, "'file name' with spaces")

	if err := os.WriteFile(file1, []byte("content1 "), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file1, err)
	}
	if err := os.WriteFile(file2, []byte("content2"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file2, err)
	}

	session := fmt.Sprintf("cat \"%s\" \"%s\"\n", file1, file2)
	want := "content1 content2"
	why := "spec: double-quoted filenames are passed to external commands as separate arguments, with spaces and literal single quotes inside each name preserved"

	got := runShell(t, binary, session)
	assertContainsWhy(t, session, got, want, why)
}

// ============================================================
// backslash inside double quotes
// ============================================================

func TestE2E_BackslashInDoubleQuotes(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "double backslash inside double quotes collapses to one backslash",
			session: "echo \"A \\\\\\\\ escapes itself\"\n",
			want:    "A \\\\ escapes itself",
			why:     "spec: \\\\\\\\ inside double quotes produces a single literal backslash",
		},
		{
			name:    "backslash-quote inside double quotes produces a literal double quote",
			session: "echo \"A \\\" inside double quotes\"\n",
			want:    "A \" inside double quotes",
			why:     "spec: \\\" inside double quotes escapes the double quote, yielding a literal \"",
		},
		{
			name:    "tester case: mixed single quotes and double-escaped backslash",
			session: "echo \"just'one'\\\\n'backslash\"\n",
			want:    "just'one'\\n'backslash",
			why:     "spec: \\\\\\\\ inside double quotes collapses to \\\\; single quotes inside double quotes are literal",
		},
		{
			name:    "tester case: escaped quote mid-string transitions out of double-quote mode",
			session: "echo \"inside\\\"literal_quote.\"outside\\\"\n",
			want:    "inside\"literal_quote.outside\"",
			why:     "spec: \\\" yields literal \"; the closing unescaped \" ends the quoted span; outside text concatenates; the final \\\" outside adds a literal quote",
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
// backslash inside double quotes — arguments to external commands
// ============================================================

func TestE2E_BackslashInDoubleQuotes_ExternalCommand(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	// The two escapes double quotes recognize (\" and \\) both target
	// characters (" and \) that are illegal inside a Windows filename
	// component, so a real file exercising either escape cannot be created
	// portably here. That escaping is covered instead by
	// TestHandleInput_BackslashInDoubleQuotes_MultipleFileArguments, which
	// asserts on the parsed argument directly without touching the
	// filesystem. This test only proves a double-quoted filename with a
	// plain space reaches an external command unharmed.
	file1 := dir + "/number 1"

	if err := os.WriteFile(file1, []byte("content1"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file1, err)
	}

	session := fmt.Sprintf("cat \"%s\"\n", file1)
	want := "content1"
	why := "spec: double quotes preserve the space in the filename, so it reaches cat as one argument"

	got := runShell(t, binary, session)
	assertContainsWhy(t, session, got, want, why)
}

// ============================================================
// handleInput — backslash outside quotes
// ============================================================

func TestE2E_Backslash(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "each escaped space is a literal space in one argument",
			session: `echo three\ \ \ spaces` + "\n",
			want:    "three" + strings.Repeat(" ", 3) + "spaces",
			why:     "spec: each \\  creates a literal space as part of one argument",
		},
		{
			name:    "the first space survives escaped, later spaces collapse",
			session: "echo before\\" + strings.Repeat(" ", 5) + "after\n",
			want:    "before" + strings.Repeat(" ", 2) + "after",
			why:     "spec: the backslash preserves the first space literally, but the shell collapses the subsequent unescaped spaces",
		},
		{
			name:    "escaping a regular letter just drops the backslash",
			session: `echo test\nexample` + "\n",
			want:    "testnexample",
			why:     "spec: \\n becomes just n",
		},
		{
			name:    "a backslash can escape a backslash",
			session: `echo hello\\world` + "\n",
			want:    `hello\world`,
			why:     "spec: the first backslash escapes the second, and the result is a single literal backslash",
		},
		{
			name:    "escaping makes single quotes literal characters",
			session: `echo \'hello\'` + "\n",
			want:    "'hello'",
			why:     "spec: \\' makes the single quotes literal characters",
		},
		{
			name:    "tester case: four escaped spaces",
			session: `echo multiple\ \ \ \ spaces` + "\n",
			want:    "multiple" + strings.Repeat(" ", 4) + "spaces",
			why:     "tester case: escaped spaces stay literal inside one argument",
		},
		{
			name:    "tester case: escaped quote characters print literally",
			session: `echo \'\"literal quotes\"\'` + "\n",
			want:    `'"literal quotes"'`,
			why:     "tester case: backslash strips the special meaning from both quote characters, and echo rejoins the resulting words with a single space",
		},
		{
			name:    "tester case: escaping a character with no special meaning",
			session: `echo ignore\_backslash` + "\n",
			want:    "ignore_backslash",
			why:     "tester case: escaping works for characters without special meaning too, the backslash is simply removed",
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
// backslash outside quotes — arguments to external commands
// ============================================================

func TestE2E_Backslash_ExternalCommand(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	file1 := dir + "/_ignored_1"
	file2 := dir + "/ignore_2"

	if err := os.WriteFile(file1, []byte("content1 "), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file1, err)
	}
	if err := os.WriteFile(file2, []byte("content2"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file2, err)
	}

	// Backslashes sit only immediately before the character each one
	// escapes, and the path itself uses forward slashes, so the escaping
	// never touches a path separator. The tester's third filename needs a
	// literal backslash character in the name, which can't be created
	// portably here; that filename's escaping is covered by
	// TestHandleInput_Backslash_MultipleFileArguments instead.
	session := fmt.Sprintf("cat %s/\\_ignored_1 %s/ignore_\\2\n", dir, dir)
	want := "content1 content2"
	why := "spec: a backslash outside quotes escapes only the single next character, leaving the rest of the path untouched"

	got := runShell(t, binary, session)
	assertContainsWhy(t, session, got, want, why)
}

// ============================================================
// backslash inside single quotes
// ============================================================

func TestE2E_BackslashInSingleQuotes(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "double backslashes inside single quotes are literal",
			session: "echo 'multiple\\\\slashes'\n",
			want:    "multiple\\\\slashes",
			why:     "spec: backslashes have no escaping behavior inside single quotes, every character is literal",
		},
		{
			name:    "backslash-quote sequences inside single quotes are literal",
			session: "echo 'every\\\"thing_is\\\"literal'\n",
			want:    "every\\\"thing_is\\\"literal",
			why:     "spec: backslashes inside single quotes do not escape double quotes, they are literal text",
		},
		{
			name:    "backslash-n inside single quotes is literal",
			session: "echo 'shell\\\\\\nscript'\n",
			want:    "shell\\\\\\nscript",
			why:     "spec: backslashes have no special escaping behavior inside single quotes, so \\\\\\n remains verbatim",
		},
		{
			name:    "backslash-double-quote inside single quotes is literal",
			session: "echo 'example\\\"test'\n",
			want:    "example\\\"test",
			why:     "spec: a backslash followed by a double quote inside single quotes is literal text",
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
// backslash inside single quotes — arguments to external commands
// ============================================================

func TestE2E_BackslashInSingleQuotes_ExternalCommand(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	// A backslash inside single quotes is passed through completely
	// literally, so exercising it here would require a real filename
	// containing a \ character, which is illegal inside a Windows filename
	// component. That case is covered instead by
	// TestHandleInput_BackslashInSingleQuotes_MultipleFileArguments, which
	// asserts on the parsed argument directly without touching the
	// filesystem. This test only proves a single-quoted filename with a
	// plain space reaches an external command unharmed.
	file1 := dir + "/no slash 1"

	if err := os.WriteFile(file1, []byte("content1"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file1, err)
	}

	session := fmt.Sprintf("cat '%s'\n", file1)
	want := "content1"
	why := "spec: single quotes preserve the space in the filename, so it reaches cat as one argument"

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

	session := fmt.Sprintf("cd '%s'\npwd\n", filepath.ToSlash(tmp))
	got := runShell(t, binary, session)
	// pwd reports the OS-native path, so assert against tmp (native
	// separators), not the forward-slash form used to type the session.
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
// quoted executable names
// ============================================================

func TestE2E_QuotedExecutable(t *testing.T) {
	catPath, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	// A double-quote character is illegal inside a Windows filename
	// component, so a real executable named `exe with "quotes"` cannot be
	// created portably here. That case (single-quoted name with an
	// embedded double quote) is covered instead by
	// TestHandleInput_QuotedExecutable_Valid, which asserts on the parsed
	// argument directly without touching the filesystem. This test copies
	// only the double-quoted-name-with-embedded-single-quotes variant,
	// since a single quote is a legal Windows filename character.
	exeName := `exe with 'single quotes'`

	copyExe := func(dst string) {
		// Windows will only resolve an extensionless name on PATH via
		// PATHEXT if the file on disk actually carries one of those
		// extensions, so the copy needs a real .exe suffix even though the
		// shell session below invokes it without one.
		if runtime.GOOS == "windows" {
			dst += ".exe"
		}
		data, err := os.ReadFile(catPath)
		if err != nil {
			t.Fatalf("setup: cannot read cat binary: %v", err)
		}
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			t.Fatalf("setup: cannot write %q: %v", dst, err)
		}
	}

	copyExe(dir + "/" + exeName)

	file := dir + "/file.txt"
	if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file, err)
	}

	// Prepend the temp dir to PATH so the shell can find the renamed binary.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "double-quoted executable name with embedded single quotes",
			session: fmt.Sprintf("\"exe with 'single quotes'\" %s\n", file),
			want:    "content",
			why:     "spec: double quotes strip the quotes and yield the literal name exe with 'single quotes', which is found on PATH and executed",
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
// stdout redirection (> and 1>)
// ============================================================

func TestE2E_StdoutRedirection(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "> creates the file if it does not exist and writes stdout into it",
			session: fmt.Sprintf("echo hello > '%s/output.txt'\ncat '%s/output.txt'\n", dir, dir),
			want:    "hello",
			why:     "spec: if the file doesn't exist, it is created; the output that would normally appear on the terminal is written to it instead",
		},
		{
			name:    "1> behaves identically to >",
			session: fmt.Sprintf("echo Hello James 1> '%s/foo.md'\ncat '%s/foo.md'\n", dir, dir),
			want:    "Hello James",
			why:     "spec: 1 is the file descriptor for standard output, so 1> and > do exactly the same thing",
		},
		{
			name:    "redirected output from an external command",
			session: fmt.Sprintf("go version > '%s/version.txt'\ncat '%s/version.txt'\n", dir, dir),
			want:    "go version",
			why:     "spec: > redirects the standard output of a command to a file, whether the command is a builtin or an external program",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContainsWhy(t, tt.session, got, tt.want, tt.why)
		})
	}
}

func TestE2E_StdoutRedirection_Overwrites(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())
	file := dir + "/existing.txt"

	if err := os.WriteFile(filepath.FromSlash(file), []byte("old contents"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file, err)
	}

	session := fmt.Sprintf("echo new contents > '%s'\ncat '%s'\n", file, file)
	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "new contents",
		"spec: if the file already exists, it is overwritten, replacing its old contents")

	data, err := os.ReadFile(filepath.FromSlash(file))
	if err != nil {
		t.Fatalf("setup: cannot read back %q: %v", file, err)
	}
	wantEqual(t, typedSession(session), strings.TrimRight(string(data), "\r\n"), "new contents",
		"spec: the file's old contents are replaced, not appended to")
}

func TestE2E_StdoutRedirection_ErrorNotRedirected(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	existing := dir + "/blueberry"
	if err := os.WriteFile(filepath.FromSlash(existing), []byte("blueberry"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", existing, err)
	}

	outFile := dir + "/quz.md"
	session := fmt.Sprintf("cat '%s' nonexistent 1> '%s'\ncat '%s'\n", existing, outFile, outFile)
	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "blueberry",
		"spec: the non-error output from cat still reaches the redirected file")

	data, err := os.ReadFile(filepath.FromSlash(outFile))
	if err != nil {
		t.Fatalf("setup: cannot read back %q: %v", outFile, err)
	}
	wantEqual(t, typedSession(session), strings.TrimRight(string(data), "\r\n"), "blueberry",
		"spec: error messages are not written to the redirected file, only the command's standard output is")
}

// ============================================================
// stdout append (>> and 1>>)
// ============================================================

func TestE2E_StdoutAppend(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    ">> creates the file if it does not exist and writes stdout into it",
			session: fmt.Sprintf("echo first >> '%s/created.txt'\ncat '%s/created.txt'\n", dir, dir),
			want:    "first",
			why:     "spec: if the file doesn't exist, it is created, just like >",
		},
		{
			name:    "1>> behaves identically to >>",
			session: fmt.Sprintf("echo Hello Emily 1>> '%s/onegt.txt'\necho Hello Maria 1>> '%s/onegt.txt'\ncat '%s/onegt.txt'\n", dir, dir, dir),
			want:    "Hello Emily\nHello Maria",
			why:     "spec: 1>> and >> do exactly the same thing",
		},
		{
			name:    "appended output from an external command follows what > wrote before it",
			session: fmt.Sprintf("echo List of files: > '%s/mixed.txt'\ngo version >> '%s/mixed.txt'\ncat '%s/mixed.txt'\n", dir, dir, dir),
			want:    "List of files:\ngo version",
			why:     "spec: >> redirects the standard output of a command to a file, whether the command is a builtin or an external program, without disturbing what was already there",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContainsWhy(t, tt.session, got, tt.want, tt.why)
		})
	}
}

func TestE2E_StdoutAppend_PreservesExistingContent(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())
	file := dir + "/existing.txt"

	// A trailing newline mirrors how a real file (one line ended with echo,
	// or a text editor's save) normally looks; >> just resumes writing at
	// EOF, it does not insert a separator, so seeding without one would
	// merge onto the last line rather than proving append landed on a new one.
	if err := os.WriteFile(filepath.FromSlash(file), []byte("old contents\n"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file, err)
	}

	session := fmt.Sprintf("echo new contents >> '%s'\ncat '%s'\n", file, file)
	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "old contents\nnew contents",
		"spec: unlike >, which overwrites the file, >> adds the output to the end of the file and preserves any existing content")

	data, err := os.ReadFile(filepath.FromSlash(file))
	if err != nil {
		t.Fatalf("setup: cannot read back %q: %v", file, err)
	}
	wantEqual(t, typedSession(session), strings.TrimRight(string(data), "\r\n"), "old contents\nnew contents",
		"spec: the file's old contents are kept, with the new output added after them")
}

// ============================================================
// stderr append (2>>)
// ============================================================

func TestE2E_StderrAppend(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	session := fmt.Sprintf("cat '%s/nonexistent' 2>> '%s/errors.txt'\ncat '%s/errors.txt'\n", dir, dir, dir)
	want := "nonexistent"
	why := "spec: if the file doesn't exist, it is created, and the command's standard error is written into it"

	got := runShell(t, binary, session)
	assertContainsWhy(t, session, got, want, why)
}

func TestE2E_StderrAppend_PreservesExistingContent(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	session := fmt.Sprintf("cat '%s/nonexistent1' 2>> '%s/errors.txt'\ncat '%s/nonexistent2' 2>> '%s/errors.txt'\ncat '%s/errors.txt'\n",
		dir, dir, dir, dir, dir)
	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "nonexistent1",
		"spec: unlike 2>, which overwrites the file, 2>> preserves the first command's error instead of losing it to the second")
	assertContainsWhy(t, session, got, "nonexistent2",
		"spec: the second command's error is added to the end of the file, after the first")
}

func TestE2E_StderrAppend_StdoutNotRedirected(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	existing := dir + "/blueberry"
	if err := os.WriteFile(filepath.FromSlash(existing), []byte("blueberry"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", existing, err)
	}

	outFile := dir + "/quz.md"
	session := fmt.Sprintf("cat '%s' nonexistent 2>> '%s'\ncat '%s'\n", existing, outFile, outFile)
	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "blueberry",
		"spec: standard output still appears on the terminal (not redirected) when only stderr is sent to a file")

	data, err := os.ReadFile(filepath.FromSlash(outFile))
	if err != nil {
		t.Fatalf("setup: cannot read back %q: %v", outFile, err)
	}
	if strings.Contains(string(data), "blueberry") {
		t.Error(failLine(typedSession(session), "no stdout content", show(string(data)),
			"spec: only the command's standard error is appended to the file, not its standard output"))
	} else {
		t.Logf("%s %s\n    expected: no stdout content\n    received: %s", markPass, typedSession(session), show(string(data)))
	}
}

// ============================================================
// tab autocompletion
// ============================================================

func TestE2E_TabAutocomplete(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "ech<TAB> completes to echo, ready for an argument",
			session: "ech\tworld\n",
			want:    "world",
			why:     "spec: ech<TAB> completes to echo with a trailing space, so typing world afterwards becomes its argument, not part of the command name",
		},
		{
			name:    "exi<TAB> completes to exit, shell exits without a 'not found' error",
			session: "exi\textra\n",
			want:    "not found",
			why:     "spec: exi<TAB> completes to exit (with a trailing space) so the line runs the exit builtin and terminates cleanly, rather than the concatenated word exitextra failing to resolve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			if tt.name == "exi<TAB> completes to exit, shell exits without a 'not found' error" {
				if strings.Contains(got, tt.want) {
					t.Error(failLine(typedSession(tt.session), "no \"not found\" error", got, tt.why))
				} else {
					t.Logf("%s %s\n    expected: no \"not found\" error\n    received: %s", markPass, typedSession(tt.session), show(got))
				}
				return
			}
			assertContainsWhy(t, tt.session, got, tt.want, tt.why)
		})
	}
}

// ============================================================
// tab autocompletion — invalid completions
// ============================================================

func TestE2E_TabAutocomplete_NoMatch(t *testing.T) {
	binary := buildTestBinary(t)

	session := "xyz\t\n"
	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "\x07",
		"spec: pressing <TAB> with no matching completion rings the bell (\\x07)")
	assertContainsWhy(t, session, got, "xyz",
		"spec: input is left unchanged when no completion is possible, so the unmatched word still reaches the command line")
}

// ============================================================
// tab completion — external executables
// ============================================================

func TestE2E_TabAutocomplete_Executable(t *testing.T) {
	catPath, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	// Windows will only resolve an extensionless name on PATH via PATHEXT
	// if the file on disk actually carries one of those extensions, so the
	// copy needs a real .exe suffix even though the completion (and the
	// session below) never mentions one.
	exePath := dir + "/custom_executable"
	if runtime.GOOS == "windows" {
		exePath += ".exe"
	}
	data, err := os.ReadFile(catPath)
	if err != nil {
		t.Fatalf("setup: cannot read cat binary: %v", err)
	}
	if err := os.WriteFile(exePath, data, 0o755); err != nil {
		t.Fatalf("setup: cannot write %q: %v", exePath, err)
	}

	file := dir + "/file.txt"
	if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file, err)
	}

	tests := []struct {
		name string
		path string // value PATH is set to before the session runs
		why  string
	}{
		{
			name: "custom<TAB> completes to custom_executable and runs it",
			path: dir,
			why:  "spec: custom<TAB> completes to custom_executable (with a trailing space), so the file argument that follows is passed to it and printed",
		},
		{
			name: "completion still works when PATH also lists a directory that doesn't exist",
			path: filepath.ToSlash(filepath.Join(t.TempDir(), "does-not-exist")) + string(os.PathListSeparator) + dir,
			why:  "notes: PATH can include directories that don't exist on disk, so completion must handle that gracefully rather than failing the whole lookup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Prepend the test directories to the real PATH rather than
			// replacing it: the copied cat binary is an MSYS/Git-Bash build
			// on Windows and needs its runtime DLL, which lives in a
			// directory the real PATH already provides.
			t.Setenv("PATH", tt.path+string(os.PathListSeparator)+os.Getenv("PATH"))
			session := fmt.Sprintf("custom\t'%s'\n", file)
			got := runShell(t, binary, session)
			assertContainsWhy(t, session, got, "content", tt.why)
		})
	}
}

// ============================================================
// tab completion — multiple matches (double <TAB>)
// ============================================================

func TestE2E_TabAutocomplete_MultipleMatches(t *testing.T) {
	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	for _, name := range []string{"xyz_quz", "xyz_bar", "xyz_baz"} {
		if err := os.WriteFile(dir+"/"+name, []byte(""), 0o755); err != nil {
			t.Fatalf("setup: cannot create %q: %v", name, err)
		}
	}
	t.Setenv("PATH", dir)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "first <TAB> on an ambiguous prefix only rings the bell",
			session: "xyz_\t\n",
			want:    "\x07",
			why:     "spec: on the first <TAB> press, ring the bell",
		},
		{
			name:    "second <TAB> lists every match, alphabetically sorted and space-separated",
			session: "xyz_\t\t\n",
			want:    "xyz_bar  xyz_baz  xyz_quz",
			why:     "spec: on the second <TAB> press, print all matching executables on a new line, listed in alphabetical order, separated by at least one space",
		},
		{
			name:    "the prompt reappears on the next line with the original prefix preserved",
			session: "xyz_\t\t\n",
			want:    "$ xyz_",
			why:     "spec: show the prompt again on the next line, keeping the original command prefix",
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
