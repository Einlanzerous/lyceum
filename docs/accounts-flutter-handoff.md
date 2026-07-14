# Handoff — Flutter parity for accounts (LYCM-86 / LYCM-804)

Ticket: **LYCM-86**. Design: the Claude Design project *"Lyceum Ebook Library Design"*
(`47922a26-81af-4a45-b80e-73f076bfaead`), file **`Household-Accounts-Handoff.html`**,
surface 7 — *"Flutter parity"*. Read it via the `DesignSync` MCP tool
(`method: "get_file"`). Frames 21–26 in `Lyceum.dc.html` are the web screens; the
Flutter shapes are described in the handoff's copy + state tables, not as separate
frames.

Local brief that produced it: [`docs/accounts-design-brief.md`](accounts-design-brief.md).

---

## Where things stand

**Backend + web are done and merged-pending in PR #43** (branch
`feat/lycm-801-user-accounts`). Lyceum is now a household: several people share one
shelf but keep their own reading positions. No passwords — you redeem a one-time
invite and the device holds a durable session.

**Enforcement still ships OFF** (`LYCEUM_AUTH=false`). That is the only reason the
Android app still works: it sends no credential. **Turning it on before this ticket
lands would lock out every phone.** Flipping it is the last step here.

### The backend contract (fixed — build to it)

