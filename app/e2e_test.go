package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

	// pwd reports os.Getwd(), the kernel-canonical path, so the expected value
	// must be resolved too: macOS $TMPDIR is /var/folders/... while Getwd says
	// /private/var/folders/..., and Windows may hand back a short 8.3 path.
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatalf("cannot resolve temp dir %q: %v", tmp, err)
	}

	session := fmt.Sprintf("cd '%s'\npwd\n", filepath.ToSlash(resolved))
	got := runShell(t, binary, session)
	assertContains(t, session, got, resolved)
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

// buildFileCatScript compiles a tiny standalone program that reads the file
// named by its first argument and copies its content to stdout — the same
// observable contract as "cat <file>", but self-contained rather than
// borrowed from the system. Tests that need to copy an executable under an
// unusual name (to prove the shell can find and run it, not to test cat
// itself) must not copy the system's own cat to do it: on this environment
// cat is a uutils-coreutils multi-call binary that refuses to run under a
// name it doesn't recognise as one of its own utilities (basename must end
// in a known name — "xcat" runs fine, "custom_executable" or "exe with
// 'single quotes'" gets "coreutils: unknown program ..." and exits 1), so
// copying the real system binary to an arbitrary name silently breaks.
func buildFileCatScript(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	src := `package main

import (
	"io"
	"os"
)

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		os.Exit(1)
	}
	defer f.Close()
	io.Copy(os.Stdout, f)
}
`
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write file-cat script source: %v", err)
	}

	binPath := filepath.Join(dir, "filecat")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build file-cat script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

func TestE2E_QuotedExecutable(t *testing.T) {
	fileCatData, err := os.ReadFile(filepath.FromSlash(buildFileCatScript(t)))
	if err != nil {
		t.Fatalf("setup: cannot read file-cat script: %v", err)
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
		if err := os.WriteFile(dst, fileCatData, 0o755); err != nil {
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
	fileCatData, err := os.ReadFile(filepath.FromSlash(buildFileCatScript(t)))
	if err != nil {
		t.Fatalf("setup: cannot read file-cat script: %v", err)
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
	if err := os.WriteFile(exePath, fileCatData, 0o755); err != nil {
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
			// replacing it, so anything else the shell or its child needs
			// to resolve from the real environment is still reachable.
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
// complete -p
// ============================================================

func TestE2E_CompleteDashP(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "complete -p git reports no completion specification",
			session: "complete -p git\n",
			want:    "complete: git: no completion specification",
			why:     "spec: complete -p <command> prints 'complete: <command>: no completion specification' when nothing has been registered",
		},
		{
			name:    "the command name passed to -p is reflected in the message",
			session: "complete -p docker\n",
			want:    "complete: docker: no completion specification",
			why:     "spec: the command name in the output matches the one passed to -p",
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
// complete -C (register) and -p (display registered completions)
// ============================================================

func TestE2E_CompleteRegisterAndDisplay(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "registering with -C then querying with -p prints the normalized format",
			session: "complete -C /path/to/git/completer git\ncomplete -p git\n",
			want:    "complete -C '/path/to/git/completer' git",
			why:     "spec: complete -C /path/to/git/completer git then complete -p git prints complete -C '/path/to/git/completer' git",
		},
		{
			name:    "a different registered command prints its own path",
			session: "complete -C /path/to/docker/completer docker\ncomplete -p docker\n",
			want:    "complete -C '/path/to/docker/completer' docker",
			why:     "spec: complete -C /path/to/docker/completer docker then complete -p docker prints complete -C '/path/to/docker/completer' docker",
		},
		{
			name:    "extra whitespace in the -C invocation still produces single-spaced output",
			session: "complete   -C    /path/to/git/completer   git\ncomplete -p git\n",
			want:    "complete -C '/path/to/git/completer' git",
			why:     "spec: arguments are separated by exactly one space in the output, regardless of how the registration command was written",
		},
		{
			name:    "an unregistered command still gives the earlier stage's error",
			session: "complete -p nosuchcmd\n",
			want:    "complete: nosuchcmd: no completion specification",
			why:     "spec: that error is still the correct response when no completion has been registered for the given command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContainsWhy(t, tt.session, got, tt.want, tt.why)
		})
	}
}

func TestE2E_CompleteDashC_NoOutput(t *testing.T) {
	binary := buildTestBinary(t)

	session := "complete -C /path/to/git/completer git\nexit\n"
	got := runShell(t, binary, session)

	if strings.Contains(got, "complete") {
		t.Error(failLine(typedSession(session), "no output containing \"complete\"", got,
			"spec: complete -C <path> <command> registers the completion and produces no output"))
	} else {
		t.Logf("%s %s\n    expected: no output containing \"complete\"\n    received: %s", markPass, typedSession(session), show(got))
	}
}


// buildCompleterScript compiles a tiny standalone program that sleeps for
// delay (if non-zero) and then prints output as its single line of stdout,
// standing in for the external completer scripts registered via
// `complete -C`. Returns its path, slash-normalised for embedding in a
// session string.
func buildCompleterScript(t *testing.T, output string, delay time.Duration) string {
	t.Helper()

	dir := t.TempDir()
	src := fmt.Sprintf(`package main

import (
	"fmt"
	"time"
)

func main() {
	time.Sleep(%d * time.Millisecond)
	fmt.Println(%q)
}
`, delay.Milliseconds(), output)

	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write completer source: %v", err)
	}

	binPath := filepath.Join(dir, "completer")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build completer script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// buildBlockingScript compiles a tiny standalone program that blocks
// reading from stdin until it hits EOF, then exits. Used (from
// main_test.go) as a background job that reliably stays "Running" for as
// long as a test needs it to, without an arbitrary sleep duration, and
// terminates cleanly once the test explicitly closes its stdin — avoiding
// a lingering process that could still hold its own binary file open when
// t.TempDir() tries to clean it up.
func buildBlockingScript(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	src := `package main

import (
	"io"
	"os"
)

func main() {
	io.Copy(io.Discard, os.Stdin)
}
`
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write blocking script source: %v", err)
	}

	binPath := filepath.Join(dir, "blocker")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build blocking script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// ============================================================
// running the completer script
// ============================================================

func TestE2E_CompleterScript(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name            string
		command         string // the command a completer is registered for
		completerOutput string // the single line the completer script prints
		typedAfterTab   string // extra text typed right after the <TAB> completes
		want            string
		why             string
	}{
		{
			name:            "the completer's single line completes the word, followed by a trailing space",
			command:         "echo",
			completerOutput: "saikiran",
			typedAfterTab:   "",
			want:            "saikiran",
			why:             "spec: your shell should run the registered completer script, read its stdout, and use that single line to complete the user's input, followed by a trailing space",
		},
		{
			name:            "text typed after the completion starts a new argument",
			command:         "echo",
			completerOutput: "foo",
			typedAfterTab:   "bar",
			want:            "foo bar",
			why:             "spec: the completed line is followed by a trailing space, so text typed next (bar) becomes a separate argument rather than continuing the completed word (foo)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completerPath := buildCompleterScript(t, tt.completerOutput, 0)

			session := fmt.Sprintf("complete -C '%s' %s\n%s \t%s\n",
				completerPath, tt.command, tt.command, tt.typedAfterTab)

			got := runShell(t, binary, session)
			assertContainsWhy(t, session, got, tt.want, tt.why)
		})
	}
}

// ============================================================
// running the completer script — waits for it to finish
// ============================================================

func TestE2E_CompleterScript_WaitsForSlowScript(t *testing.T) {
	binary := buildTestBinary(t)

	completerPath := buildCompleterScript(t, "delayed_output", 300*time.Millisecond)

	session := fmt.Sprintf("complete -C '%s' echo\necho \t\n", completerPath)
	want := "delayed_output"
	why := "notes: the shell must wait for the completer script to finish before inserting the completion, otherwise it may read partial output"

	got := runShell(t, binary, session)
	assertContainsWhy(t, session, got, want, why)
}

// ============================================================
// running the completer script — scoped to the registered command
// ============================================================

func TestE2E_CompleterScript_ScopedToRegisteredCommand(t *testing.T) {
	binary := buildTestBinary(t)

	completerPath := buildCompleterScript(t, "alpha_candidate", 0)

	// alpha has a completer registered; beta does not, so its <TAB> must not
	// pick up alpha's script output.
	session := fmt.Sprintf("complete -C '%s' alpha\nbeta \t\n", completerPath)
	got := runShell(t, binary, session)

	if strings.Contains(got, "alpha_candidate") {
		t.Error(failLine(typedSession(session), "no completion from alpha's script", got,
			"spec: your shell should first check whether a completer is registered for that command; a completer registered for a different command must not be used"))
	} else {
		t.Logf("%s %s\n    expected: no completion from alpha's script\n    received: %s", markPass, typedSession(session), show(got))
	}
}

// buildEmptyCompleterScript compiles a completer program that exits
// successfully without writing anything to stdout — the true "no
// candidates" case, distinct from buildCompleterScript(t, "", 0), which
// would still print a bare newline.
func buildEmptyCompleterScript(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("setup: cannot write completer source: %v", err)
	}

	binPath := filepath.Join(dir, "completer")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build completer script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// ============================================================
// running the completer script — empty output
// ============================================================

func TestE2E_CompleterScript_EmptyOutput(t *testing.T) {
	binary := buildTestBinary(t)

	completerPath := buildEmptyCompleterScript(t)

	session := fmt.Sprintf("complete -C '%s' echo\necho xyz\t\n", completerPath)
	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "\x07",
		"spec: when the completer script prints nothing, the shell rings the terminal bell")
	assertContainsWhy(t, session, got, "xyz",
		"spec: the input line is left unchanged, so echo still runs with its originally typed argument")
}

