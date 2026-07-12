# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Additional Guidelines

- See `guidelines.md` for cross-cutting engineering and UX guidelines that should be applied across the project.

## Line length

- The line-length limit is **120 characters**, enforced by `golines` (`max-len: 120` in
  [.golangci.yml](.golangci.yml)). Use the full width — fill lines close to 120 before
  wrapping rather than stopping short at 80 or 100.
- **This applies to comments too.** Comments should use the full 120-character width like code; do not wrap comment
  text early. `golines` shortens over-length lines but never fills short ones, so comment width is a manual habit the
  tooling cannot enforce for you.
- Breaking a line before 120 is acceptable only when justified — by grammar/readability, or to separate distinct
  logical blocks. Otherwise, keep filling the line.
- Do not otherwise hand-enforce formatting rules that the configured formatters and linters already handle (import
  grouping/order, mechanical wrapping, etc.).
- When editing code, prefer readable formatting and then run the project formatters/linters to normalize the result.

## Comments

- **Keep comments short and to the point.** Explain the non-obvious *why*, not the obvious *what*. Prefer a terse
  phrase over a full sentence, and don't pad prose to fill the line — concise wins over long, even at 120.
- **Struct fields:** give each field at most one comment, inline (trailing) on the field's own line, using the room
  left up to 120. If a field needs more than fits there, don't spill onto extra inline lines — move the longer
  explanation above the struct declaration (or into the type's doc comment).

## Verification

- After making Go code changes, run `golangci-lint run` before considering the task complete.
- Also run the relevant `go test` commands for the code you changed.
- If lint or tests cannot be run, say so explicitly in the final response.

## Code Style Guidelines

- **The Go version** is set in `go.mod`.
- **Follow modern Go idioms for the version in go.mod**. When in doubt, prefer stdlib solutions over manual
  implementations or third-party packages.
- **Tool-enforced style:** Follow the configured formatters and linters in
  [.golangci.yml](.golangci.yml). Do not manually enforce rules that are already handled
  there, such as import grouping/order, line wrapping, and other mechanical formatting details.
- **Error Handling:** Return errors with context; use `fmt.Errorf`; use `logrus` for logging.
- **Naming:** PascalCase for exported items, camelCase for non-exported items.
- **Types:** Use struct field JSON tags where applicable; EXCEPT for structs that represent custom errors.
- **Testing:**
  - Use `testify` package for assertions.
- **Concurrency:** Use a mutex for thread safety; use `context` for cancellation.
- **Do not name the return values of functions** unless it is necessary, and if so, document it in a comment.
- **Use the functions from the standard library** packages `slices` and `maps` whenever possible. For example, copy
  data structures using the appropriate `Clone` function instead of a loop.
- **Prefer iterators judiciously** when they improve clarity, especially when replacing: (1) a non-range for loop, or
  (2) a for range where the loop counter isn't used in the body. Use plain `for range` when it is clearer.
- **Prefer tagged switches** over if-then-else when there are more than 2 conditional branches.
- **Set pattern**: use `map[T]struct{}` when implementing a set (where only membership matters, not associated values).
- **Prefer JSON encoders/decoders** over loading complete payloads in memory and calling `json.Marshal` or
  `json.Unmarshal`.
- **Use `max`/`min` builtins** (Go 1.21+) instead of `math.Max`/`math.Min`.
- **Use `clear` builtin** for maps/slices (Go 1.21+).
- **Prefer `any` over `interface{}`** (alias since Go 1.18).
- **Comment the current state of the code**, not its past state. Document what the code does, not what it did before.
