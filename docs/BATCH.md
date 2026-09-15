# Scoped GET batches — 0.3.0-sabiz.4

Use the existing `trello_api_read` tool. No account scope or new tool is needed.

```json
{
  "endpoint": "/batch",
  "query": {
    "urls": "/boards/ALLOWED_BOARD_ID?fields=id%2Cname,/cards/VERIFIED_CARD_ID/actions?filter=commentCard&limit=100"
  }
}
```

Replace placeholders with verified IDs. The `urls` string has 1–10 comma-separated relative GET routes, without a host or `/1` API prefix. Percent-encode commas inside individual query values (`fields=id%2Cname`) to distinguish them from route separators. Inspect the catalog parameters first; unknown query names, duplicate query keys and invalid scalar values are rejected. The bundled catalog omits action pagination parameters already supported by the named tools; `before`, `since`, `fields` and `limit` (1–1000) are explicitly supported on actions endpoints.

All routes are parsed before scope discovery. Nested batches, external URLs, credentials, traversal, encoded/ambiguous paths and non-GET operations are rejected. In board mode, each route and referenced resource must pass existing scope checks; membership expansion remains blocked. A failing scope check prevents the whole content batch from being sent. Minimal resource ownership probes may precede that decision. Resource-to-board checks are memoized only within the current sequential validation; there is no cross-run authorization cache. Board moves between validation and retrieval remain a possible concurrent change, just as for individual reads; execution-time checks are still required.

`status` is the outer HTTP status. `data` preserves Trello's ordered array of per-route status/body objects. Added fields:

- `batch_routes`: canonical routes in request order.
- `batch_complete`: true only when the response has the expected number of entries and every entry has one 2xx status.
- `batch_failed_indexes`: zero-based indexes of failed/missing/malformed entries, omitted when empty.

An outer 200 does not establish full success. Inspect each entry and retry only failed reads when appropriate. An unexpected response shape or count requires investigation rather than treating a missing failure index as success. Pagination is unchanged: one successful page is not a complete history. The existing 8 MiB response limit applies to the whole batch; reduce fields, page size or batch size if exceeded. No automatic retries or pagination are added.

Grouping saves network round trips, not necessarily the equivalent number of provider quota operations. Ownership checks still incur reads; grouping routes for the same card saves repeated checks. Measure actual calls rather than assuming tenfold speedup.

LoanFlow must verify the Ariel label before including any card content in a batch. Batch does not apply a label filter and must not be used to read a broad content inventory and filter it afterward. Batch only independent reads; do not include reads that depend on a previous subrequest's unverified result.

## Validation

Run `go test -race ./...` and `go vet ./...` from `cli/`. Tests cover malformed inputs, limits, scope rejection before content, credential injection, field-list encoding, local ownership memoization, account-mode compatibility and partial/malformed responses.

The optional `TRELLO_BATCH_LIVE_TEST=1 go test -run '^TestBatchLiveComparison$' -v` uses existing environment credentials and requires board scope with exactly one allowed board. It compares three individual board metadata reads with one batch, including comma-separated fields. It does not write remote state or print credentials/returned metadata. Do not copy credentials into commands, fixtures or commits.