// buildArgEchoCompleterScript compiles a completer program that echoes back
// exactly what it received as its own arguments, joined by "|" — so a test
// can assert on the literal argv[1..3] values the shell passed, independent
// of any candidate-filtering logic (which is the completer script's own job
// per spec, not something the shell needs to get right).
func buildArgEchoCompleterScript(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	src := `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println(strings.Join(os.Args[1:], "|"))
}
`
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write completer source: %v", err)
	}

	binPath := filepath.Join(dir, "completer")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build completer script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// ============================================================
// running the completer script — passing argv[1..3]
// ============================================================

func TestE2E_CompleterScript_PassesArguments(t *testing.T) {
	binary := buildTestBinary(t)
	completerPath := buildArgEchoCompleterScript(t)

	tests := []struct {
		name  string
		typed string // what's typed, right up to <TAB>, after "complete -C ... echo\n"
		want  string // the argv[1]|argv[2]|argv[3] the completer should have received
		why   string
	}{
		{
			name:  "command, partial word, and the word before it",
			typed: "echo remote set",
			want:  "echo|set|remote",
			why:   "spec: for `git remote set<TAB>`, the shell passes argv[1]=git (command), argv[2]=set (the word being completed), argv[3]=remote (the word before it)",
		},
		{
			name:  "partial word with no preceding word",
			typed: "echo s",
			want:  "echo|s|",
			why:   "spec: argv[3] is an empty string when there's no word before the one being completed",
		},
		{
			name:  "no partial text at all, right after the command",
			typed: "echo ",
			want:  "echo||",
			why:   "notes: e.g. git <TAB> — pass an empty string as argv[3]; here argv[2] is empty too, since nothing has been typed yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := fmt.Sprintf("complete -C '%s' echo\n%s\t\n", completerPath, tt.typed)
			got := runShell(t, binary, session)
			assertContainsWhy(t, session, got, tt.want, tt.why)
		})
	}
}

// buildEnvEchoCompleterScript compiles a completer program that echoes back
// the COMP_LINE and COMP_POINT environment variables it received, joined by
// "|" — so a test can assert on the literal values the shell set.
func buildEnvEchoCompleterScript(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	src := `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(os.Getenv("COMP_LINE") + "|" + os.Getenv("COMP_POINT"))
}
`
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write completer source: %v", err)
	}

	binPath := filepath.Join(dir, "completer")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build completer script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// ============================================================
// running the completer script — COMP_LINE / COMP_POINT
// ============================================================

func TestE2E_CompleterScript_PassesCompEnv(t *testing.T) {
	binary := buildTestBinary(t)
	completerPath := buildEnvEchoCompleterScript(t)

	tests := []struct {
		name  string
		typed string // what's typed, right up to <TAB>, after "complete -C ... echo\n"
		want  string // the COMP_LINE|COMP_POINT the completer should have received
		why   string
	}{
		{
			name:  "COMP_LINE is the full typed line, COMP_POINT its byte length",
			typed: "echo ad",
			want:  "echo ad|7",
			why:   `spec: for "git ad<TAB>" the shell sets COMP_LINE="git ad" and COMP_POINT=6 (its byte length); "echo ad" is 7 bytes for the same reason, just a longer command name`,
		},
		{
			name:  "COMP_POINT is a byte index, not a character index",
			typed: "echo é",
			want:  "echo é|7",
			why:   `notes: COMP_POINT is a byte index — "echo é" is 7 bytes ("echo " is 5 ASCII bytes plus 2 bytes for the multibyte é), not 6, which is what counting runes/characters would give`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := fmt.Sprintf("complete -C '%s' echo\n%s\t\n", completerPath, tt.typed)
			got := runShell(t, binary, session)
			assertContainsWhy(t, session, got, tt.want, tt.why)
		})
	}
}

// buildFixedCandidatesCompleterScript compiles a completer program that
// always prints the given candidates, one per line, in the exact order
// given — so a test can control whether they arrive pre-sorted or not.
func buildFixedCandidatesCompleterScript(t *testing.T, candidates []string) string {
	t.Helper()

	dir := t.TempDir()
	src := fmt.Sprintf(`package main

import "fmt"

func main() {
	for _, c := range %#v {
		fmt.Println(c)
	}
}
`, candidates)

	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write completer source: %v", err)
	}

	binPath := filepath.Join(dir, "completer")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build completer script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// ============================================================
