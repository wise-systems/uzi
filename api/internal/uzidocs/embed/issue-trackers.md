---
title: Issue trackers (YouTrack)
order: 76
audience: user
---

# Issue trackers (YouTrack)

A repo can take its issues from a YouTrack project while its code, merge
requests and CI stay on its forge. YouTrack is the only issue tracker
supported. A repo with no link behaves as before.

## Before you start (operator)

The operator must set `ISSUE_TRACKER_ALLOWED_BASE_URLS` (Helm
`issueTracker.allowedBaseURLs`). It is empty by default, so no connection can
be made until it is set. Entries are a bare `https://host[:port]`: https scheme
and host only. A YouTrack served under a context path (such as `/youtrack`) is
not supported. See [Configuration](./configuration.md).

## Connect

1. In YouTrack, open Profile, Account Security, New token. Give it the scope
   YouTrack, for the account uzi acts as.
2. In uzi, open Settings, Issue trackers, and add a connection: pick the base
   URL from the operator's allowed list and paste the token. (A kind picker
   shows only once more than one kind is supported; today YouTrack is implicit.)

The token is write-only: it is stored sealed, never shown again, and never sent
to the worker. Reconnecting with the same base URL replaces the token; if a repo
is linked, the new token must belong to the same YouTrack account. A connection
with a linked repo cannot be deleted.

## Link a repo

A repo that was never enabled offers "Link to an issue tracker" on the Repos
page. Choose the connection, the project and the eligibility tag, and tick
"They can edit issues in this project". The choice cannot be changed afterwards
(no unlink or relink). A repo that was ever enabled cannot be linked: that
includes every repo that existed when issue tracker links were added and a repo
that was enabled, then removed and listed again. Link in the web before
anything enables the repo, including `/server setup`.

Issues carrying the eligibility tag come into uzi. The tag is chosen by id: a
same-named tag of another user does not count, and renaming it keeps working.

- The connection's account must be able to apply the tag, and so must the people
  who start work by tagging.
- A named group never establishes the connection account's own access. It counts
  for others only when YouTrack reports at least two members for it (a missing
  count does not count). So a tag shared only with a named group can be refused
  at link time: share the eligibility tag with named users or with All Users.
- Tag sharing is checked at link time. Whether those people can edit issues in
  the project is not checked, and the admin confirms it.

## Map board columns to States

On a linked repo each board column maps to one State, plus the open State and
the closed State. The closed State must mean done (for example Fixed or Done),
never Won't fix; uzi refuses it. Moving a card into an unmapped column is
refused until it is mapped. Use "Map States" on the repo row or in the board's
column settings.

The web shows an issue's key (for example `ABC-12`, from the project short name
stored at link time) on the board (cards, search, screen-reader labels), the
issue page (header, breadcrumb), the runs list and run page heading, created-issue
lists, the findings group-file notice, the board's needs-attention strip and
the run options name. It still shows `#number` in the
overview run row, the needs-decision card, triage, a schedule's last fire,
proposal cards, the command menu and the capacity rail.

## Not offered on a linked repo

Autopilot, GitHub Projects v2 sync, creating repo labels, eligibility by
assigning the issue to the uzi bot, and changing the issue source after the
first enable.

There is no author mapping setting: only the connection's own YouTrack account
counts as a uzi user. Comments by anyone else are withheld from runs, and a
scheduled run auto-approves its plan only for an issue that account reported
and whose title and body only it edited.

## An issue moved to another YouTrack project

It gets a new number there, so uzi treats it as gone from the linked project.
Later issue writes for a live run are skipped and logged, the run's code side
(branch, MR, CI) continues, and the next full sync (every
`FORGE_RECONCILE_EVERY`-th poll, default 10) removes it from the board.

## From the CLI

`uzi issue-tracker list` and `uzi repo issue-tracker-states <repo>` are
read-only. Connecting and linking are in the web only, because they take a token
or change the repo's issue source. See [CLI](./cli.md).
