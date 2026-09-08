# CLAUDE.md — Shell Project Test Generation Context

## What this project is

A minimal Unix shell written in Go (`main.go`). It reads commands from stdin,
parses them, and dispatches to builtin handlers or external programs.

This file tells you how to generate tests whenever a new task is completed.

---

## Project file structure

```
app/
  main.go          ← entry point: the main loop + command dispatch
  parser.go        ← handleInput (tokenizer)
  builtins.go      ← builtin handlers (echo, pwd, cd, type, exec)
  redirect.go      ← redirect parsing + output/error writers
  autocomplete.go  ← Tab completion + readLine
  main_test.go     ← unit tests  (one test block per handler function)
  e2e_test.go      ← end-to-end tests (compile binary, pipe stdin, check stdout)
  report_test.go   ← the shared reporter + every assertion helper (do not modify)
  fuzz_test.go     ← fuzz targets (do not modify unless explicitly asked)
  go.mod
```

---

## How the user will give you work

The user will share two things:

**1. The task spec** — a description of the feature being added. It includes:
- What the feature does (rules, behaviour)
- A table of example commands and their expected output
- Sometimes notes about external commands involved

**2. Their method implementation** — the Go function(s) they wrote or modified.

### How the implementation may arrive

The user can share their code in three ways — treat all of them identically:

| How they share it | What to do |
|---|---|
| Pasted inline in the message | Read it directly from the message |
| Uploaded as a `.go` file | Read it from `/mnt/user-data/uploads/<filename>` |
| Says "I wrote it in `<file>.go`" or "check my `<file>.go`" | Read it from `/mnt/user-data/uploads/<file>.go` |

**Never ask the user to paste code they already uploaded or named.** If a file path is mentioned or a file is attached, read it yourself before responding.

---

## Teacher Mode

Teacher Mode changes how you interact with the user across **two phases**: task explanation and code diagnosis. It is separate from test generation — tests are still generated as normal at the end.

### Activating Teacher Mode

Teacher Mode is **on** whenever the user's message starts with or contains `[TEACH]` or `teacher mode`.
Teacher Mode is **off** by default and stays off until explicitly activated.
Once on, it stays on for the rest of the conversation unless the user says `[TEACH OFF]`.

---

### Phase 1 — Task Explanation (when the user gives a task in Teacher Mode)

Before writing any code or tests, explain the task as a patient teacher would to a Go beginner.

Your explanation must cover all of these, in this order:

#### 1. What the task is asking for (plain English)
Restate the task in your own words. Strip the jargon. One short paragraph.

#### 2. Key concept(s) involved
Name the Go concepts or Unix/shell concepts the user will need.
For each concept, give a one-sentence definition and a tiny concrete example if it helps.

Examples of things to cover depending on the task:
- Parsing (tokenising, state machines, quote rules)
- I/O redirection (file descriptors, `os.Create`, `os.Stderr`)
- Process execution (`exec.Command`, `os.Exec`, PATH lookup)
- String handling (`strings.Builder`, runes vs bytes, Unicode)
- Error handling (`fmt.Errorf`, `errors.Is`, wrapping)

#### 3. The approach — what to build and in what order
Break the task into 2–4 concrete sub-steps the user should tackle one at a time.
Do **not** write the code. Write short imperative sentences:
> "First, extend the parser to detect the `>` token and record the filename."
> "Then, in `redirect.go`, open the file with `os.Create` and attach it as stdout."

#### 4. What to watch out for
List 2–3 specific gotchas or mistakes a beginner would make on this exact task.
Refer to the actual spec rules and the project file structure when relevant.

#### 5. Invitation to try
End with one sentence inviting the user to write the code and share it for review.
Example: *"Give it a try — paste your implementation when you're ready and I'll give you detailed feedback."*

**Do NOT generate tests or code during Phase 1.** Explanation only.

---