// running the completer script — multiple candidates
// ============================================================

func TestE2E_CompleterScript_MultipleCandidates(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name       string
		candidates []string // what the completer script prints, in this order
		session    string   // %s is replaced with the completer's path
		want       string
		why        string
	}{
		{
			name:       "first <TAB> rings the bell since there's no unique completion",
			candidates: []string{"add", "commit", "push"},
			session:    "complete -C '%s' git\ngit \t\n",
			want:       "\x07",
			why:        "spec: the first TAB should ring the terminal bell (since there's no unique completion)",
		},
		{
			name:       "second <TAB> displays every candidate, space-separated",
			candidates: []string{"add", "commit", "push"},
			session:    "complete -C '%s' git\ngit \t\t\n",
			want:       "add  commit  push",
			why:        "spec: the second TAB should display all candidates on the next line, separated by at least one space",
		},
		{
			name:       "candidates are sorted alphabetically regardless of the order the completer printed them",
			candidates: []string{"push", "add", "commit"},
			session:    "complete -C '%s' git\ngit \t\t\n",
			want:       "add  commit  push",
			why:        "spec: the second TAB should display all candidates sorted alphabetically",
		},
		{
			name:       "the prompt and original input are reprinted after the candidate list",
			candidates: []string{"add", "commit", "push"},
			session:    "complete -C '%s' git\ngit \t\t\n",
			want:       "$ git ",
			why:        "spec: after displaying the candidates, the shell should reprint the prompt with the original input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completerPath := buildFixedCandidatesCompleterScript(t, tt.candidates)
			session := fmt.Sprintf(tt.session, completerPath)
			got := runShell(t, binary, session)
			assertContainsWhy(t, session, got, tt.want, tt.why)
		})
	}
}

