# Review instructions

What a review of *this* repo is for (LYCM-120). The shared reviewer
(construct-server `docs/pr-reviewer.md`) supplies the procedure; this file
supplies the judgement.

Unlike the other adopting repos, **lyceum has no `CLAUDE.md`** — so this file is
the reviewer's only repo-specific context and carries the how-it-works material
too. If a `CLAUDE.md` ever lands, move the mechanics down and keep the
judgement here.

## What CI already proves — and where it doesn't

Do not spend the review re-proving these. Do not assume more than they say:

| job | proves | reaches |
|---|---|---|
| `backend.yml` / `test` | `go build`, `go vet`, and `go test -race ./...` against a **real Postgres 16** service container | **only when the diff touches `**.go`, `go.mod`, `go.sum`, `migrations/**` or `backend.yml`** |
| `web.yml` / `check` | bun `lint`, `format:check`, `typecheck`, `test`, `build` (vue-tsc) | `web/**` **or `.github/workflows/web.yml`** — path-filtered trigger, so on a PR touching neither it does not report at all |
| `mobile.yml` / `flutter` | `dart format --set-exit-if-changed`, `flutter analyze`, `flutter test`, debug APK build | `mobile/lyceum/**` **or `.github/workflows/mobile.yml`** — same shape |
| `wrappers.yml`, `windows-release.yml`, `mobile-release.yml` | packaging and signing only | releases |

The trap is `backend.yml`. It is a **required check**, so it has to report on
every PR — including PRs with no Go in them, which it satisfies by
short-circuiting to a printed pass. **A green `test` on a web-only PR proves
nothing about the Go tests.** Read what the diff touches before crediting it.

## Ticket fidelity — check this first

When a Switchyard ticket is linked, read its description and exit criteria
before the diff, and answer explicitly in the summary:

- Does the implementation satisfy the stated exit criteria, or only the easy
  subset?
- Did a requirement get silently dropped, narrowed, or deferred without saying?
- Does the PR claim something is verified that the diff does not demonstrate?
  Note the local shape of this: CI runs the Go tests *when Go changed*, so
  "added tests" is credible — what it does not tell you is whether the test
  asserts the thing the ticket asked for. A passing test of the wrong behaviour
  is still a finding.

A change that is clean code and wrong scope is a **🔴 Important** finding. Say
which criterion is unmet and quote it.

When no ticket is linked, say so in one line and review the diff on its own
terms. Do not invent intent from the branch name.

## Severity

- **🔴 Important** — widens who can read or write whose books, loses or corrupts
  library data, leaks a credential, stops the server booting, or does not do
  what the ticket asked.
- **🟡 Nit** — conventions, clarity, a comment that will mislead. Never blocking.
- **🟣 Pre-existing** — real, not introduced here. At most two per review.

Cap nits at five; beyond that say "plus N similar" in the summary. A review that
buries one Important finding under twelve nits has failed at its job.

## Always check

### 1. Which wrapper the route got — and what that means today

Every route is wired in one table, `Handler()` in `internal/api/api.go`. There
are three guards and they have **three different defaults**:

- **`requireUser`** (`session.go`) — needs a session token. But `LYCEUM_AUTH`
  defaults **off**, and while it is off *every gated route is served as the
  owner* (see `WithUserAuth` in `api.go`). So "it's behind `requireUser`" does
  not mean "it's guarded"; it means "guarded once the operator flips the flag."
  Review cross-household leaks as real whether or not auth is on today.
- **`requireOwner`** (`session.go`) — `requireUser` plus an ownership check, and
  it **refuses outright (403) while auth is off**. That is deliberate and worth
  preserving: with auth off, anyone who can reach the port could otherwise mint
  themselves an invite, redeem it for a durable session, and still hold it after
  the operator turns auth on — escalating straight through the step meant to
  close the door.
- **`requireScope`** (`auth.go`) — service tokens for the ecosystem hooks
  (`delivery:send`, `eidolon:read`). **Closed by default**: no token table
  configured means 401, the opposite default from `requireUser`.

So: a new ecosystem hook wrapped in `requireUser` instead of `requireScope` is
open where its neighbours are shut. And **a bare `mux.HandleFunc` with no
wrapper is unauthenticated in every mode.** Exactly two are legitimate today —
`POST /auth/session` and `POST /auth/sso/cloudflare`, both of which *are* how a
client gets a credential. A third one needs an argument in the diff.