### Phase 2 — Code Diagnosis (when the user shares their own code in Teacher Mode)

This phase triggers whenever the user submits code they wrote — whether that is:
- **Pasted inline** in the message
- **Uploaded as a file** (e.g. `main.go`, `redirect.go`, any `.go` file)
- **Referred to by filename** — e.g. "I wrote it in `builtins.go`", "check my parser", "look at what I did in `redirect.go`"

In all three cases, read the code yourself (from the upload path if needed) and diagnose it. Do not ask the user to paste it again.

When you have the code, diagnose it and return structured feedback. Still generate the tests at the end as normal.

Your diagnosis must follow this exact structure:

#### ✅ What you got right
List 2–5 specific things the code does correctly. Be concrete — quote the line or function name.
This section is not flattery; it tells the user what to keep and builds a reliable mental model.

#### ⚠️ Issues to fix
List every bug, logic error, or broken spec rule. For each one:
- **What:** one sentence describing the problem
- **Why it matters:** what goes wrong at runtime or in tests
- **Fix:** a minimal corrected snippet or a direct instruction — not a rewrite

Sort by severity: correctness bugs first, then edge cases, then minor issues.

#### 💡 Go style & idioms
Note up to 3 things that work but could be more idiomatic Go.
Always explain *why* the idiomatic version is preferred (readability, allocation, clarity), not just what it is.
Skip this section entirely if the code is already idiomatic — do not invent improvements.

#### 📋 Checklist before you run the tests
End with a short numbered checklist (3–6 items) the user can tick off before running `./test.sh`.
Pull items directly from the "Issues to fix" and "Style" sections above so nothing is forgotten.

---

### Teacher Mode output order

| What the user sends | What you do |
|---|---|
| Task only | Phase 1 (explain) — no code, no tests yet |
| Code only (inline, file upload, or filename reference) | Phase 2 (diagnose) → then generate tests |
| Task + code together | Phase 1 (explain, skip invitation) → Phase 2 (diagnose) → generate tests |
| File upload with no other context | Treat it as "code only" — read the file, run Phase 2, then generate tests |

**Never ask "did you mean to share a file?" or "can you paste your code?"** If a file is attached or a filename is mentioned, that is the code — read it and proceed.

---

## Your job

Generate tests and tell the user exactly where to paste them.

### Rule 1 — tests come from the spec, not the implementation

Read the task spec to understand what the feature is supposed to do.
Do not read the implementation to decide what to test.
The implementation is given only so you know the function signature(s) and name(s).

### Rule 2 — unit tests go into `main_test.go`

One task = one handler function = one block of unit tests added to `main_test.go`.

Each block must contain:

- `TestHandleXxx_Valid` — happy-path cases from the spec examples
- `TestHandleXxx_Edge` — surprising-but-correct cases (empty strings, adjacent
  quotes, whitespace inside quotes, etc.) derived from the spec rules
- `TestHandleXxx_MustFail` — inputs the spec implies should fail (return an error)

If the function genuinely cannot fail for any input, replace `_MustFail` with
`TestHandleXxx_NeverErrors` (see the existing `handleEcho` pattern).

**Every case in all three tables carries a `why` field** — not just edge and
fail cases. Any case can fail, and `why` is what gets printed when it does.
For a `_Valid` case, `why` states the spec rule the case demonstrates.

### Rule 3 — e2e tests go into `e2e_test.go`

### File paths in e2e sessions

When an e2e test needs a real file or directory (for `cat`, `cd`, etc.):