// buildPrefixFilteringCompleterScript compiles a completer program that, given
// its second argument (the word being completed — matching the shell's own
// os.Args[2] convention, see runCompleter), prints only the candidates from
// allCandidates that start with it. Unlike buildFixedCandidatesCompleterScript
// (which always returns every candidate), this one narrows as more is typed,
// letting a test drive a candidate list down to a single match.
func buildPrefixFilteringCompleterScript(t *testing.T, allCandidates []string) string {
	t.Helper()

	dir := t.TempDir()
	src := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	word := ""
	if len(os.Args) > 2 {
		word = os.Args[2]
	}
	for _, c := range %#v {
		if strings.HasPrefix(c, word) {
			fmt.Println(c)
		}
	}
}
`, allCandidates)

	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write completer source: %v", err)
	}

	binPath := filepath.Join(dir, "completer")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build completer script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// ============================================================
// running the completer script — longest common prefix completion
// ============================================================

func TestE2E_CompleterScript_LongestCommonPrefix(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name     string
		filtered bool // true: completer narrows by the typed word; false: always returns both candidates
		session  string // %s is replaced with the completer's path
		want     string
		why      string
	}{
		{
			name:     "candidates sharing a prefix longer than what's typed complete silently to that prefix",
			filtered: false,
			session:  "complete -C '%s' echo\necho c\t\n",
			want:     "che",
			why:      "spec: checkout and cherry-pick share the prefix che, longer than the typed c, so the shell completes to che",
		},
		{
			name:     "typing further to leave only one candidate completes the full word with a trailing space",
			filtered: true,
			session:  "complete -C '%s' echo\necho chec\t\n",
			want:     "checkout",
			why:      "spec: only checkout still matches once chec is typed, so TAB completes the full word",
		},
		{
			name:     "the trailing space after completing the sole candidate starts a new argument",
			filtered: true,
			session:  "complete -C '%s' echo\necho chec\tX\n",
			want:     "checkout X",
			why:      "spec: completing to the sole candidate is followed by a trailing space, so text typed next (X) becomes a separate argument",
		},
		{
			name:     "an LCP equal to what's already typed is treated as no common prefix: bell, then list on the next TAB",
			filtered: false,
			session:  "complete -C '%s' echo\necho che\t\t\n",
			want:     "checkout  cherry-pick",
			why:      "notes: if the LCP equals what the user has already typed, treat it the same as no common prefix — ring the bell and display candidates on the next TAB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var completerPath string
			if tt.filtered {
				completerPath = buildPrefixFilteringCompleterScript(t, []string{"checkout", "cherry-pick"})
			} else {
				completerPath = buildFixedCandidatesCompleterScript(t, []string{"checkout", "cherry-pick"})
			}
			session := fmt.Sprintf(tt.session, completerPath)
			got := runShell(t, binary, session)
			assertContainsWhy(t, session, got, tt.want, tt.why)
		})
	}
}

// ============================================================
// running the completer script — no bell when the LCP extends the input
// ============================================================

func TestE2E_CompleterScript_LongestCommonPrefix_NoBell(t *testing.T) {
	binary := buildTestBinary(t)
	completerPath := buildFixedCandidatesCompleterScript(t, []string{"checkout", "cherry-pick"})

	session := fmt.Sprintf("complete -C '%s' echo\necho c\t\n", completerPath)
	why := "spec: no bell rings when the LCP extends the current input"

	got := runShell(t, binary, session)
	call := typedSession(session)

	if strings.Contains(got, "\x07") {
		t.Error(failLine(call, "no bell (the LCP extends the input)", show(got), why))
	} else {
		t.Logf("%s %s\n    expected: no bell (the LCP extends the input)\n    received: %s", markPass, call, show(got))
	}
}

// ============================================================
// complete -r (remove a registered completion)
// ============================================================

func TestE2E_CompleteDashR(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "removing a registered completion makes -p report none registered",
			session: "complete -C /path/to/yarn/completer yarn\ncomplete -r yarn\ncomplete -p yarn\n",
			want:    "complete: yarn: no completion specification",
			why:     "spec: complete -r <command> removes any stored completion rule for that command",
		},
		{
			name:    "removing one command's rule leaves a different command's registration intact",
			session: "complete -C /path/to/helm/completer helm\ncomplete -r terraform\ncomplete -p helm\n",
			want:    "complete -C '/path/to/helm/completer' helm",
			why:     "spec: -r removes the rule for the named command; an unrelated command's registration must survive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContainsWhy(t, tt.session, got, tt.want, tt.why)
		})
	}
}

func TestE2E_CompleteDashR_NoOutput(t *testing.T) {
	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		why     string
	}{
		{
			name:    "removing a registered completion produces no output",
			session: "complete -C /path/to/git/completer git\ncomplete -r git\nexit\n",
			why:     "spec: the command should produce no output on success",
		},
		{
			name:    "removing a command with no completion registered is still not an error",
			session: "complete -r nosuchcmd\nexit\n",
			why:     "notes: if complete -r is called for a command that has no completion registered, the shell should still produce no output and not treat it as an error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			if strings.Contains(got, "complete") {
				t.Error(failLine(typedSession(tt.session), "no output containing \"complete\"", got, tt.why))
			} else {
				t.Logf("%s %s\n    expected: no output containing \"complete\"\n    received: %s", markPass, typedSession(tt.session), show(got))
			}
		})
	}
}

func TestE2E_CompleteDashR_TabNoLongerCompletes(t *testing.T) {
	binary := buildTestBinary(t)
	completerPath := buildCompleterScript(t, "checkout", 0)

	session := fmt.Sprintf("complete -C '%s' git\ncomplete -r git\ngit \t\n", completerPath)
	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "\x07",
		"spec: after complete -r git, pressing TAB for git should behave as if no completion was ever registered, so the bell rings")

	if strings.Contains(got, "checkout") {
		t.Error(failLine(typedSession(session), "no completion from the removed script", got,
			"spec: complete -r removes the stored completion rule, so its candidate must not appear after removal"))
	} else {
		t.Logf("%s %s\n    expected: no completion from the removed script\n    received: %s", markPass, typedSession(session), show(got))
	}
}

// ============================================================
// background jobs (&)
//
// The tester-verified behaviours from the spec: (1) the job line
// "[JOB_NUMBER] PID" appears on its own line, (2) the shell doesn't wait
// for the command and keeps reading input, (3) the background process
// actually starts running. Every background command's own stdout is
// redirected to a file with the shell's existing `>` feature: runShell
// captures the shell process's stdout into a bytes.Buffer, and Go's exec
// internals won't consider that pipe closed until every process holding
// the fd — including a still-running background grandchild — exits, so
// an un-redirected background job would make runShell itself block.
// ============================================================

func TestE2E_BackgroundJob_PrintsJobLine(t *testing.T) {
	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())
	sleeperPath := buildCompleterScript(t, "bg_done_marker", 0)

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "prints [job_number] pid on its own line",
			session: fmt.Sprintf("'%s' > '%s/job_line.txt' &\n", sleeperPath, dir),
			want:    "[1] ",
			why:     "spec: 'When a background job starts, the shell prints a line showing its job number and process ID: $ sleep 30 & / [1] 84470' — 'in this stage only one background job will be started, so the job number is always [1]'",
		},
		{
			name:    "keeps reading and runs the next command after starting a background job",
			session: fmt.Sprintf("'%s' > '%s/keeps_reading.txt' &\necho after_bg_marker\n", sleeperPath, dir),
			want:    "after_bg_marker",
			why:     "spec: 'show the next prompt immediately' — the shell must resume reading and running subsequent commands right after starting a background job, not stall on it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			assertContainsWhy(t, tt.session, got, tt.want, tt.why)
		})
	}
}

func TestE2E_BackgroundJob_DoesNotBlockShell(t *testing.T) {
	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	delay := 2 * time.Second
	sleeperPath := buildCompleterScript(t, "bg_slow_marker", delay)
	outFile := dir + "/bg_slow_out.txt"
	session := fmt.Sprintf("'%s' > '%s' &\n", sleeperPath, outFile)
	why := "spec: 'The shell starts the program but doesn't wait for it to finish, allowing you to continue typing other commands' — tester check (2): 'the next prompt appears immediately (the shell doesn't wait for the command to finish)'"

	start := time.Now()
	runShell(t, binary, session)
	elapsed := time.Since(start)

	const maxElapsed = 1 * time.Second
	if elapsed >= maxElapsed {
		t.Error(failLine(typedSession(session), "shell returns in well under "+maxElapsed.String(), elapsed.String(), why))
	} else {
		t.Logf("%s %s\n    expected: shell returns in well under %s\n    received: %s", markPass, typedSession(session), maxElapsed.String(), show(elapsed.String()))
	}
}

func TestE2E_BackgroundJob_ActuallyRuns(t *testing.T) {
	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	delay := 2 * time.Second
	sleeperPath := buildCompleterScript(t, "bg_ran_marker", delay)
	outFile := dir + "/bg_ran_out.txt"
	session := fmt.Sprintf("'%s' > '%s' &\n", sleeperPath, outFile)
	why := "spec: tester check (3): 'the background process actually starts running' — its delayed stdout must eventually land in the file it was redirected to, even though the shell (and this runShell call) already returned"

	runShell(t, binary, session)

	const pollFor = 4 * time.Second // delay (2s) plus margin: the grandchild may finish slightly after the shell process itself exits
	const pollEvery = 100 * time.Millisecond
	outFileNative := filepath.FromSlash(outFile)

	deadline := time.Now().Add(pollFor)
	var got string
	for {
		if data, err := os.ReadFile(outFileNative); err == nil {
			got = string(data)
			if strings.Contains(got, "bg_ran_marker") {
				break
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(pollEvery)
	}

	wantContains(t, typedSession(session), got, "bg_ran_marker", why)
}

// ============================================================
// background job output — shares the shell's stdout/stderr
//
// Unlike TestE2E_BackgroundJob_* above, these deliberately do NOT
// redirect the background job's own stdout/stderr to a file: the point
// here is to prove that with no redirect, its output still reaches the
// same terminal stream the shell itself writes to. runShell blocking
// until the child's inherited stdout/stderr pipe closes (the same
// mechanism flagged as a gotcha in the section above) is exactly what
// lets these tests observe the content — the delay is kept short (200ms)
// since nothing here is racing a deadline.
// ============================================================

// buildStderrScript compiles a tiny standalone program that sleeps for
// delay (if non-zero) and then prints output as its single line of
// stderr — a stderr-writing sibling of buildCompleterScript. Returns its
// path, slash-normalised for embedding in a session string.
func buildStderrScript(t *testing.T, output string, delay time.Duration) string {
	t.Helper()

	dir := t.TempDir()
	src := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	time.Sleep(%d * time.Millisecond)
	fmt.Fprintln(os.Stderr, %q)
}
`, delay.Milliseconds(), output)

	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write stderr script source: %v", err)
	}

	binPath := filepath.Join(dir, "stderr_script")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build stderr script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// runShellCapturingStderr behaves like runShell, but captures the shell
// process's own stderr into a separate buffer instead of discarding it
// (runShell sets cmd.Stderr = nil to hide the shell's EOF-panic trace).
// A background job's stderr is inherited from the shell's own os.Stderr
// (builtins.go:106), so runShell's discarded stderr can never observe it —
// this dedicated variant exists solely to make that claim testable,
// without changing runShell's behavior for every other E2E test.
func runShellCapturingStderr(t *testing.T, binary, session string) (stdout, stderr string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary)
	cmd.Stdin = strings.NewReader(session)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("shell did not exit within 5s\n  session: %q\n  stdout so far: %q\n  stderr so far: %q", session, outBuf.String(), errBuf.String())
	}
	return outBuf.String(), errBuf.String()
}