| Route | |
|---|---|
| `POST /auth/session` | `{token, device_label}` → `{user, session_token}`, and sets a `lyceum_session` cookie. A wrong / spent / expired invite **all return the same 401** — the server genuinely cannot tell them apart, deliberately. |
| `GET /auth/me` | `{id, email, display_name, is_owner}` |
| `PATCH /auth/me` | `{display_name}` |
| `DELETE /auth/session` | Sign out **this device only** |
| `GET /auth/sessions` | `[{id, device_label, created_at, last_seen_at, current}]` |
| `DELETE /auth/sessions/{id}` | Revoke one of *your own* devices (scoped server-side; someone else's id 404s) |
| `GET /admin/users` | Owner. `[{...user, last_seen_at, invite_expires_at, session_count}]` |
| `POST /admin/users` | Owner. `{email, display_name}` → `{user, invite_token}` — **shown once, never recoverable** |
| `POST /admin/users/{id}/invite` | Owner. A fresh invite for an existing member |
| `DELETE /admin/users/{id}` | Owner. Remove (deletes their positions; the shelf is untouched). The owner can't be removed. |

Facts that shape the UI:

- A session may be presented as **`Authorization: Bearer <token>`** *or* the
  `lyceum_session` cookie. Flutter should use the header.
- Invites are single-use and expire after **7 days**.
- `/admin/*` returns **403** while `LYCEUM_AUTH=false` — "household administration
  requires LYCEUM_AUTH". Not a permissions failure to apologise for; a server that
  can't tell who is asking refuses to mint credentials. It gets its own explained
  state.
- **`GET /auth/me` returning 200 with no token means enforcement is off** and the
  server is serving you as the owner. That is how the client detects an auth-off
  server — there is no separate endpoint.

---

## The Flutter app as it stands

Riverpod + go_router. Hybrid: native library/settings/scan, **WebView reader**.

| What | Where |
|---|---|
| HTTP client | `lib/api/client.dart` — `LyceumClient`, one shared `http.Client` injected at construction (`_http`), every path through `_uri()` (`:43`) |
| Providers | `lib/api/api_providers.dart` — `httpClientProvider`, `lyceumClientProvider` (rebuilds when the server URL changes) |
| Base URL | `lib/api/server_store.dart` — `lyceum.server_url` + `--dart-define=LYCEUM_BASE_URL` |
| Device id (sync) | `lib/api/device.dart` — SharedPreferences key `lyceum.device_id` |
| **The fake profile** | `lib/prefs/profile.dart` — `lyceum.profile_name`, `profileNameProvider` / `profileInitialProvider`. **This is what you're replacing.** Used in `settings_screen.dart` (`_ProfileEditor`, `:125-205`) and `library_screen.dart:73` (avatar). |
| Settings | `lib/features/settings/settings_screen.dart` — `_Group` sections; `server_settings.dart` |
| Reader | `lib/features/reader/reader_screen.dart` — WebView loading `client.readerUrl(id)` = **`$baseUrl/reader/$id` on the server** |
| Covers | `Image.network(coverUrlOf(b.id))` — e.g. `library_search.dart:94` |
| Router | `lib/router/app_router.dart` — `/`, `/settings`, `/scan`, `/reader/:id` |

---

## The plan

### 1. Attach the session — one class, zero call-site changes

The web had to rewrite 18 ad-hoc `fetch()` calls. **Flutter does not**: every request
already goes through one injected `http.Client`. Subclass `http.BaseClient`:

```dart
class AuthClient extends http.BaseClient {
  AuthClient(this._inner, this._token, this._onUnauthorized);
  final http.Client _inner;
  final String? Function() _token;          // read from secure storage
  final void Function() _onUnauthorized;    // surface the signed-out sheet

  @override
  Future<http.StreamedResponse> send(http.BaseRequest req) async {
    final t = _token();
    if (t != null) req.headers['Authorization'] = 'Bearer $t';
    final res = await _inner.send(req);
    if (res.statusCode == 401) _onUnauthorized();
    return res;
  }
}
```

Return it from `httpClientProvider` and `LyceumClient` is authenticated everywhere,
untouched. Store the token in **`flutter_secure_storage`**, not SharedPreferences.

Suppress the 401 reaction during sign-in — a bad invite *is* a 401, and firing a
"you've been signed out" sheet at someone trying to sign *in* is absurd. (The web
uses a nesting counter; see `web/src/api/http.ts`.)

### 2. Covers are easy here — unlike the web

The web's whole cookie mechanism exists because an `<img>` tag **cannot** send an
`Authorization` header. **Flutter's `Image.network` can**:

```dart
Image.network(client.coverUrl(id), headers: {'Authorization': 'Bearer $token'})
```

So no blob/object-URL dance (`web/src/api/coverSrc.ts` exists only for the Wails
shell). Just thread the header through `coverUrlOf`'s call sites.

### 3. The WebView reader — the interesting one

`reader_screen.dart` loads `/reader/{id}` **from the server**, and that page (the web
SPA) then makes its own same-origin `fetch` calls from inside the WebView. **A token
held in Dart does not reach it.**

But the plumbing already exists. `reader_screen.dart:120-123` already injects theme
and font into the page's localStorage:

```dart
"try{localStorage.setItem('lyceum.theme','$theme');"
"localStorage.setItem('lyceum.readingFont','${font.name}');}catch(e){}"
```

…and the web SPA reads its session from **exactly** `localStorage['lyceum.session_token']`
(`web/src/api/http.ts`, `TOKEN_KEY`). So **add one more `setItem`** and the WebView
reader authenticates itself. No new backend surface, no cookie juggling.

Inject it in the *early* hook (`onPageStarted` / the pre-navigation pass), not just
`onPageFinished` — the SPA reads the token when it boots.

**While you're there:** inject `lyceum.device_id` too. Today an Android install has
**two** device ids — the native one in SharedPreferences and a separate one the SPA
generates inside the WebView — so one phone will otherwise occupy two rows in "your
devices" and split its reading positions across two device rows.

### 4. Screens

Per the handoff: native, Material 3, same tokens; **bottom sheets** rather than
modals; Android back = up; a paste affordance above the keyboard.

- **Sign in** — one field (the invite), device label inferred and correctable.
  States: empty · pasted · submitting · rejected (the shared-401 copy) · unreachable.
  Plus the **upgrade moment**: greet a returning reader by their old
  `lyceum.profile_name` and carry it over.
- **Account** in settings — replaces `_ProfileEditor`. Avatar, inline rename
  (`PATCH /auth/me`), Owner badge, sign out (*this device only* — say so).
- **Your devices** — with revoke; mark the current one.
- **Household** (owner only) — members, invite, re-invite, remove-confirm, and the
  auth-off locked state.
- **Invite reveal** — the hero. Shown once. See the pitfalls below.

Exact copy strings are in the handoff HTML; use them verbatim, the error and warning
text *is* most of this UX.

---

## Pitfalls — every one of these bit me on the web

1. **`DELETE` returns 204, not JSON.** Parsing an empty body throws. The Settings
   "Revoke" button blew up *after* the revoke had already succeeded server-side.
2. **The invite reveal's close semantics.** "Copy & close" / "I've saved it" =
   confirmed → just close. The **✕** = walked away → show the "that invite is gone,
   issue another" recovery path. Getting this wrong means telling someone who *just
   copied the key* that they lost it. And **never close on a failed copy** — the
   plaintext is unrecoverable.
3. **Auth-off servers.** Hide "Sign out" and "Your devices" when the server doesn't
   enforce auth (detect: `/auth/me` 200 with no token held). Otherwise signing out
   strands someone on a front door that issues no invites and that they can't get
   past.
4. **Strip whitespace from pasted invites.** They arrive out of chat apps and
   terminal logs, wrapped and padded.
5. **The 401 is ambiguous on purpose.** Don't write copy that guesses "expired" —
   name all three (wrong / spent / expired).
6. **Adopt the legacy `lyceum.profile_name` once**, then clear it, so a name someone
   later changes on the server isn't reverted by a stale local label.

---

## Verifying

A dev server is likely already running (see the session that produced this). To
stand one up:

```sh
docker run -d --name lyceum-dev-pg -e POSTGRES_PASSWORD=dev -e POSTGRES_USER=dev \
  -e POSTGRES_DB=lyceum_dev -p 55433:5432 postgres:16-alpine

go build -o /tmp/lyceum ./cmd/lyceum      # embeds the built SPA (npm run build first)

LYCEUM_ADDR=:4090 \
LYCEUM_DATABASE_URL="postgres://dev:dev@localhost:55433/lyceum_dev?sslmode=disable" \
LYCEUM_DATA_DIR=/tmp/lyceum-data LYCEUM_AUTH=true LYCEUM_INGEST_QC=false \
LYCEUM_OWNER_EMAIL=you@home.lan LYCEUM_OWNER_NAME=You \
  /tmp/lyceum
# first boot prints a one-time owner invite; `lyceum mint-token` issues more
```

From the **Android emulator**, the host is `10.0.2.2` — point the app at
`http://10.0.2.2:4090`, not `localhost`.

Go tests need `TEST_DATABASE_URL` (they skip without it). Flutter: `flutter test`,
`flutter analyze`.

---

## Done when

- Someone redeems an invite on their phone, reads, and sees **their own** progress —
  not a housemate's, on the same book.
- The WebView reader authenticates without a second sign-in.
- One phone = **one** row in "your devices".
- `LYCEUM_AUTH=true` in prod (`~/construct-server/docker-compose.yml`, the `lyceum`
  service), and an unauthenticated request to the reader core is refused.

That last step completes the LYCM-800 epic's client story, and unblocks **SERV-38**
(Purser's Lyceum connector) and **LYCM-85** (Cloudflare Access SSO, which layers on
top of this session mechanism rather than replacing it).