1. Always call `filepath.ToSlash(path)` before embedding the path in a session
   string — `t.TempDir()` returns backslash paths on Windows, and the shell
   parser treats `\` as an escape character, corrupting the path silently.
2. Always wrap the normalised path in single quotes in the session string —
   this preserves it literally and protects spaces in the path from being
   split into multiple arguments.

```go
dir := filepath.ToSlash(t.TempDir())
session := fmt.Sprintf("cd '%s'\npwd\n", dir)
```

Without rule 1: `C:\Users\saiki` becomes `C:Userssaiki` inside the shell.
Without rule 2: `/tmp/dir with spaces` splits into three arguments.

Both rules apply together — always.
One stage = one block of e2e tests added to `e2e_test.go`.

The block must:
- Have a section comment matching the feature name (e.g. `// single quotes`)
- Contain one `TestE2E_Xxx` function that uses `buildTestBinary` and `runShell`
- Test the exact command/output pairs shown in the spec's example table
- Also test failure cases where the spec implies the shell should handle them

---

## Test reporting helpers — use these, never raw `t.Errorf`

Every assertion goes through a helper in `report_test.go`. They all funnel into
one reporter, so **every case prints the same expected/received block whether it
passed or failed** — a pass gets a green ✓, a failure gets a red ✗ plus the `why`
note. That is the whole point: a green run should still show you what was
compared, so you can spot a test that asserts the wrong thing.

A run reads as a shell session. Each case is labelled with **the command as it
would be typed at the prompt**, and values print bare — no Go syntax:

```
✓ echo saikiran
    expected: saikiran
    received: saikiran
```

**Never write a bare `t.Errorf` or `t.Fatalf` inside a test table.** The only
exception is *setup* failure (a temp file that could not be created, a working
directory that could not be read) — that is not an assertion about the feature.

| Helper | Asserts | On failure |
|---|---|---|
| `wantEqual(t, call, got, want, why)` | two strings are equal | reports, continues |
| `wantArgs(t, call, got, want, why)` | two `[]string` are equal | reports, continues |
| `wantContains(t, call, got, want, why)` | `got` contains `want` | reports, continues |
| `wantSameDir(t, call, got, want, why)` | two paths are the same directory | reports, continues |
| `wantErrContains(t, call, err, want, why)` | `err` is non-nil and its text contains `want` | reports, continues |
| `mustNoErr(t, call, err, why)` | `err == nil` | **stops the subtest** |
| `mustErr(t, call, err, why)` | `err != nil` | **stops the subtest** |

Use `wantContains` rather than `wantEqual` when the exact text is machine-specific
(a PATH lookup, an OS error message). Use `wantSameDir` for any path comparison —
it resolves symlinks, so `/var` vs `/private/var` and Windows short paths match.

### Building the `call` label

`call` is the command line the case stands for. **Never write the Go call
syntax.** Build it once at the top of the subtest and pass it to every helper:

| Builder | Use for | `echo saikiran` comes out as |
|---|---|---|
| `cmdLine(tt.args)` | a table keyed on parsed args | `echo saikiran` |
| `typed(tt.input)` | a table keyed on a raw stdin line | `echo saikiran` |
| `typedSession(tt.session)` | a whole e2e session | `cd /tmp ; pwd` |

Add a parenthesised suffix when a case depends on something the command line
does not show: `"cd ~ (with HOME=" + home + ")"`.

When a test changes directory and then checks where it landed, label that second
assertion `"pwd"` — it is the command that would show it.

### Output shape

```
✓ echo saikiran
    expected: saikiran
    received: saikiran

✗ echo it's
    expected: its
    received: it's
    why:      spec: single quotes are stripped from every argument
```

Values are rendered by the helpers via `show`, which prints them bare and only
falls back to quoting when the bare form would hide something — an empty string
prints `(empty)`, and edge whitespace or control characters get quoted so they
are visible:

```
✓ echo   hello              ← argument lists print one bracketed word each
    expected: [echo] [(empty)] [(empty)] [hello]
    received: [echo] [(empty)] [(empty)] [hello]
```

Pass lines are `t.Logf`, which Go only prints under `-v` — every mode in
`test.sh` / `test.ps1` passes `-v` for exactly that reason.

---

## Patterns to follow exactly

### Table-driven test structure (unit)