func TestE2E_BackgroundJob_StdoutAppearsInTerminal(t *testing.T) {
	binary := buildTestBinary(t)
	session := fmt.Sprintf("'%s' &\n", buildCompleterScript(t, "bg_stdout_marker", 200*time.Millisecond))
	why := "spec: 'the background process's stdout and stderr streams remain connected to the shell's terminal... any output it produces should still appear in your shell' — with no redirect on the command, its stdout must reach the same terminal the shell itself writes to, not be silently dropped"

	got := runShell(t, binary, session)
	assertContainsWhy(t, session, got, "bg_stdout_marker", why)
}

func TestE2E_BackgroundJob_StderrAppearsInTerminal(t *testing.T) {
	binary := buildTestBinary(t)
	session := fmt.Sprintf("'%s' &\n", buildStderrScript(t, "bg_stderr_marker", 200*time.Millisecond))
	why := "spec: 'you must ensure the background process shares the same stdout and stderr as the shell' — stderr is named explicitly alongside stdout, so a background job's error output must also reach the terminal"

	_, gotStderr := runShellCapturingStderr(t, binary, session)
	wantContains(t, typedSession(session), gotStderr, "bg_stderr_marker", why)
}

func TestE2E_BackgroundJob_ForegroundStillWorksAfterBackgroundJob(t *testing.T) {
	binary := buildTestBinary(t)

	sleeperPath := buildCompleterScript(t, "bg_marker", 200*time.Millisecond)
	session := fmt.Sprintf("'%s' &\necho foreground_marker\n", sleeperPath)
	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "bg_marker",
		"spec: the background job's own output still appears in the terminal")
	assertContainsWhy(t, session, got, "foreground_marker",
		"spec: 'I can type this immediately' — a foreground command started right after a background job runs normally and its own output still appears, sharing the same terminal without interference")
}

// ============================================================
// jobs — lists a single running background job
// ============================================================

func TestE2E_Jobs_ListsSingleRunningJob(t *testing.T) {
	binary := buildTestBinary(t)

	sleeperPath := buildCompleterScript(t, "jobs_marker", 200*time.Millisecond)
	session := fmt.Sprintf("'%s' &\njobs\n", sleeperPath)
	why := "spec: 'The jobs builtin lists background jobs in this format: [1]+  Running                 sleep 10 &' — tester checks: job number [1], marker +, status Running, command matching what was run"

	got := runShell(t, binary, session)
	want := fmt.Sprintf("[1]+  Running                 %s &", sleeperPath)

	assertContainsWhy(t, session, got, want, why)
}

func TestE2E_Jobs_ListsMultipleRunningJobs(t *testing.T) {
	binary := buildTestBinary(t)

	job1 := buildCompleterScript(t, "e2e_job1", 0)
	job2 := buildCompleterScript(t, "e2e_job2", 0)
	job3 := buildCompleterScript(t, "e2e_job3", 0)

	session := fmt.Sprintf("'%s' &\njobs\n'%s' &\njobs\n'%s' &\njobs\n", job1, job2, job3)
	why := "spec: 'When multiple commands run in the background, the jobs command lists them in the order they were started' — the current job (+) moves to the newest, the previous current job becomes previous (-), and older jobs get a blank marker"

	got := runShell(t, binary, session)

	afterFirst := fmt.Sprintf("[1]+  Running                 %s &", job1)
	afterSecond := fmt.Sprintf("[1]-  Running                 %s &\n[2]+  Running                 %s &", job1, job2)
	afterThird := fmt.Sprintf("[1]   Running                 %s &\n[2]-  Running                 %s &\n[3]+  Running                 %s &", job1, job2, job3)

	assertContainsWhy(t, session, got, afterFirst, why)
	assertContainsWhy(t, session, got, afterSecond, why)
	assertContainsWhy(t, session, got, afterThird, why)
}

// ============================================================
// jobs — reaps completed background jobs
// ============================================================

func TestE2E_Jobs_ReapsCompletedJob(t *testing.T) {
	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	bgJob := buildCompleterScript(t, "reap_e2e_marker", 300*time.Millisecond)
	delay := buildCompleterScript(t, "delay_marker", 700*time.Millisecond)

	session := fmt.Sprintf("'%s' > '%s/bg.txt' &\njobs\n'%s'\njobs\njobs\n", bgJob, dir, delay)
	why := "spec: the first jobs call shows the job Running with a trailing &; once it exits, the next jobs call shows it once as Done without the trailing &; the call after that shows nothing (it was removed)"

	got := runShell(t, binary, session)

	runningLine := fmt.Sprintf("[1]+  Running                 %s &", bgJob)
	doneLine := fmt.Sprintf("[1]+  Done                    %s", bgJob)

	assertContainsWhy(t, session, got, runningLine, why)
	assertContainsWhy(t, session, got, doneLine, why)

	count := strings.Count(got, doneLine)
	if count != 1 {
		t.Error(failLine(typedSession(session), "the Done line appears exactly once (then the job is removed)",
			fmt.Sprintf("appeared %d times", count), why))
	} else {
		t.Logf("%s %s\n    expected: the Done line appears exactly once (then the job is removed)\n    received: appeared once", markPass, typedSession(session))
	}
}

func TestE2E_Jobs_ReapsMultipleCompletedJobs(t *testing.T) {
	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	job1 := buildCompleterScript(t, "multi_reap_1", 5*time.Second)
	job2 := buildCompleterScript(t, "multi_reap_2", 300*time.Millisecond)
	job3 := buildCompleterScript(t, "multi_reap_3", 1200*time.Millisecond)
	delay1 := buildCompleterScript(t, "delay1_marker", 700*time.Millisecond)
	delay2 := buildCompleterScript(t, "delay2_marker", 900*time.Millisecond)

	session := fmt.Sprintf(
		"'%s' > '%s/j1.txt' &\n'%s' > '%s/j2.txt' &\n'%s' > '%s/j3.txt' &\n'%s'\njobs\n'%s'\njobs\njobs\n",
		job1, dir, job2, dir, job3, dir, delay1, delay2,
	)
	why := "spec: 'When jobs are removed, the markers shift' — job 1 starts with a space marker, is promoted to - once job 2 is reaped, then to + once job 3 is reaped too. " +
		"Since a later stage made reaping automatic before every prompt, job 2's and job 3's Done lines now appear on their own, right after the preceding command's output and before jobs is even read — so each jobs call below only ever lists what's still Running."

	got := runShell(t, binary, session)

	doneJob2 := fmt.Sprintf("[2]-  Done                    %s", job2)
	afterFirstJobsCall := fmt.Sprintf("[1]-  Running                 %s &\n[3]+  Running                 %s &", job1, job3)
	doneJob3 := fmt.Sprintf("[3]+  Done                    %s", job3)
	final := fmt.Sprintf("[1]+  Running                 %s &", job1)

	assertContainsWhy(t, session, got, doneJob2, why)
	assertContainsWhy(t, session, got, afterFirstJobsCall, why)
	assertContainsWhy(t, session, got, doneJob3, why)
	assertContainsWhy(t, session, got, final, why)
}

// ============================================================
// jobs — automatic reaping before each prompt
// ============================================================