### 2. Per-user versus household

This repo's most repeated defect. LYCM-112 shipped mark-as-read as a
library-wide flag: one housemate marking a book read marked it for everyone.

Reading position, `finished`, and read marks are **the caller's own**; title,
author, cover, series and review state are **the household's**. For any new
column, query or response field, say which side it is on. A store query that
forgets its user scope compiles, passes its test, and silently shares.

### 3. Invites, tokens, and the origin they advertise

Sign-in is invite-based: single-use, TTL'd, redeemed for a durable session bound
to a device. Two invariants the code already states and a change can quietly
break:

- **A token is returned exactly once, by the call that mints it.** `userJSON`
  carries no token material on purpose. Anything that puts token material into a
  second response, a log line, or a URL that outlives its redemption is
  Important.
- **`WithMobileBaseURL` decides which origin a QR advertises** (LYCM-102/103) —
  the public bearer-authenticated host a phone can reach, not the
  Cloudflare-gated one the owner's browser is on. A malformed base is *dropped*
  rather than stored, because clients prefer it over the link they would have
  built: keeping a bad value would emit a QR that scans nowhere **and** suppress
  the fallback that still worked. Preserve that direction.

### 4. Path identity in ingest

The folder watcher must ignore two respelling axes: **case**, and Unicode
**NFC/NFD** (LYCM-109). It matters more than it looks because a deploy restart
re-scans the whole tree — so a path-identity regression does not fail a test, it
shows up later as the library silently duplicating itself.
`internal/store/sources.go`, `internal/api/watch.go`.

### 5. Destructive paths

Delete, the duplicate hold (LYCM-113), and QC approve all remove or hide a book
the user can see. Check the pointers survive: `duplicate_of` must stop naming a
book once that book is deleted. Check a hold is reversible.

### 6. Config read as configured

`LYCEUM_BINDERY_API_KEY` unset makes the acquirer a **silent no-op** —
`find_digital` marks inventory `wanted` and grabs nothing. That was prod's live
state for weeks, and it looked like a working feature. New configuration must
have a stated behaviour when unset, and `main.go` should log which branch it
took.

### 7. Credentials in output

LYCM-119 put the database password in `lyceum --help`. `DATABASE_URL` carries a
password; anything that prints config, echoes flags, or wraps a DSN into an
error message is a candidate.

### 8. Migrations

`migrations/` is applied in order at startup by `store.Migrate`, and a failure
is **fatal** — a bad migration does not fail a test, it stops the server
booting. Every `.up.sql` needs its `.down.sql`. Prefer additive.

### 9. Query counts

LYCM-115: the shelf render was 1+2N. `internal/api/querycount_test.go` is the
guard. A new per-book lookup inside a library loop belongs in a batch.

## How the repo works, in the four lines a reviewer needs

- Go server (`cmd/lyceum`, `internal/`), Vue SPA (`web/`), Flutter Android app
  (`mobile/lyceum/`), Wails/Capacitor desktop wrappers (`wrappers/`).
- Postgres, shared with the rest of the estate. Migrations run at boot.
- The Flutter app is hybrid: native library and settings, WebView epub.js reader,
  so reading positions stay CFI-compatible with the web reader.
- **Merging does not deploy.** `publish.yml` pushes `:latest`; production only
  moves on a construct-server deploy dispatch. Do not describe a merge as
  shipping.

## Verification bar

Report a finding only when you can point at the line that causes it and name the
concrete failure — the input, state, or sequence that produces the wrong
outcome. "This could be risky" is not a finding.

Behaviour inferred from a name is not evidence. If you find yourself writing
"this may not handle…", go read the implementation or drop it. For auth findings
specifically, trace the actual call path and say which of the three guards
applies — several handlers that look unguarded are guarded in the route table.

## Re-reviews

Round three should be shorter than round one. After the first review of a PR:
report **new Important findings only**. No new nits, no restating open findings,
no re-raising something the author explicitly declined. Note in one line what got
fixed, then move on.

## Summary shape

Open with a one-line tally — `2 important, 1 nit` — or **No blocking issues**.
Then ticket fidelity in a sentence. Then findings, most severe first.

If the diff is clean, say so in one line and stop. Do not pad.
