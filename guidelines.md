# Guidelines

This file records implementation practices that we want to keep applying across the project.

## HTTP And Context

- Pass a `context.Context` through every layer that can trigger HTTP work.
- Prefer `...Context` variants for API helpers, and keep convenience wrappers only as thin adapters.
- Make HTTP calls cancellable end-to-end: UI action -> workflow/service -> API helper -> HTTP client.
- If a user can reasonably want to stop an operation, there should be an actual cancel path, not just a hidden goroutine.
- Preserve caller deadlines when they exist.
- Apply a default maximum timeout to HTTP calls when the caller does not provide one.
- Current default maximum timeout: `30s`.
- Centralize the default timeout in the HTTP client layer so all requests behave consistently.

## UI For Long-Running Work

- Long-running modal operations should show progress and expose a `Cancel` button.
- Canceling from the UI should cancel the underlying context, not only dismiss the dialog.
- Progress dialogs should stay open only while work is in progress.
- If an operation fails validation immediately, do not flash a progress dialog first.
- Validation failures should use a normal modal message dialog with a clear `Close` button.

## Validation And Error Messages

- Validate as early as possible, before starting background work.
- Error messages shown to users should be phrased in user language, not internal/technical language.
- Start user-facing sentences with a capital letter and end them with punctuation.
- Prefer actionable guidance over diagnostic wording.
- When rejecting input, suggest examples of criteria or actions that will succeed.

## Translations

- French and Breton use the typographic apostrophe `’`, never the straight `'`. Breton takes it in the `c’h` trigraph
  too. English keeps `'`.
- Symbols are not language: arrows, glyphs and the like belong in the interface that composes the string, never in the
  message a translator edits. Left there, every translator re-decides them and they drift apart.
- Punctuation inside a sentence is prose, and stays with the translator. The rule above is about decoration attached to
  a label, not about hoisting a separator out of a sentence and gluing fragments back together.
- A message that names a label the wiki shows (a bot-password grant, a group) should quote that wiki's own wording,
  taken from its message catalogue rather than translated afresh, so the name matches what is on the operator's screen.
- Identifiers stay untranslated: right names such as `editsitecss`, and canonical titles such as
  `Special:BotPasswords`, which resolve on a wiki in any language.

## Execution And Workflow Cancellation

- Cancellation should cover preparation phases as well as execution phases.
- If a workflow has a planning/read phase that performs network requests, that phase should also run under the workflow context.
- Sequential execution loops should check `ctx.Done()` between items and before retries/sleeps.
- Retry/backoff waits should also be context-aware so cancellation is immediate.

## API Client Behavior

- The HTTP client is the right place to enforce shared concerns such as:
  - default timeout
  - retries
  - retry delays
  - CSRF token refresh
  - throttling
  - request user-agent
- Keep these rules centralized instead of re-implementing them in screens or workflows.

## Testing

- Add tests when introducing infrastructure behavior such as cancellation, timeout defaults, or retry logic.
- Test both the default behavior and the override behavior.
- For timeout behavior, prefer transport-level tests that inspect the outgoing client request rather than assuming server handlers see client deadlines.

## Maintenance

- When a temporary testing aid is added, label it clearly and remove it once the behavior is verified.
- When a bug leads to a useful engineering lesson, add that lesson here so it becomes a reusable project rule.