func TestE2E_Jobs_AutomaticReapingBeforePrompt(t *testing.T) {
	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	job1 := buildCompleterScript(t, "auto_reap_1", 5*time.Second)
	job2 := buildCompleterScript(t, "auto_reap_2", 300*time.Millisecond)
	delayCmd := buildCompleterScript(t, "cmd_output_marker", 600*time.Millisecond)

	session := fmt.Sprintf("'%s' > '%s/j1.txt' &\n'%s' > '%s/j2.txt' &\n'%s'\njobs\n", job1, dir, job2, dir, delayCmd)
	why := "spec: 'Before printing the $ prompt... Display a Done line for each completed job... This means completed jobs appear automatically without needing to run jobs. The Done entries appear between the command output and the next prompt' — and afterward 'the jobs builtin no longer shows job [2] (already reaped)'"

	got := runShell(t, binary, session)

	doneLine := fmt.Sprintf("[2]+  Done                    %s", job2)
	runningLine := fmt.Sprintf("[1]+  Running                 %s &", job1)

	assertContainsWhy(t, session, got, doneLine, why)
	assertContainsWhy(t, session, got, runningLine, why)

	idxCmdOutput := strings.Index(got, "cmd_output_marker")
	idxDone := strings.Index(got, doneLine)
	idxJobsListing := strings.Index(got, runningLine)
	if idxCmdOutput == -1 || idxDone == -1 || idxJobsListing == -1 || !(idxCmdOutput < idxDone && idxDone < idxJobsListing) {
		t.Error(failLine(typedSession(session), "the Done line appears after the command's own output and before the jobs listing",
			fmt.Sprintf("cmd output at %d, Done line at %d, jobs listing at %d", idxCmdOutput, idxDone, idxJobsListing), why))
	} else {
		t.Logf("%s %s\n    expected: the Done line appears after the command's own output and before the jobs listing\n    received: correct order", markPass, typedSession(session))
	}

	count := strings.Count(got, doneLine)
	if count != 1 {
		t.Error(failLine(typedSession(session), "the Done line appears exactly once (shown automatically, not repeated by jobs)",
			fmt.Sprintf("appeared %d times", count), why))
	} else {
		t.Logf("%s %s\n    expected: the Done line appears exactly once (shown automatically, not repeated by jobs)\n    received: appeared once", markPass, typedSession(session))
	}
}

// ============================================================
// jobs — recycles job numbers
// ============================================================

func TestE2E_Jobs_RecyclesJobNumber(t *testing.T) {
	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())

	job1 := buildCompleterScript(t, "recycle_e2e_1", 5*time.Second)
	job2 := buildCompleterScript(t, "recycle_e2e_2", 300*time.Millisecond)
	delayCmd := buildCompleterScript(t, "recycle_delay_marker", 600*time.Millisecond)
	job3 := buildCompleterScript(t, "recycle_e2e_3", 5*time.Second)

	session := fmt.Sprintf("'%s' > '%s/j1.txt' &\n'%s' > '%s/j2.txt' &\n'%s'\n'%s' > '%s/j3.txt' &\njobs\n",
		job1, dir, job2, dir, delayCmd, job3, dir)
	why := "spec: 'Otherwise, assign one more than the highest job number currently in the table' — after job 2 exits and is reaped, job 1 remains as [1]; the next job started must recycle number [2], not jump to [3]"

	got := runShell(t, binary, session)

	job1Line := fmt.Sprintf("[1]-  Running                 %s &", job1)
	job3Line := fmt.Sprintf("[2]+  Running                 %s &", job3)

	assertContainsWhy(t, session, got, job1Line, why)
	assertContainsWhy(t, session, got, job3Line, why)
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

// buildCatScript compiles a tiny standalone program that copies stdin to
// stdout verbatim — a minimal, dependency-free stand-in for the system
// "cat" command, used as the downstream half of a pipeline in tests so they
// don't depend on a real "cat" being on PATH.
func buildCatScript(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	src := `package main

import (
	"io"
	"os"
)

func main() {
	io.Copy(os.Stdout, os.Stdin)
}
`
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write cat script source: %v", err)
	}

	binPath := filepath.Join(dir, "catlike")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build cat script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// buildLineHeadScript compiles a tiny standalone program that prints the
// first n lines read from stdin, one at a time, then exits — the same
// observable contract as "head -n N". It exists because this environment's
// real head (uutils coreutils) fully buffers its stdout until it exits,
// regardless of whether the destination is a terminal or a pipe (confirmed
// by piping a live "tail -f" into it directly, outside the shell: tail's
// own output reaches the pipe immediately, but nothing from head appears
// until it has already read all n lines). That buffering policy belongs to
// head, not to the shell being tested, so using it downstream would
// confound head's own behavior with whatever the shell's pipeline
// implementation does. Go's fmt.Println already writes straight through
// via an unbuffered os.File.Write, so this stand-in flushes every line the
// instant it is read, isolating exactly the property the streaming test
// cares about: whether the shell starts both commands concurrently.
func buildLineHeadScript(t *testing.T, n int) string {
	t.Helper()

	dir := t.TempDir()
	src := fmt.Sprintf(`package main

import (
	"bufio"
	"fmt"
	"os"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for count := 0; count < %d && scanner.Scan(); count++ {
		fmt.Println(scanner.Text())
	}
}
`, n)

	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("setup: cannot write line-head script source: %v", err)
	}

	binPath := filepath.Join(dir, "linehead")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", binPath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("setup: cannot build line-head script: %v\n%s", err, out)
	}
	return filepath.ToSlash(binPath)
}

// ============================================================
// pipelines (cmd1 | cmd2)
// ============================================================

func TestE2E_Pipeline_Basic(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}
	if _, err := exec.LookPath("wc"); err != nil {
		t.Skip("wc is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())
	file := dir + "/pipeline_input.txt"

	// 4 lines, 8 words — deliberately distinct counts so a passing
	// assertion on each number can't be satisfied by the other by accident.
	content := "red fox\nblue jay\ngreen frog\nyellow bee\n"
	if err := os.WriteFile(filepath.FromSlash(file), []byte(content), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file, err)
	}

	tests := []struct {
		name    string
		session string
		want    []string
		why     string
	}{
		{
			name:    "cat piped into wc reports line and word counts",
			session: fmt.Sprintf("cat '%s' | wc\n", file),
			want:    []string{"4", "8"},
			why:     "spec: 'A pipeline connects the standard output of one command to the standard input of the next command using the | operator' — example: '$ cat /tmp/foo/file | wc'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			for _, want := range tt.want {
				assertContainsWhy(t, tt.session, got, want, tt.why)
			}
		})
	}
}

// ============================================================
// pipelines with three or more stages
// ============================================================

