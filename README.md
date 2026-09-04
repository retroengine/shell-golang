# shell-golang

A minimal Unix-style shell written in Go. It reads commands from stdin, parses
them (quoting and escaping included), and dispatches each one to a builtin
handler or an external program.

## Features

### Builtins

| Command | Behaviour |
|---|---|
| `echo [args...]` | Prints its arguments joined by a single space. |
| `pwd` | Prints the current working directory. |
| `cd [path\|~]` | Changes directory. `~` resolves to `$HOME`. Errors if the target doesn't exist. |
| `type <name>` | Reports whether `name` is a shell builtin, an external command (with its resolved path), or not found. |
| `exit` | Recognised as a builtin by `type`, but does not currently terminate the shell — see [Known limitations](#known-limitations). |

Anything else is looked up on `PATH` and run as an external command, with
stdin/stdout/stderr connected straight through to the shell's own.

### Quoting and escaping

| Syntax | Behaviour |
|---|---|
| `'...'` | Everything inside is literal — no escape sequences are processed. |
| `"..."` | Literal, except `\"` and `\\`, which become `"` and `\`. Any other `\x` keeps both characters. |
| `\x` (outside quotes) | Escapes the next character, including a space. |

Examples:

```sh
$ echo 'hello    world'
hello    world
$ echo "say \"hi\""
say "hi"
$ echo hello\ world
hello world
$ type echo
echo is a shell builtin
$ type go
go is /usr/local/go/bin/go
```

### Known limitations

- `exit` is recognised by `type` but does not stop the read loop (`break`
  inside its `switch` case only exits the switch, not the `for` loop). The
  process currently only ends when stdin is closed.

## What's implemented

Under the hood, each piece of behaviour is its own function in `main.go`:

- **`handleInput`** — reads one line and splits it into words, handling
  single quotes, double quotes, and backslash escaping along the way. This
  is the parser everything else depends on.
- **`handleEcho`** — joins the words after `echo` back into one line and
  returns it.
- **`handlePWD`** — asks the OS for the current directory and returns it.
- **`handleCD`** — changes the current directory, including `~` for home,
  and returns a clear error if the target doesn't exist.
- **`handleTYPE`** — looks a name up: is it a builtin, a program on `PATH`,
  or nothing at all?
- **`handleExecFile`** — finds an external program on `PATH` and runs it,
  connecting its input/output straight to the shell's own.
- **`main`** — the loop that ties it all together: print a prompt, read a
  line, parse it, and hand it to the right function above.

Every one of these is done and covered by tests (see below).

## Project structure

```
app/
  main.go          the shell implementation (builtins + main loop)
  main_test.go     unit tests, one block per handler function
  e2e_test.go      end-to-end tests (build the binary, pipe stdin, check stdout)
  report_test.go   shared test reporter + assertion helpers
  fuzz_test.go     fuzz targets
  go.mod
docs/              notes on specific fixes/changes
CLAUDE.md          instructions for generating tests with Claude Code
test.sh            test runner (bash / Git Bash)
test.ps1           test runner (PowerShell)
```

## Requirements

Go 1.26 or later.

## Build & run

From `app/`:

```sh
cd app
go build ./...
go run .
```

## Testing

### Kinds of tests

There are three layers, each catching different kinds of mistakes:

- **Unit tests** (`app/main_test.go`) — test one function at a time. Each
  function gets a short list of inputs and the output each one should
  produce, so a change that breaks `handleCD` shows up as a `handleCD`
  failure, not a mystery. There are around 30 of these groups in total —
  most for `handleInput`, since parsing quotes and backslashes correctly is
  the trickiest part of the shell.
- **End-to-end tests** (`app/e2e_test.go`) — build the actual shell binary
  and "type" a whole session into it, the way a person would at a real
  prompt, then check what it printed back. There are 23 of these, one per
  real-world scenario (running `echo`, quoting, changing directories,
  running an external program, and so on).
- **Fuzz tests** (`app/fuzz_test.go`) — instead of fixed inputs, these throw
  thousands of random and mutated strings at the parser to try to make it
  crash. There are 3 fuzz targets, covering input parsing, `echo`, and
  `type`.

### How a test run reads

Every check — in every layer — goes through the same reporter, so a test run
reads like a transcript of a shell session rather than a wall of Go errors.
A passing check looks like this:

```
✓ echo saikiran
    expected: saikiran
    received: saikiran
```

A failing one adds a plain-English reason, so you know *why* it mattered,
not just that it broke:

```
✗ echo it's
    expected: its
    received: it's
    why:      spec: single quotes are stripped from every argument
```

Passing checks (✓) only print when you run with `-v`, which is why the test
scripts below always pass it.

### Running the tests

Run via the wrapper script for your shell — both drive `go test` under the hood.

```sh
./test.sh [mode]      # bash / Git Bash / macOS / Linux
```
```powershell
.\test.ps1 [mode]     # PowerShell
```

| Mode | Runs |
|---|---|
| `unit` | `TestHandle*` only |
| `e2e` | `TestE2E*` only |
| `all` (default) | `go vet` + every test |
| `cover` | all tests + an HTML coverage report at `app/coverage.html` |
| `strict` | shuffled, repeated 3× — catches ordering/state leaks |
| `fuzz` | fuzz targets, 30s each by default |

Set `NO_COLOR=1` to drop ANSI colour in test output, or `SHELL_TEST_ASCII=1`
to swap the ✓/✗ marks for `[PASS]`/`[FAIL]`.

CI (`.github/workflows/test.yml`) runs vet, build, and tests across Ubuntu,
Windows, and macOS, plus a separate race-detector job, a fuzz job per target,
and a coverage job.
