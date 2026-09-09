# CLAUDE.md — Shell Project Test Generation

## Project

Minimal Unix shell in Go. Reads stdin, parses, dispatches to builtins or external programs.

```
app/
  main.go          ← main loop + command dispatch
  parser.go        ← handleInput (tokenizer)
  builtins.go      ← builtin handlers (echo, pwd, cd, type, exec)
  redirect.go      ← redirect parsing + output/error writers
  autocomplete.go  ← tab completion + readLine
  main_test.go     ← unit tests (one block per handler)
  e2e_test.go      ← end-to-end tests (compile binary, pipe stdin, check stdout)
  report_test.go   ← shared reporter + assertion helpers — do not modify
  fuzz_test.go     ← fuzz targets — do not modify unless asked
  go.mod
```

## Input

User gives a **task spec** (feature rules + example table) and their **implementation**.

Code can arrive three ways — treat them identically, never ask the user to paste code they already shared:
- Pasted inline in the message
- Uploaded file → read from `/mnt/user-data/uploads/<filename>`
- Filename mentioned → read from `/mnt/user-data/uploads/<file>.go`

---

## Teacher Mode

**Activate:** message contains `[TEACH]` or `teacher mode`. Stays on until `[TEACH OFF]`. Off by default.

| User sends | You do |
|---|---|
| Task only | Phase 1 only — no code, no tests |
| Code only (any form) | Phase 2 → generate tests |
| Task + code | Phase 1 (skip invite) → Phase 2 → generate tests |
| File upload with no context | Treat as code only |

### Phase 1 — Task Explanation
In order, no code written:
1. **Plain English** — restate the task, strip jargon, one paragraph
2. **Key concepts** — Go/shell concepts needed; one-sentence definition + tiny example each
3. **Approach** — 2–4 imperative steps, no code: *"First, extend the parser to detect `>`…"*
4. **Gotchas** — 2–3 beginner mistakes specific to this task and these spec rules
5. **Invite** — *"Give it a try and share your code for feedback."*

### Phase 2 — Code Diagnosis
Read the code (from upload path if needed), then return in this order:

**✅ What you got right** — 2–5 specific correct things, quote the line or function name. Tells user what to keep.

**⚠️ Issues to fix** — every bug/logic error/broken spec rule, sorted by severity:
- **What:** one sentence · **Why it matters:** runtime or test consequence · **Fix:** minimal snippet, not a rewrite

**💡 Go style & idioms** — up to 3 improvements; explain *why* each is preferred. Skip entirely if code is already idiomatic.

**📋 Checklist** — 3–6 items pulled from the two sections above; tick off before running `./test.sh`.

---

## Test Generation

### Rules
1. **Tests come from the spec, not the impl.** Use the impl only for function name/signature.
2. **Unit tests → `main_test.go`.** One task = one handler = one block with:
   - `TestHandleXxx_Valid` — happy-path cases from spec examples
   - `TestHandleXxx_Edge` — surprising-but-correct (empty strings, adjacent quotes, inner whitespace)
   - `TestHandleXxx_MustFail` — inputs the spec implies should error
   - If the function genuinely cannot fail, use `TestHandleXxx_NeverErrors` instead of `_MustFail`
   - **Every case in every table has a `why` field** quoting the relevant spec rule
3. **E2E tests → `e2e_test.go`.** One stage = one block. For file/dir paths in session strings:
   - Always `filepath.ToSlash(path)` — backslashes corrupt silently on Windows
   - Always wrap in single quotes — protects spaces from splitting
   ```go
   dir := filepath.ToSlash(t.TempDir())
   session := fmt.Sprintf("cd '%s'\npwd\n", dir)
   ```

### Assertion helpers — never raw `t.Errorf`

| Helper | Asserts | Stops subtest? |
|---|---|---|
| `wantEqual(t, call, got, want, why)` | strings equal | no |
| `wantArgs(t, call, got, want, why)` | `[]string` equal | no |
| `wantContains(t, call, got, want, why)` | got contains want | no |
| `wantSameDir(t, call, got, want, why)` | same dir (resolves symlinks) | no |
| `wantErrContains(t, call, err, want, why)` | err non-nil + contains want | no |
| `mustNoErr(t, call, err, why)` | err == nil | **yes** |
| `mustErr(t, call, err, why)` | err != nil | **yes** |

