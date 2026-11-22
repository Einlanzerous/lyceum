# Active-users count API (LYCM-140)

One route for Satlink, the estate health dashboard: how many accounts used a
session recently. It is a count and nothing else. Lyceum has no OpenAPI file, so
the contract lives here, beside [`eidolon-api.md`](eidolon-api.md).

## Auth

A database service token with scope `admin:read`, sent as
`Authorization: Bearer <token>`. Mint one on the server (it is printed once, and
only its SHA-256 is stored):

```sh
docker exec lyceum lyceum service-token mint --label satlink --scope admin:read
docker exec lyceum lyceum service-token list
docker exec lyceum lyceum service-token revoke --id N      # or --label satlink
```

One scope per token. An `admin:read` token reaches this route and no other: not
`GET /admin/users`, not the reader core, not `POST /auth/session`. A user session
does not reach it either (`401`). Rotating is mint, update the consumer, revoke
the old id; both tokens are valid in between and no restart is needed.

| Status | Meaning |
|---|---|
| `200` | the count below |
| `401` | no token, unknown token or revoked token (one body for all three) |
| `403` | a live token without `admin:read` |
| `503` | the database is unreachable, **or** `LYCEUM_AUTH` is off (see below) |

## GET /admin/users/active

No parameters. The window is fixed at 900 seconds and a query string is ignored.

```json
{ "active": 3, "window_seconds": 900, "asOf": "2026-11-21T22:10:53.123456Z" }
```

- `active`: distinct accounts with a session whose `last_used_at` is within the
  last 900 s. An account with two devices counts once; a never-used session does
  not count.
- `asOf`: the database clock's `now()` at the moment of the count, which is also
  the clock that stamped `last_used_at`.
- The response carries no email, name, id or per-user timestamp, and its Go type
  is an allow-list that does not embed the user or member shapes.

### Auth off is unknown, not zero

Sessions only record use while `LYCEUM_AUTH` is on. With it off the route answers
`503` with a plain-text reason, never `{"active": 0}`: the count is not known,
and a consumer must treat it as unavailable. (`403`, as the owner-only admin
routes give, would tell the caller its credential is wrong, which it is not.)

## Audit

`mint` and `revoke` each write an `audit_log` row (actor `cli:<os user>`) in the
same transaction as the change. A refused unknown or revoked token writes one
row per hash prefix per hour (the first 12 hex characters of its SHA-256, never
the token), with the label when the token was known. `last_used_at` on a token is
stamped at most once a minute.
