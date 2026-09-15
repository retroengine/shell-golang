# shell-golang

![Go version](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)
![Tests](https://img.shields.io/badge/tests-178-blue)

A small Unix shell written from scratch in Go. No `readline` library, and it never shells
out to `/bin/sh` — it reads your keystrokes one at a time, parses quotes and escapes
itself, completes words with Tab, and runs pipelines and background jobs of its own.

```console
$ echo one two three | tr ' ' '\n' | sort -r
two
three
one
$ sleep 5 &
[1] 14732
$ jobs
[1]+  Running                 sleep 5 &
```

## Quick start

You need **Go 1.26 or later**. Nothing else.

```sh
git clone <this-repo>
cd shell-golang/app
go run .
```

You get a `$ ` prompt. Type commands; type `exit` to leave.

> CI runs on Linux. macOS and Windows both work but aren't CI-verified — on Windows, use
> `.\test.ps1` wherever this README says `./test.sh`.

## User guide

One session, with everything worth knowing in it:

```sh
$ echo 'keeps    spaces'     # single quotes: every character is literal
keeps    spaces
$ echo "say \"hi\""          # double quotes: only \" and \\ are special
say "hi"
$ echo hello\ world          # a backslash protects the next character
hello world

$ pwd > where.txt            # > truncates, >> appends, 2> / 2>> do errors
$ cat where.txt | wc -l      # pipe through as many stages as you like
1

$ sleep 5 &                  # trailing & runs it in the background
[1] 14732
$ jobs                       # what's still going
[1]+  Running                 sleep 5 &
[1]+  Done                    sleep 5     # finished jobs report before the next prompt

$ history                    # numbered, bash-style
    1  echo 'keeps    spaces'
$ ec<TAB>                    # Tab completes; ↑ / ↓ walk back through history
$ exit
```

## Features

### Builtins

Eight commands are handled by the shell itself. Anything else is looked up on your `PATH`
and run as a normal program, wired straight through to your terminal.

| Command | What it does |
|---|---|
| `echo [words...]` | Prints its arguments, separated by single spaces. |
| `pwd` | Prints the current directory. |
| `cd [path]` | Changes directory. A bare `~` goes to your home directory. |
| `type <name>` | Says whether `name` is a builtin, an external program (with its path), or not found. |
| `jobs` | Lists background jobs. |
| `history [n]` | Shows past commands; `-r <file>` / `-w <file>` load and save them. |
| `complete` | Registers or removes a Tab-completion script — see [below](#custom-tab-completion). |
| `exit` | Quits the shell. |

### Quoting and escapes

| You type | What happens |
|---|---|
| `'...'` | Everything inside is literal — even backslashes. |
| `"..."` | Literal too, except `\"` becomes `"` and `\\` becomes `\`. |
| `\x` | Outside quotes, keeps the next character as-is — including a space. |
| `'ab''cd'` | Quoted pieces touching each other join into one argument (`abcd`). |

### Redirection

`>` and `1>` replace a file with the command's normal output; `>>` and `1>>` append to it.
`2>` and `2>>` do the same for error output. Builtins and external programs both honour
them, and a redirect with no command in front of it (`> out.txt`) just creates or
truncates the file.

### Pipelines

`a | b | c` — any number of stages, each one's output feeding the next.

Builtins work in a pipeline too, running in-process rather than as a separate program. One
at the *end* of a pipe prints normally; one in the **middle** discards what reaches it,
since none of this shell's builtins read stdin. An `exit` mid-pipeline is a no-op rather
than a shutdown, matching how real shells run those stages in a subshell.

### Background jobs

End a command with `&` and the shell starts it, prints `[job] pid`, and returns to the
prompt. In `jobs` output, `+` marks the most recent job and `-` the one before it.
Finished jobs are reported as `Done` and dropped — when you run `jobs`, or automatically
just before the next prompt.

### Tab completion

<kbd>Tab</kbd> looks for matches in this order: a **completion script** you registered for
the command, then a **built-in list** of ~150 common Unix command names, then real
programs on your **`PATH`** (those three for the first word only), then **files and
folders** in the current directory for every word after it.

| Matches found | What happens |
|---|---|
| None | The terminal beeps. Your input is untouched. |
| Exactly one | It is filled in, plus a space (or a `/` if it's a folder). |
| Several sharing a longer start | Filled in as far as they agree, like bash. |
| Several | First <kbd>Tab</kbd> beeps, second lists them, then further presses cycle. |

### Custom Tab completion

`complete -C <script> <command>` points a command's argument completion at a program of
your own; `complete -p <command>` shows what's registered and `-r` removes it.
Registrations live in memory only.

Your script runs as `script <command> <word-being-typed> <word-before-it>`, with
`COMP_LINE` and `COMP_POINT` in its environment, and should print one suggestion per line.

```sh
#!/bin/sh
# git-completer — suggest git subcommands starting with "$2"
for c in add commit push; do
  case "$c" in "$2"*) echo "$c" ;; esac
done
```

## How it works

Six source files, each with one job:

| File | Job |
|---|---|
| [`app/main.go`](app/main.go) | The prompt loop, and the `switch` that picks a handler. |
| [`app/parser.go`](app/parser.go) | Turns one typed line into arguments, applying the quote and escape rules. |
| [`app/builtins.go`](app/builtins.go) | The eight builtins, plus running external programs and tracking jobs. |
| [`app/redirect.go`](app/redirect.go) | Spots `>` `>>` `2>` `2>>` and writes output to the file. |
| [`app/pipeline.go`](app/pipeline.go) | Splits on `\|` and wires the stages together. |
| [`app/autocomplete.go`](app/autocomplete.go) | Raw-mode keystrokes; everything Tab- and history-key related. |

## Testing

**178 test functions** across three layers: 98 unit tests covering every handler
(`app/main_test.go`), 77 end-to-end tests that build the real binary and type a whole
session into it (`app/e2e_test.go`), and 3 fuzz targets hunting for parser crashes
(`app/fuzz_test.go`).

Every check goes through one shared reporter, so a run reads like a shell session rather
than a wall of stack traces:

```
✗ echo it's
    expected: its
    received: it's
    why:      spec: single quotes are stripped from every argument
```

```sh
./test.sh [mode]        # Linux / macOS / Git Bash
.\test.ps1 [mode]       # PowerShell
```

| Mode | Runs |
|---|---|
| `unit` | Every test except the end-to-end ones. |
| `e2e` | The end-to-end tests only. |
| `all` *(default)* | `go vet` plus everything. |
| `cover` | Everything, plus an HTML report at `app/coverage.html`. |
| `strict` | Shuffled and repeated 3× — catches ordering bugs and leaked state. |
| `fuzz [duration]` | Each fuzz target, 30s by default. |

`NO_COLOR=1` drops the colours; `SHELL_TEST_ASCII=1` swaps `✓`/`✗` for `[PASS]`/`[FAIL]`.

Tests are written to a spec rather than to the implementation — [`CLAUDE.md`](CLAUDE.md)
sets out how each feature's cases are derived from its written rules.

## Limitations

- **One redirect per command.** `cmd > out.txt 2> err.txt` only honours the first operator.
- **No variables or wildcards.** `$VAR` and `*.txt` are passed through as literal text.
- **No input redirection.** `< file` isn't supported, only the output forms.
- **`~` only works alone.** `cd ~` is fine; `~/dir` and `~user` are not expanded. Windows
  doesn't set `HOME` outside Git Bash or WSL, so `cd ~` needs it set first.
- **Completion doesn't understand quotes.** It splits on plain spaces, so a half-typed
  quoted filename won't match.
