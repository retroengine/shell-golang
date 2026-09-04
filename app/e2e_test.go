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

	file1 := dir + "/number 1"
	file2 := dir + "/doublequote \" 2"
	file3 := dir + "/backslash \\ 3"

	if err := os.WriteFile(file1, []byte("content1"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file1, err)
	}
	if err := os.WriteFile(file2, []byte("content2"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file2, err)
	}
	if err := os.WriteFile(file3, []byte("content3"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file3, err)
	}

	// Build session: filenames with special chars are passed using \\" and \\\\ inside double quotes.
	session := fmt.Sprintf("cat \"%s\" \"%s\" \"%s\"\n",
		dir+"/number 1",
		dir+"/doublequote \\\" 2",
		dir+"/backslash \\\\ 3")
	want := "content1content2content3"
	why := "spec: \\\" yields a literal \" and \\\\\\\\ yields a literal \\ inside double quotes, giving the correct filenames to cat"

	got := runShell(t, binary, session)
	assertContainsWhy(t, session, got, want, why)
}

// ============================================================
// backslash outside quotes
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

	file1 := dir + "/no slash 1"
	file2 := dir + "/one slash \\2"
	file3 := dir + "/two slashes \\3\\"

	if err := os.WriteFile(file1, []byte("content1"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file1, err)
	}
	if err := os.WriteFile(file2, []byte("content2"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file2, err)
	}
	if err := os.WriteFile(file3, []byte("content3"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file3, err)
	}

	session := fmt.Sprintf("cat '%s' '%s' '%s'\n", file1, file2, file3)
	want := "content1content2content3"
	why := "spec: backslashes inside single quotes are literal, so the filenames with backslashes are passed verbatim to cat"

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