Use `wantContains` for OS-specific text. Use `wantSameDir` for all path comparisons.
Do not use `%q`/`%v` — `show`/`showArgs` in `report_test.go` own all value formatting.

### `call` label builders — never write Go syntax in `call`

| Builder | Use for |
|---|---|
| `cmdLine(tt.args)` | table keyed on parsed args |
| `typed(tt.input)` | table keyed on raw stdin line |
| `typedSession(tt.session)` | whole e2e session |

Add a parenthesised suffix when context matters: `"cd ~ (with HOME=" + home + ")"`.

---

## Code templates

**Unit — Valid / Edge** (same struct shape):
```go
func TestHandleXxx_Valid(t *testing.T) {
    tests := []struct {
        name string
        args []string
        want string
        why  string
    }{
        {name: "…", args: []string{"cmd", "arg"}, want: "…", why: "spec: …"},
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

**Unit — MustFail:**
```go
func TestHandleXxx_MustFail(t *testing.T) {
    tests := []struct {
        name        string
        args        []string
        wantContain string // omit when message is OS-specific
        why         string
    }{
        {name: "…", args: []string{"cmd"}, wantContain: "missing operand", why: "…"},
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
If failure is reported via return string with nil error (like `handleTYPE`), use `mustNoErr` + `wantEqual`.

**Unit — NeverErrors:**
```go
func TestHandleXxx_NeverErrors(t *testing.T) {
    tests := []struct{ name string; args []string }{
        {name: "nil args", args: nil},
        {name: "empty args", args: []string{}},
        {name: "command only", args: []string{"cmd"}},
        {name: "empty arg", args: []string{"cmd", ""}},
        {name: "unbalanced quote", args: []string{"cmd", "'"}},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := handleXxx(tt.args)
            mustNoErr(t, cmdLine(tt.args), err, "handleXxx must never error")
        })
    }
}
```

**E2E:**
```go
// ============================================================
// <feature name>
// ============================================================
func TestE2E_Xxx(t *testing.T) {
    binary := buildTestBinary(t) // once, outside the loop
    tests := []struct {
        name    string
        session string
        want    string
        why     string
    }{
        {name: "…", session: "echo 'hello    world'\n", want: "hello    world", why: "spec: …"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := runShell(t, binary, tt.session)
            assertContainsWhy(t, tt.session, got, tt.want, tt.why)
        })
    }
}
```
`runShell`, `assertContains`, `assertContainsWhy` are defined in `e2e_test.go` — do not redefine them.

---

## What NOT to do
- Don't test implementation details or intermediate steps
- Don't add already-present imports
- Don't modify `report_test.go` or `fuzz_test.go` (unless asked for fuzz)
- Don't redefine any `want*`/`must*` helper — package-wide; redeclaring is a compile error
- Don't use bare `t.Errorf`/`t.Fatalf` inside test tables
- Don't rewrite existing tests — append only
- Don't generate tests for features not in the spec
- Don't guess behaviour from function names — read the spec

---

## Output format

Two labelled blocks, ready to paste:
```
### Add to main_test.go
<unit block>

### Add to e2e_test.go
<e2e block>
```
Skip the unit block if there's no new handler function (say so). Skip if task is e2e-only (say so).
Tell the user: `./test.sh unit` for unit, `./test.sh e2e` for e2e.

## Test runner

| Mode | Command | Runs |
|---|---|---|
| unit | `./test.sh unit` | `TestHandle*` |
| e2e | `./test.sh e2e` | `TestE2E*` |
| all | `./test.sh all` | vet + everything |
| cover | `./test.sh cover` | all + HTML coverage |
| strict | `./test.sh strict` | shuffled, 3× repeated |
| fuzz | `./test.sh fuzz` | fuzz targets 30s each |

`NO_COLOR=1` drops ANSI colour. `SHELL_TEST_ASCII=1` swaps ✓/✗ for `[PASS]`/`[FAIL]`.