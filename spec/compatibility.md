# Provider compatibility

AI harness session stores are private implementation details and can change
without notice. session-recall treats every provider adapter as a versioned,
read-only compatibility boundary.

## Refresh guarantees

- Every adapter declares a `ParserRevision`.
- The index records that revision per source. Increasing it makes sources from
  only that provider eligible for re-decoding even when their files have not
  changed.
- Successfully decoded sources are replaced atomically in the derived index.
- A source-level parse failure keeps the previous indexed rows and remains
  eligible for retry on the next refresh.
- A broken provider does not block searching data from other providers.

`session-recall doctor` shows the active parser revision and the most recent
scan's source, skip, and failure counts. Its warning paths identify the local
source that needs a new fixture without printing prompt content. After an
adapter revision changes, diagnostics show `refresh=pending` until that parser
has completed a refresh.

## Changing an adapter

When adding support for a new harness format:

1. Add a sanitized fixture representing the new structure. Fixtures must not
   contain private prompts, credentials, or machine-specific identifiers.
2. Keep the previous fixture and decoder behavior unless the old format can no
   longer occur.
3. Add a regression test for structurally valid but unsupported user-message
   content. It must produce a source failure rather than an empty successful
   session.
4. Increment `ParserRevision` whenever existing sources need to be re-decoded.
   Path discovery or resume-command-only changes do not require a parser bump.
5. Run `mise run check` and `mise run bench`.

Unknown fields and irrelevant record kinds should be ignored. Invalid JSON,
missing session identity, and unsupported user-message shapes should be counted
and surfaced as compatibility failures.
