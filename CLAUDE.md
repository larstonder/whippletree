# whippletree — working notes

## Comments

Default to fewer. Aim for well under 15% comment lines in non-test Go files; if a file is
climbing past that, the comments are probably carrying weight the code should carry.

**Write a comment when it explains something the code cannot:**

- Why a non-obvious constraint exists — especially security rationale and fail-open/fail-closed
  choices. `resolveHandlerPath` re-checking containment at dispatch time needs its one sentence,
  because deleting the "redundant" check would reintroduce an RCE.
- Empirical facts about a harness that were expensive to learn. `os.Stat` never setting execute
  bits on Windows, opencode's `.opencode/skills` placement, Copilot's exit-code semantics. This
  is the research the project exists to hold; it earns its space.
- Exported doc comments, one or two sentences.

**Do not write a comment that:**

- Restates the code. `// loop over requirements` above a loop over requirements.
- Narrates the change history. "Until now this was never read", "this used to panic", "adds X".
  That belongs in the commit message, where it is attached to a diff and stays accurate. In a
  source file it rots the day someone else edits the line.
- Justifies the work to a reviewer. The commit message is the place to argue; the file is for
  whoever reads it in a year with no memory of the argument.
- Repeats a doc comment on the implementation below it.

**Length.** One or two sentences is the norm. A block over six lines needs a reason; a block over
ten almost always belongs in `docs/` with a pointer to it. Long-form harness research goes in
`docs/*-probe-findings.md`, not inline.

Prose style in comments follows the repo's existing voice: plain sentences, no hedging, no
exclamation. Match the file you are editing.

## Licence headers

Every Go file carries a REUSE-style SPDX header:

```go
// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0
```

followed by a blank line, so a package doc comment below it still binds to the
package. New files need it; CI fails without it.

## Testing

A test for a bug must fail before the fix. Verify that, don't assume it.

Tests whose fixtures are shell scripts skip on Windows rather than reporting a false pass.

## Verification

`gofmt -l .`, `go vet ./...`, and `GOOS=windows go vet ./...` all clean before committing.
`examples/kb-shaped` artifacts are committed, so `whippletree build` on it must reproduce them
byte-identically — check `git status` after.

## Scope

`whippletree.dev` and `dev.whippletree.v1` ship inside every binary and every compiled bundle;
treat them as load-bearing identifiers, not placeholders.

Windows and the importer are tracked as issues #1 and #2. Both are contract-surface questions
that should be decided before `dev.whippletree.v1` is frozen at v1.0.
