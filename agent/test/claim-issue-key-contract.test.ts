import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import type { ClaimResponse } from "../src/protocol.js";
import { linkedIssueKey, linkedIssueLabel } from "../src/linked-issue.js";

// The worker side of the PRD 4173 linked-issue claim contract. It parses the SAME golden the Go
// producer test pins (api/internal/workersvc/claim_linked_issue_wire_test.go ->
// TestLinkedIssueClaimWireContract), so the server's `issue_key` and the agent's idea of a linked
// run cannot drift into two lenient fakes. Typing the parse as ClaimResponse makes
// `npm run typecheck` fail if `issue_key` leaves protocol.ts.
const fixture = join(
  import.meta.dirname,
  "..",
  "..",
  "api",
  "internal",
  "workersvc",
  "testdata",
  "claim_linked_issue_wire.json",
);

test("linked issue claim wire contract: issue_key reaches the agent and marks the run linked", () => {
  const claim = JSON.parse(readFileSync(fixture, "utf8")) as ClaimResponse;
  assert.equal(claim.kind, "issue");
  assert.equal(claim.issue_iid, 12);
  assert.equal(claim.issue_key, "ABC-12");

  const linked = linkedIssueKey(claim);
  assert.equal(linked.linked, true);
  assert.equal(linked.label, "ABC-12");
  assert.equal(linkedIssueLabel(linked), "ABC-12");
});

test("an unlinked claim carries no issue_key at all, so the run is not linked", () => {
  const claim = JSON.parse(readFileSync(fixture, "utf8")) as ClaimResponse;
  // The server omits the field for an unlinked claim (never null, never ""); the agent treats
  // the key's mere presence as the link, so an absent one must read as unlinked.
  const { issue_key: _dropped, ...unlinked } = claim;
  assert.equal("issue_key" in unlinked, false);
  assert.deepEqual(
    linkedIssueKey({
      issue_key: (unlinked as { issue_key?: string }).issue_key,
    }),
    { linked: false, label: null },
  );
});
