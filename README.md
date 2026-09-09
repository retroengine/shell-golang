# Unix - shell

![Go version](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)
![Tests](https://img.shields.io/badge/tests-unit%20%C2%B7%20e2e%20%C2%B7%20fuzz-blue)

A small Unix shell written from scratch in Go. It has no `readline` library and
never shells out to `/bin/sh` — it reads your keystrokes one at a time, parses
quotes and escapes itself, completes words with Tab, runs programs, and
redirects their output to files. It works the same on Linux, macOS, and Windows.

```
$ echo 'hello    world'
hello    world
$ echo "say \"hi\""
say "hi"
$ pwd > where.txt
$ type cd
cd is a shell builtin
$ git st<TAB>
status
```

## Quick start

You need **Go 1.26 or later**. Nothing else.

```sh
git clone <this-repo>
cd shell-golang/app
go run .
```

```powershell
git clone <this-repo>
cd shell-golang\app
go run .
```

You get a `$ ` prompt. Type commands; type `exit` to leave.

---

## What it can do

### Builtins

Six commands are handled by the shell itself:

| Command | What it does |
|---|---|
| `echo [words...]` | Prints its arguments, separated by single spaces. |
| `pwd` | Prints the current directory. |
| `cd [path]` | Changes directory. A bare `~` goes to your home directory. |
| `type <name>` | Says whether `name` is a builtin, an external program (showing its full path), or not found. |
| `complete` | Registers, shows, or removes a custom Tab-completion script — see [below](#custom-tab-completion-complete). |
| `exit` | Quits the shell. |

Anything that is not on this list is looked up on your `PATH` and run as a
normal program, with its input and output connected straight through to yours.

### Quotes and escapes

| You type | What happens |
|---|---|
| `'...'` | Everything inside is literal. Even backslashes. |
| `"..."` | Literal too, except `\"` becomes `"` and `\\` becomes `\`. |
| `\x` | Outside quotes, keeps the next character as-is — including a space. |
| `'ab''cd'` | Quoted pieces touching each other join into one argument. |

```sh
$ echo 'hello    world'      # spaces kept inside single quotes
hello    world
$ echo "say \"hi\""          # \" becomes a real quote
say "hi"
$ echo hello\ world          # backslash protects the space
hello world
$ echo 'foo''bar'            # touching quotes join up
foobar
```

### Redirecting output

| Operator | Effect |
|---|---|
| `>` or `1>` | Send normal output to a file, replacing it. |
| `>>` or `1>>` | Send normal output to a file, adding to the end. |
| `2>` | Send error output to a file, replacing it. |
| `2>>` | Send error output to a file, adding to the end. |

This works the same for builtins and for external programs:

```sh
$ echo hello > out.txt
$ cat out.txt
hello
$ ls /no/such/dir 2>> errors.log
$ pwd 1>> session.log
```

### Tab completion

Press `Tab` and the shell looks for matches, trying these sources in order:

1. **A completion script you registered** for this command (see below).
2. **A built-in list** of ~150 common Unix command names — used for the first
   word you type.
3. **Real programs on your `PATH`** — also for the first word.
4. **Files and folders in the current directory** — for everything after the
   first word.

However many matches come back, they resolve the same way:

| Matches found | What happens |
|---|---|
| None | The terminal beeps. Your input is untouched. |
| Exactly one | It is filled in, plus a space (or a `/` if it's a folder). |
| Several sharing a longer start | Filled in as far as they agree, like bash. |
| Several, first `Tab` | Just a beep. |
| Several, second `Tab` | All of them are listed. |
| Several, further `Tab`s | Cycles through them one at a time, like PowerShell. |

### Custom Tab completion (`complete`)

You can teach the shell how to complete arguments for any command by pointing it
at a small program of your own.

| Command | What it does |
|---|---|
| `complete -C <script> <command>` | Use `<script>` to complete `<command>`'s arguments. Prints nothing. |
| `complete -p <command>` | Show what is registered for `<command>`. |
| `complete -r <command>` | Remove it again. Prints nothing. |

Your script is run as `script <command> <word-being-typed> <word-before-it>`,
with `COMP_LINE` (the whole line so far) and `COMP_POINT` (the cursor position)
in its environment. It should print **one suggestion per line**.

```sh
#!/bin/sh
# git-completer — suggest git subcommands starting with "$2"
for c in add commit push; do
  case "$c" in "$2"*) echo "$c" ;; esac
done
```

```sh
$ complete -C /path/to/git-completer git
$ complete -p git
complete -C '/path/to/git-completer' git
$ git c<TAB>
commit
$ complete -r git
```

Registrations live in memory only — they are gone when the shell exits.

---

## How it works

Five source files, each with one job:

| File | Job |
|---|---|
| [`app/main.go`](app/main.go) | The prompt loop, and the `switch` that decides which handler runs. |
| [`app/parser.go`](app/parser.go) | Turns one typed line into a list of arguments, applying the quote and escape rules. |
| [`app/builtins.go`](app/builtins.go) | The six builtin commands, plus running external programs. |
| [`app/redirect.go`](app/redirect.go) | Spots `>` `>>` `2>` `2>>` and writes output to the file. |
| [`app/autocomplete.go`](app/autocomplete.go) | Reads keystrokes in raw mode and does everything Tab-related. |

**Want the detail?** [`docs/internals.md`](docs/internals.md) walks through every
function — what it receives, what it decides, what it returns — with a flowchart
of one command from keystroke to output.

## Testing

Three layers, 106 test functions in total:

| Layer | File | What it checks |
|---|---|---|
| **Unit** | `app/main_test.go` | 53 table-driven tests, one function per handler, so a `handleCD` bug shows up as a `handleCD` failure. |
| **End-to-end** | `app/e2e_test.go` | 50 tests that build the real binary, type a whole session into it, and check what came back. |
| **Fuzz** | `app/fuzz_test.go` | 3 targets throwing random input at the parser and builtins, looking for crashes. |

Every check goes through one shared reporter, so a test run reads like a shell
session rather than a wall of stack traces:

```
✓ echo saikiran
    expected: saikiran
    received: saikiran

✗ echo it's
    expected: its
    received: it's
    why:      spec: single quotes are stripped from every argument
```

### Running them

```sh
./test.sh [mode]        # bash / Git Bash / Linux / macOS
```
```powershell
.\test.ps1 [mode]       # PowerShell
```

| Mode | Runs |
|---|---|
| `unit` | The unit tests only. |
| `e2e` | The end-to-end tests only. |
| `all` *(default)* | `go vet` plus everything. |
| `cover` | Everything, plus an HTML coverage report at `app/coverage.html`. |
| `strict` | Shuffled and repeated 3× — catches ordering bugs and leaked state. |
| `fuzz [duration]` | Each fuzz target, 30s by default. |

Set `NO_COLOR=1` to drop the colours, or `SHELL_TEST_ASCII=1` to swap `✓`/`✗`
for `[PASS]`/`[FAIL]` on consoles that can't render them.

## Known limitations

- **One redirect per command.** `cmd > out.txt 2> err.txt` only honours the
  first operator it finds.
- **No pipes or chaining.** `|`, `&&`, `||`, `;` and background `&` are not
  supported — one line is one command.
- **No variables or wildcards.** `$VAR` and `*.txt` are passed through as
  literal text.
- **No input redirection.** `< file` is not supported, only the output forms.
- **`~` only works alone.** `cd ~` is fine, but `~/dir` and `~user` are not
  expanded. On Windows, `HOME` isn't set by default outside Git Bash or WSL, so
  `cd ~` needs it set first.
- **Builtins always win.** A real `echo` or `pwd` program on your `PATH` is
  never reached.
- **Completion doesn't understand quotes.** It splits on plain spaces, so a
  half-typed quoted filename won't match anything. (A Tab pressed *inside* a
  quote is correctly inserted as a real tab, though.)
- **`type complete` says "not found."** The builtin is registered under a name
  with a stray trailing space ([main.go:18](app/main.go#L18)), so `type` misses
  it. `complete` itself works fine.
- **Custom completions list too eagerly.** When a registered script returns
  several suggestions that share no common start, the first `Tab` beeps *and*
  lists them, instead of waiting for a second `Tab` like the other sources do.
  ([details](docs/internals.md#known-quirks-in-the-current-code))

## About the tests

The test suite is written to a spec, not to the implementation:
[`CLAUDE.md`](CLAUDE.md) tells Claude Code how each feature's tests should be
derived from its written rules, which table each case belongs in, and how to
word the `why` line the reporter prints on failure. That is why every test block
here has the same shape.

## License

No license file is included yet — all rights reserved by default until one is
added.