func TestE2E_Pipeline_MultiStage(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}
	if _, err := exec.LookPath("head"); err != nil {
		t.Skip("head is not on PATH in this environment")
	}
	if _, err := exec.LookPath("tail"); err != nil {
		t.Skip("tail is not on PATH in this environment")
	}
	if _, err := exec.LookPath("wc"); err != nil {
		t.Skip("wc is not on PATH in this environment")
	}
	if _, err := exec.LookPath("ls"); err != nil {
		t.Skip("ls is not on PATH in this environment")
	}
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep is not on PATH in this environment")
	}

	binary := buildTestBinary(t)

	dir1 := filepath.ToSlash(t.TempDir())
	file := dir1 + "/pipeline_multistage_input.txt"
	// 5 lines; head -n 3 must actually truncate to the first 3 (3 lines, 6
	// words) before wc ever sees it, proving the truncation from stage 2
	// reaches stage 3 rather than the full file passing straight through.
	content := "red fox\nblue jay\ngreen frog\nyellow bee\npurple owl\n"
	if err := os.WriteFile(filepath.FromSlash(file), []byte(content), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file, err)
	}

	// ls -la on an empty directory containing exactly these 6 files prints,
	// in order: "total N", ".", "..", then the 6 names sorted alphabetically
	// — 9 lines total. tail -n 5 keeps the last 5 (everything from
	// bbb_file2.txt on); head -n 3 of that keeps bbb_file2.txt,
	// ccc_other.txt, ddd_file3.txt; grep "file" then keeps only the two
	// whose name contains "file", dropping ccc_other.txt — so a pipeline
	// that only passed data straight through, or dropped the wrong window,
	// would show a different pair (or all six) instead.
	dir2 := filepath.ToSlash(t.TempDir())
	for _, name := range []string{"aaa_file1.txt", "bbb_file2.txt", "ccc_other.txt", "ddd_file3.txt", "eee_other2.txt", "fff_file4.txt"} {
		if err := os.WriteFile(filepath.FromSlash(dir2+"/"+name), nil, 0o644); err != nil {
			t.Fatalf("setup: cannot create %q: %v", dir2+"/"+name, err)
		}
	}

	tests := []struct {
		name        string
		session     string
		wantContain []string
		wantAbsent  []string
		why         string
	}{
		{
			name:        "three stages: cat | head -n 3 | wc",
			session:     fmt.Sprintf("cat '%s' | head -n 3 | wc\n", file),
			wantContain: []string{"3", "6"},
			why:         "spec example: '$ cat /tmp/foo/file | head -n 3 | wc' → '3 3 10' (3 lines, 6 words counted here since char count is platform-dependent, matching TestE2E_Pipeline_Basic's own convention)",
		},
		{
			name:        "four stages: ls -la | tail -n 5 | head -n 3 | grep \"file\"",
			session:     fmt.Sprintf("ls -la '%s' | tail -n 5 | head -n 3 | grep \"file\"\n", dir2),
			wantContain: []string{"bbb_file2.txt", "ddd_file3.txt"},
			wantAbsent:  []string{"aaa_file1.txt", "ccc_other.txt", "eee_other2.txt", "fff_file4.txt"},
			why:         `spec example: '$ ls -la /tmp/foo | tail -n 5 | head -n 3 | grep "file"' — only the entries inside the tail/head window whose name matches "file" survive to the final stage`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			for _, want := range tt.wantContain {
				assertContainsWhy(t, tt.session, got, want, tt.why)
			}
			for _, absent := range tt.wantAbsent {
				wantEqual(t, typedSession(tt.session), fmt.Sprintf("%v", strings.Contains(got, absent)), "false", tt.why)
			}
		})
	}
}

// TestE2E_Pipeline_MultiStage_Builtin confirms that a longer chain still
// handles a builtin correctly wherever it sits, not just as one side of
// exactly two stages — extending TestE2E_Pipeline_Builtins the same way
// TestE2E_Pipeline_MultiStage extends TestE2E_Pipeline_Basic.
func TestE2E_Pipeline_MultiStage_Builtin(t *testing.T) {
	if _, err := exec.LookPath("wc"); err != nil {
		t.Skip("wc is not on PATH in this environment")
	}
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())
	file := dir + "/pipeline_multistage_builtin_input.txt"
	if err := os.WriteFile(filepath.FromSlash(file), []byte("unused\n"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file, err)
	}

	tests := []struct {
		name    string
		session string
		want    []string
		why     string
	}{
		{
			name:    "builtin producer feeds two external stages",
			session: "type echo | cat | wc\n",
			want:    []string{"1", "5", "24"},
			why:     "built-in commands must be handled correctly wherever they appear in a pipeline — a producer builtin followed by two external stages, not just one",
		},
		{
			name:    "builtin in the middle of an external-builtin-external-shaped chain",
			session: fmt.Sprintf("cat '%s' | type exit | cat\n", file),
			want:    []string{"exit is a shell builtin"},
			why:     "a builtin mid-chain still discards its predecessor's output and its own result still reaches the final stage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			for _, want := range tt.want {
				assertContainsWhy(t, tt.session, got, want, tt.why)
			}
		})
	}
}