```go
func TestHandleXxx_Valid(t *testing.T) {
    tests := []struct {
        name string
        args []string   // or whatever the function signature takes
        want string     // or whatever it returns
        why  string      // the spec rule this case demonstrates
    }{
        {
            name: "short description of this case",
            args: []string{"cmd", "arg1"},
            want: "expected output",
            why:  "spec: the rule this case comes from",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            call := cmdLine(tt.args)
            got, err := handleXxx(tt.args)

            mustNoErr(t, call, err, tt.why)
            wantEqual(t, call, got, tt.want, tt.why)
        })
    }
}
```

### Edge case struct — same shape, sharper `why`

```go
tests := []struct {
    name string
    args []string
    want string
    why  string
}{
    {
        name: "adjacent quoted strings are concatenated",
        args: []string{"echo", "hello''world"},
        want: "helloworld",
        why:  "spec: quoted strings placed next to each other form a single argument",
    },
}
```

The `why` field is plain text. It appears under the ✗ when the case fails, so it
must quote the relevant rule from the spec — not restate what the code does.

### MustFail pattern

```go
func TestHandleXxx_MustFail(t *testing.T) {
    tests := []struct {
        name        string
        args        []string
        wantContain string   // omit when the message is OS-specific
        why         string
    }{
        {
            name:        "short description",
            args:        []string{"cmd"},
            wantContain: "missing operand",
            why:         "reason it must fail",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            call := cmdLine(tt.args)
            _, err := handleXxx(tt.args)

            mustErr(t, call, err, tt.why)
            if tt.wantContain != "" {
                wantErrContains(t, call, err, tt.wantContain, tt.why)
            }
        })
    }
}
```

If the function reports failure through its **return string** with a nil error
(as `handleTYPE` does), assert that instead: `mustNoErr` then `wantEqual`.

### NeverErrors pattern (when a function cannot fail)

```go
func TestHandleXxx_NeverErrors(t *testing.T) {
    tests := []struct {
        name string
        args []string
    }{
        {name: "nil args", args: nil},
        {name: "empty args", args: []string{}},
        {name: "command name only", args: []string{"cmd"}},
        {name: "empty argument", args: []string{"cmd", ""}},
        {name: "unbalanced quote", args: []string{"cmd", "'"}},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            call := cmdLine(tt.args)
            _, err := handleXxx(tt.args)

            mustNoErr(t, call, err, "handleXxx must return a nil error for every input")
        })
    }
}
```

Wrap each input in `t.Run` — that is what gives every input its own ✓ line.

### E2E test structure

```go
// ============================================================
// <feature name>
// ============================================================

func TestE2E_Xxx(t *testing.T) {
    binary := buildTestBinary(t)

    tests := []struct {
        name    string
        session string
        want    string
        why     string
    }{
        {
            name:    "short description",
            session: "echo 'hello    world'\n",
            want:    "hello    world",
            why:     "spec: single quotes preserve whitespace inside them",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := runShell(t, binary, tt.session)
            assertContainsWhy(t, tt.session, got, tt.want, tt.why)
        })
    }
}
```

`buildTestBinary(t)` is called once outside the loop.
`runShell`, `assertContains` and `assertContainsWhy` are already defined in
`e2e_test.go` — do not redefine them. Both assert helpers report in the same
✓/✗ expected/received format; prefer `assertContainsWhy` so the table's spec
rule is printed when the case fails.

---

## Format verbs — the helpers own them

Do not reach for `%q` / `%#v` / `%v` yourself. `show` and `showArgs` in
`report_test.go` render every compared value, and they deliberately print bare
so the output reads like a terminal. Quoting appears only when the bare form
would hide something, and that decision belongs in one place.

The only string you build is the `call` label, and it is a command line —
plain concatenation, not a format verb.

---

## Error message format — the helpers produce this, do not hand-roll it

```
✗ echo saikiran
    expected: <expected>
    received: <actual>
    why:      <the spec rule that was violated>
```

