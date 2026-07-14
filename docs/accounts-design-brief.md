# Design brief — Lyceum Accounts & Household (LYCM-801 / LYCM-802)

*Paste this into Claude Design. It is the input brief; the deliverable is a handoff
like the one that produced `ISBN-Ingest-Handoff.html`.*

---

## The product

**Lyceum** is a self-hosted, DRM-free ebook reader and sync ecosystem. One Go server on
a home network; three clients against it — a Vue 3 web SPA, a native Flutter Android
app, and a Wails Windows shell that embeds the same SPA. It syncs your reading position
across devices by EPUB CFI, so you close a book on your phone and resume mid-sentence on
your laptop.

The scale is a **household, not a SaaS**: two to five people who live together, one
server, no billing, no org chart, no password-reset emails. Design for that. Anything
that feels like enterprise IAM is wrong here.

## The change

Lyceum has been single-user forever. The backend now supports accounts: **several people
share one shelf but keep their own reading positions**, so one person finishing a book no
longer shows everyone else as finished. The whole point is that the library is communal
and the *bookmarks* are private.

The backend shipped and is not up for redesign — design against the contract below. What
does not exist yet is any of the UI, in any client.

## The existing design language — match it

Dark is the default; light is warm paper. A single brass accent. It reads like a private
library, not a dashboard.

```
Dark                          Light ("warm paper")
--bg              #171717     #f7f5f0
--surface         #1c1c1a     #efece4
--surface-raised  #201f1c     #fffdf8
--text            #eaeae5     #1c1a17
--muted           #9a9a92     #6b6660
--dim             #7a7a72     #908a80
--brass           #c99a4e     #9c6f2e   ← the only accent
--brass-bright    #ddb066     #b3853c
--on-brass        #171717     #fffdf8
--border          8% text     10% text
--error           #e08a6e     #b4502f
--success         #5aa86a     #4f8a5e

--font-display  Archivo (500–900)          headings, the big page title
--font-ui       Hanken Grotesk Variable    everything chrome
--font-read     Georgia                    reading surface only
```

Established patterns to reuse rather than reinvent:

- **Page shell** — a back `pill` in a top bar, then a lowercase `eyebrow` ("Preferences",
  "Your library") above a large Archivo `title`.
- **Settings** — `group` (small caps label) → `card` → `row` (name + hint on the left,
  control on the right). Today Settings has three groups: Profile, Connection (native
  shells only), Appearance.
- **Library** — cover grid; a circular **avatar chip** in the top-right header showing a
  single letter, which is currently just a link to Settings.
- **Ingest Verify** — the batch review screen from the previous handoff. That screen's
  density and table-ish rhythm is the reference for anything list-like.

Today's "Profile" is a lie we're replacing: a display name in browser `localStorage`,
never sent to the server, defaulting to "Reader". Its first letter is the avatar. On
upgrade, that local name should fold into the real account.

## The backend contract (fixed — design to this)

**No passwords, ever.** Sign-in is by **one-time invite**. The owner hands out an invite;
redeeming it once yields a durable per-device session. That's the whole model.

| Route | Behavior |
|---|---|
| `POST /auth/session` | Body `{token, device_label}`. Redeems an invite → `{user, session_token}`, and sets a `lyceum_session` cookie. A spent, expired, or unknown invite → **401** (deliberately indistinguishable). |
| `GET /auth/me` | `{id, email, display_name, is_owner}` |
| `PATCH /auth/me` | `{display_name}` — rename yourself |
| `DELETE /auth/session` | Sign out **this device only**; other devices keep working |
| `POST /admin/users` | Owner only. `{email, display_name}` → `{user, invite_token}` — **the token is returned exactly once and is never recoverable** |
| `GET /admin/users` | Owner only. Every account |
| `POST /admin/users/{id}/invite` | Owner only. A fresh invite for an existing member (second device, or a lost token) |
| `DELETE /admin/users/{id}` | Owner only. Remove a member; the **owner cannot be removed** |

Facts that shape the UI:

- An invite is a string like `lyc_9dh79WQf72X2rB4kLm...` — ~47 chars, URL-safe. **People
  will paste it**, so the input must tolerate whitespace and be paste-friendly.
- Invites are **single-use** and **expire after 7 days**.
- There is **exactly one owner** — the person whose server it is. They adopt all
  pre-accounts reading history. They alone can invite and remove.
- On first boot the server **prints an invite for the owner into its own log**. That is
  how the very first human gets in. `lyceum mint-token` on the host reissues one.