func TestE2E_Pipeline_MustFail(t *testing.T) {
	if _, err := exec.LookPath("wc"); err != nil {
		t.Skip("wc is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())
	file := dir + "/pipeline_fail_input.txt"
	if err := os.WriteFile(filepath.FromSlash(file), []byte("content\n"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file, err)
	}

	tests := []struct {
		name    string
		session string
		want    string
		why     string
	}{
		{
			name:    "left command not on PATH",
			session: "nosuchcmd12345 | wc\n",
			want:    "not found",
			why:     "both halves of a pipeline are external commands resolved from PATH, same as any other command",
		},
		{
			name:    "right command not on PATH",
			session: fmt.Sprintf("cat '%s' | nosuchcmd12345\n", file),
			want:    "not found",
			why:     "both halves of a pipeline are external commands resolved from PATH, same as any other command",
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
// pipeline streaming — interactive shell test infrastructure
//
// The tail -f | head example requires appending to a file WHILE the
// pipeline is still running (the shell is blocked inside a single
// foreground command the whole time), which the static-session runShell
// helper above cannot exercise: it feeds a whole session upfront and only
// inspects output after the shell process has fully exited. interactiveShell
// instead starts the shell with real, live stdin/stdout pipes so a test can
// write to it and read from it while it is still executing.
// ============================================================

// interactiveShell wraps a live shell process. Output is drained into a
// mutex-guarded buffer by a background goroutine rather than read line by
// line: the "$ " prompt has no trailing newline, so a line-oriented scanner
// would never flush it.
type interactiveShell struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	mu    sync.Mutex
	buf   strings.Builder
	done  chan struct{}
}

func startInteractiveShell(t *testing.T, binary string) *interactiveShell {
	t.Helper()

	cmd := exec.Command(binary)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("setup: cannot open shell stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("setup: cannot open shell stdout: %v", err)
	}
	cmd.Stderr = nil // discard the EOF panic trace, same as runShell

	if err := cmd.Start(); err != nil {
		t.Fatalf("setup: cannot start shell: %v", err)
	}

	sh := &interactiveShell{cmd: cmd, stdin: stdin, done: make(chan struct{})}

	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := stdout.Read(buf)
			if n > 0 {
				sh.mu.Lock()
				sh.buf.Write(buf[:n])
				sh.mu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}()

	go func() {
		cmd.Wait()
		close(sh.done)
	}()

	return sh
}

// send writes line plus a trailing newline to the shell's stdin, as if typed.
func (sh *interactiveShell) send(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(sh.stdin, line+"\n"); err != nil {
		t.Fatalf("setup: cannot write to shell stdin: %v", err)
	}
}

func (sh *interactiveShell) snapshot() string {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.buf.String()
}

// waitForOutput polls (rather than blocking on a channel) since output can
// arrive as arbitrary byte chunks, not discrete messages; it returns true as
// soon as want appears anywhere in everything read so far, or false once
// timeout elapses.
func (sh *interactiveShell) waitForOutput(want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if strings.Contains(sh.snapshot(), want) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// close closes the shell's stdin (as EOF would) and waits for it to exit,
// force-killing it if it doesn't — so a bug under test fails this test
// instead of leaking a hung process into the rest of the suite.
func (sh *interactiveShell) close(t *testing.T) {
	t.Helper()
	sh.stdin.Close()
	select {
	case <-sh.done:
	case <-time.After(5 * time.Second):
		sh.cmd.Process.Kill()
		<-sh.done
	}
}

func TestE2E_Pipeline_Streaming(t *testing.T) {
	if _, err := exec.LookPath("tail"); err != nil {
		t.Skip("tail is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "stream.txt")

	initial := "raspberry strawberry\npear mango\npineapple apple\n"
	if err := os.WriteFile(file, []byte(initial), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", file, err)
	}

	appendLine := func(s string) {
		t.Helper()
		f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatalf("setup: cannot append to %q: %v", file, err)
		}
		defer f.Close()
		if _, err := f.WriteString(s + "\n"); err != nil {
			t.Fatalf("setup: cannot append to %q: %v", file, err)
		}
	}

	sh := startInteractiveShell(t, binary)
	defer sh.close(t)

	// linehead stands in for "head -n 5" — see buildLineHeadScript for why
	// this environment's real head can't be used to observe streaming.
	linehead := buildLineHeadScript(t, 5)
	session := fmt.Sprintf("tail -f '%s' | '%s'\n", filepath.ToSlash(file), linehead)
	why := "spec: 'For the tail -f command, the tester will check if the running command keeps printing new lines' — appended lines must reach head while tail is still running (both commands started concurrently via Start, not one Run to completion before the next begins), not only after tail eventually exits"
	sh.send(t, session)

	for _, want := range []string{"raspberry strawberry", "pear mango", "pineapple apple"} {
		if !sh.waitForOutput(want, 4*time.Second) {
			t.Fatal(failLine(typedSession(session), "line containing "+show(want)+" (pre-existing file content)", "(timed out waiting)", why))
		}
	}

	appendLine("This is line 4.")
	if !sh.waitForOutput("This is line 4.", 4*time.Second) {
		t.Fatal(failLine(typedSession(session), "\"This is line 4.\" appearing live after being appended mid-command", "(timed out waiting)", why))
	}

	appendLine("This is line 5.")
	if !sh.waitForOutput("This is line 5.", 4*time.Second) {
		t.Fatal(failLine(typedSession(session), "\"This is line 5.\" appearing live after being appended mid-command", "(timed out waiting)", why))
	}

	// head has now read its 5 lines and exited; tail only notices the
	// closed pipe (SIGPIPE) on its own next write attempt, so one more
	// append is needed to trigger that and let the pipeline — and the
	// prompt — return.
	appendLine("trigger line for pipe teardown")

	terminationWhy := "spec: 'The tester will check if the final output matches the expected output after pipeline execution' — once head is satisfied, the whole pipeline (including tail) must terminate and control must return to the shell"
	if !sh.waitForOutput("$", 4*time.Second) {
		t.Fatal(failLine(typedSession(session), "prompt reappears once the pipeline terminates", "(timed out waiting)", terminationWhy))
	}
}

// ============================================================
// pipelines with builtins
// ============================================================

func TestE2E_Pipeline_Builtins(t *testing.T) {
	if _, err := exec.LookPath("wc"); err != nil {
		t.Skip("wc is not on PATH in this environment")
	}
	if _, err := exec.LookPath("ls"); err != nil {
		t.Skip("ls is not on PATH in this environment")
	}

	binary := buildTestBinary(t)

	tests := []struct {
		name    string
		session string
		want    []string
		why     string
	}{
		{
			name:    "builtin at the beginning of a pipeline, piped into an external command",
			session: "echo apple-orange | wc\n",
			want:    []string{"1", "1", "13"},
			why:     "spec example: '$ echo apple-orange | wc' reports 1 line, 1 word, 13 characters",
		},
		{
			name:    "external command piped into a builtin at the end of a pipeline",
			session: "ls | type exit\n",
			want:    []string{"exit is a shell builtin"},
			why:     "spec example: '$ ls | type exit' → 'exit is a shell builtin'",
		},
		{
			name:    "another builtin at the beginning of a pipeline",
			session: "type echo | wc\n",
			want:    []string{"1", "5", "24"},
			why:     "built-in commands need to be handled correctly wherever they appear in a pipeline, not just echo — here type is the producer",
		},
		{
			name:    "a failing builtin at the end of a pipeline reports its own error",
			session: "echo hi | cd definitely-not-a-real-subdirectory\n",
			want:    []string{"No such directory"},
			why:     "a builtin's own failure, when it is the pipeline's last stage, is the pipeline's result — same as any other command's failure would be",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runShell(t, binary, tt.session)
			for _, want := range tt.want {
				assertContainsWhy(t, tt.session, got, want, tt.why)
			}
		})
	}
}

// TestE2E_Pipeline_Builtins_ProducerOutputHidden is split out from the table
// above because, unlike a plain "does the output contain X" check, it also
// has to prove a negative: ls's own listing must never reach the terminal
// when type is what actually consumes the pipeline's result.
func TestE2E_Pipeline_Builtins_ProducerOutputHidden(t *testing.T) {
	if _, err := exec.LookPath("ls"); err != nil {
		t.Skip("ls is not on PATH in this environment")
	}

	binary := buildTestBinary(t)
	dir := filepath.ToSlash(t.TempDir())
	marker := "pipeline_builtin_marker_file.txt"
	markerPath := dir + "/" + marker
	if err := os.WriteFile(filepath.FromSlash(markerPath), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: cannot create %q: %v", markerPath, err)
	}

	session := fmt.Sprintf("cd '%s'\nls | type exit\n", dir)
	why := "spec: '$ ls | type exit' example — 'the ls output is not supposed to be printed', only type's own result"

	got := runShell(t, binary, session)

	assertContainsWhy(t, session, got, "exit is a shell builtin", why)
	wantEqual(t, typedSession(session), fmt.Sprintf("%v", strings.Contains(got, marker)), "false", why)
}