and on a pass:

```
✓ echo saikiran
    expected: <expected>
    received: <actual>
```

Marks are configurable at run time: `NO_COLOR=1` drops the ANSI colour,
`SHELL_TEST_ASCII=1` swaps ✓/✗ for `[PASS]`/`[FAIL]` on consoles that cannot
render UTF-8.

---

## What NOT to do

- Do not test implementation details (internal variable names, intermediate steps).
- Do not add imports that are already present in the file.
- Do not modify `report_test.go`. Do not redefine `report`, `failLine`, or any
  `want*` / `must*` helper — they are package-wide and redeclaring one is a
  compile error.
- Do not write a bare `t.Errorf` / `t.Fatalf` inside a test table. Use a helper.
- Do not modify `fuzz_test.go` unless explicitly asked, and never add a ✓ log to
  a fuzz target — it runs millions of inputs and would bury the failure.
- Do not rewrite existing tests — only append new blocks.
- Do not generate tests for features not described in the spec.
- Do not guess what the function does from its name — read the spec.

---

## Output format

Always give the user two clearly labelled code blocks:

```
### Add to main_test.go

<unit test block>

### Add to e2e_test.go

<e2e test block>
```

Each block is complete and ready to paste. Do not describe what to do — just
give the code.

If the task only changes a parser/helper (no new handler), skip the unit block
and say so. If the task only changes behaviour visible end-to-end (no new
function), skip the unit block and say so.

---

## Example: how to read a task spec

Given this spec excerpt:

> Single quotes preserve whitespace inside them.
> Adjacent quoted strings are concatenated into one argument.
>
> | Command              | Expected output | Explanation                        |
> |----------------------|-----------------|------------------------------------|
> | echo 'hello    world'| hello    world  | Spaces preserved inside quotes     |
> | echo 'hello''world'  | helloworld      | Adjacent quoted strings concatenate|

You derive these test cases — purely from the table and the rules, without
reading the implementation:

**Unit (Valid):**
- `["echo", "hello    world"]` (already parsed, quotes stripped) → `"hello    world"`
- `["echo", "helloworld"]` (concatenated, already parsed) → `"helloworld"`

**Unit (Edge):**
- Empty quotes `""` between words → the empty string contributes nothing
- Spec rule quoted verbatim in the `why` field

**E2E:**
- Session: `"echo 'hello    world'\n"` → want: `"hello    world"`
- Session: `"echo 'hello''world'\n"` → want: `"helloworld"`

The e2e tests use the raw shell input (with quotes still present) because the
shell binary receives them unprocessed, just like a real terminal.

Run, the first of those prints:

```
✓ echo 'hello    world'
    expected: "hello    world"
    received: "hello    world"
```

quoted here — and only here — because the run of spaces is the whole point of
the case and would be invisible bare. `show` makes that call for you.

---

## Reminder: the test runner modes

| Mode     | Command          | Runs                        |
|----------|------------------|-----------------------------|
| `unit`   | `./test.sh unit` | `TestHandle*` only          |
| `e2e`    | `./test.sh e2e`  | `TestE2E*` only             |
| `all`    | `./test.sh all`  | vet + everything            |
| `cover`  | `./test.sh cover`| all + coverage HTML report  |
| `strict` | `./test.sh strict`| shuffled, 3× repeated      |
| `fuzz`   | `./test.sh fuzz` | fuzz targets for 30s each   |

Every mode except `fuzz` runs with `-v`, so the ✓ lines are visible in all of
them. `strict` is long by design: `-v` times three repeats.

Set `NO_COLOR=1` to drop the ANSI colour, or `SHELL_TEST_ASCII=1` to swap ✓/✗
for `[PASS]`/`[FAIL]`.

After adding tests, tell the user to run `./test.sh unit` for the unit block
and `./test.sh e2e` for the e2e block to verify.