- **The shelf is shared; progress is not.** Everyone sees every book. Each person sees
  only their own progress bar and their own resume point.
- Enforcement currently ships **off** (`LYCEUM_AUTH=false`) so existing installs keep
  working. While off, `/admin/*` returns **403** with "household administration requires
  LYCEUM_AUTH". The UI must handle a server where accounts exist but administration is
  switched off.

## What to design

### 1. Sign in — the front door
The first thing a new device sees. Someone has been handed a long opaque token by a
housemate (or found it in the server log) and needs to get in. This should feel like
being handed a key to a house, not filling in a form.

States: empty · pasted · submitting · **rejected** (a single 401 covers wrong, spent,
*and* expired — the copy must be honest about that ambiguity without being useless) ·
server unreachable (the native shells can point at a server that's simply off).

The request also carries a `device_label` ("Pixel 8", "living-room tablet"). Decide
whether to ask for it or infer it silently — see the open questions.

### 2. Account — in Settings
Replaces the fake local "Profile" group. Now server-backed: avatar, display name (an
inline edit that PATCHes), the email, an **Owner** marker if they are one, and **sign
out** — which must clearly mean *this device*, not everywhere.

### 3. Household — owner only, new
List the people on this server. Invite someone. Re-invite. Remove someone. Also needs a
**disabled state** for when the server has `LYCEUM_AUTH=false` — administration is real
but switched off, and the operator has to flip a server setting.

Removing a person deletes their reading positions. Say so.

### 4. The invite reveal — **the hero screen**
This is the moment worth the most care, the way "Verify batch" was last time.

The owner clicks *Invite*. A secret appears **exactly once**. If they navigate away
without copying it, it is gone forever and they must issue another. It expires in 7 days.
It is single-use, so it is a real key, not a link.

Design that moment: the reveal, copy-to-clipboard with real feedback, the honest warning,
and a graceful "you dismissed it — issue another?" path. Getting this wrong means
housemates locked out and a confusing re-issue loop.

### 5. Session expiry / signed-out
A revoked or removed session starts 401ing mid-use — possibly while someone is reading.
What happens to a reader mid-chapter? This should not feel like a crash.

### 6. Library avatar → account
The header chip currently just links to Settings. It now has a real identity behind it.
Does it become a menu (who am I / settings / sign out)? On a **shared living-room tablet**
— explicitly a case Lyceum supports, since two people can read the same book on the same
device — is fast user switching worth it, or is that scope creep?

### 7. Flutter parity
Everything above needs an Android form. It's a hybrid app: **native** library, settings,
and scan screens (Material 3, same tokens), but the **reader is a WebView** loading the
remote SPA. Sign-in and account live on the native side. Assume the same tokens and
information architecture; show where the phone diverges (bottom sheets, back behavior,
the paste affordance on a touch keyboard).

## Hard constraints

- **No passwords, no email delivery, no "forgot password".** The server cannot send mail
  for this. Every recovery path ends at "ask the owner for another invite" or "run
  `lyceum mint-token` on the box".
- **The invite is shown once.** Any design that assumes it can be re-read later is wrong.
- **The owner is not deletable and not transferable** (today).
- **Offline-capable**: no external CDNs, no gravatar, no remote fonts. Avatars are
  generated locally — today just an initial.
- Don't design a "who else is reading this" affordance. That's LYCM-802 and it is
  deliberately a separate decision.

## Open questions — please have a view

1. **Device sessions.** We capture a `device_label` at sign-in but currently show it
   nowhere. Is a "your devices" list (with per-device revoke) worth it for a household of
   four? If you want it, I'll add `GET /auth/sessions` + `DELETE /auth/sessions/{id}` —
   the data is already there.
2. **Device label at sign-in.** Ask the user, or infer from the platform and let them
   rename later? Asking adds a field to the front door; inferring makes a devices list
   less useful.
3. **The upgrade moment.** An existing user already has a local display name and a shelf
   full of progress. When their server turns accounts on, they have to sign in for the
   first time on a device they've been using for months. What does that transition feel
   like, and does their local name carry over automatically?
4. **Shared-device switching.** Real for a living-room tablet, or over-engineering?

## Deliverable

Frames for each surface plus a self-contained **handoff HTML** in the shape of
`ISBN-Ingest-Handoff.html`: every state (not just the happy path), component specs
against the tokens above, and the **exact copy strings** — especially the error and
warning text, which is most of the UX here. Cover dark and light.

Name the states you expect me to implement, and flag anything that needs a backend change
so I can add it rather than have you design around a limitation that isn't real.
