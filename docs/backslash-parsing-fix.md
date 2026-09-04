# Backslash parsing fix — what changed

Commit `a485507 "fixed backslash parsing"`, on top of `56d5b0a`.

## app/main.go — `handleInput`

Only the double-quote branch changed. The single-quote branch (`case inQuote:`) was already correct and is untouched.

```diff
 	inQuote := false
 	inDoubleQuote := false
 	slash := false
-	dquoteSlash := false
+	pendingEscape := false
 
 	for _, r := range line {
 		switch {
-		case dquoteSlash:
-			// Inside double quotes after a backslash:
-			// \\ → literal \, \" → literal ", anything else → keep both \ and the char.
-			if r == '\\' || r == '"' {
-				current.WriteRune(r)
-			} else {
-				current.WriteRune('\\')
-				current.WriteRune(r)
-			}
-			dquoteSlash = false
 		case slash:
 			current.WriteRune(r)
 			slash = false
@@ -51,13 +41,23 @@
 			}
 
 		case inDoubleQuote:
-			if r == '"' {
+			switch {
+			case pendingEscape:
+				pendingEscape = false
+				if r == '"' || r == '\\' {
+					current.WriteRune(r)
+				} else {
+					current.WriteRune('\\')
+					current.WriteRune(r)
+				}
+			case r == '"':
 				inDoubleQuote = false
-			} else if r == '\\' {
-				dquoteSlash = true
-			} else {
+			case r == '\\':
+				pendingEscape = true
+			default:
 				current.WriteRune(r)
 			}
+
 		case r == '\'':
 			inQuote = true
 			inArg = true
```

**What was broken:** the working tree (before this fix) had drifted from the last commit into a different, buggy shape — a `doubleSlash` flag duplicated into *both* the single- and double-quote branches, collapsing any `\\` pair in single quotes too (single quotes should never treat backslash specially), and with no working path for `\"` inside double quotes at all.

**What this restores:** `dquoteSlash` → `pendingEscape` is a rename plus a restructure from a top-level `case` to a `switch` nested inside `case inDoubleQuote:`, same behavior:
- `\\` inside double quotes → one literal `\`
- `\"` inside double quotes → one literal `"`, without closing the quote
- any other `\x` inside double quotes → both characters kept literally
- single quotes: backslash has no special meaning at all, every character literal

## app/e2e_test.go — 4 Windows-portability fixes

No assertions changed, only how the tests set up files/paths on Windows:

| Test | Problem | Fix |
|---|---|---|
| `TestE2E_BackslashInDoubleQuotes_ExternalCommand` | tried to create a real file named with a literal `"` — illegal on Windows | dropped that filename case, kept the plain-filename case, added a comment pointing at the unit test that covers `\"` without touching the filesystem |
| `TestE2E_BackslashInSingleQuotes_ExternalCommand` | tried to create a real file named with a literal `\` — illegal on Windows | same treatment as above |
| `TestE2E_CD_ThenPWD` | embedded a raw Windows path (`C:\Users\...`) straight into the shell session, so the shell's own backslash-escaping corrupted it | wrap the path in `filepath.ToSlash(...)` + single quotes before typing it into the session; assert the `pwd` output against the original OS-native path, not the forward-slash form |
| `TestE2E_QuotedExecutable` | one subtest needed a `"` in a real filename (illegal); the copied test binary had no extension, so Windows `PATH` lookup couldn't find it | dropped the illegal-filename subtest (covered by the unit test instead); append `.exe` to the copied binary on Windows |

## Known remaining issue — left alone

`FuzzHandleEcho` (`app/fuzz_test.go`) asserts that `handleEcho` itself strips single quotes from its output. That's not `handleEcho`'s responsibility — quote-stripping happens in `handleInput` during parsing, before args ever reach `handleEcho`, which only joins them with spaces. Left unfixed per this project's rule not to modify `fuzz_test.go` without being explicitly asked.
