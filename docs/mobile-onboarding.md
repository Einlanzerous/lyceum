# Getting the Android app onto a phone

**Lyceum is a client for a library you run.** It is not a bookstore and not a
service with an account you can sign up for: with no server behind it the app is
an empty shelf and a prompt asking to be pointed at one. Two things are needed
before it does anything — **a Lyceum server**, and **an invite from whoever runs
it**.

That is the whole of the onboarding story, and the rest of this page is what it
looks like from each side.

## For the person installing it

1. **Install the app** — from the Play closed-testing link the owner sends you,
   or by sideloading the signed APK attached to a [GitHub
   Release](https://github.com/Einlanzerous/lyceum/releases).
2. **Ask the owner for an invite.** It arrives as a QR code on their screen.
3. **Open Lyceum.** A fresh install lands on *Connect to your library* — no
   address, no account, nothing to type.
4. **Tap Scan invite** and point the camera at the QR.
5. **That's it.** The shelf loads.

One scan settles both halves of the problem, which is why there is no step where
you type a server address. The QR encodes `<server>/sign-in?token=…`
(LYCM-102/103): the app takes the address off the front of that link and points
itself at the library, then redeems the token on the end of it to sign this
device in. It checks the address answers before committing to it, so a QR for a
server this phone cannot see leaves the app exactly as it was rather than
stranding it on a dead address.

**Invites are one-time and expire after 7 days.** A scanned invite is spent; a
second device needs a second invite. If yours has gone stale, ask for another —
nothing is lost by re-issuing.

### When there is no QR

Some keys arrive with no server address attached: a bare `lyc_…` token pasted
into a chat, or the short pairing code the owner can read out over the phone.
Neither can say *which* library it belongs to, so those still need the address
typed once. Tap **Enter a server address instead**, put in the server URL, hit
**Test** (it pings `/healthz`) then **Save**, and then paste the token or code.

The same manual path is what a LAN address or a dev box wants.

### If something goes wrong

| What you see | What it means |
| --- | --- |
| *Couldn't reach `<address>`* | The phone can't get to that server — check the phone's network, and that the server is up and reachable from outside the house if you're not on its LAN. The app keeps the library it was on. |
| The key is refused | Spent, expired, or mistyped. The server doesn't say which, on purpose. Ask for a fresh invite. |
| *Connect to a different library?* | You scanned an invite for a **different** server than the one this phone is already on. Connecting signs the device out of the old library — the books and reading positions stay on that server, and another invite brings you back. |
| Too many tries | Pairing-code sign-in is rate-limited; the code space is short enough to guess at. Wait a minute. |

## For the owner handing out invites

Set **`LYCEUM_MOBILE_BASE_URL`** to the public address a phone can actually
reach — the direct edge, not the SSO-gated hostname you administer from. Every
invite the server mints then carries a ready-made sign-in URL built from it, and
that is what makes a single scan enough. Left unset, invites carry no URL and
clients fall back to building one from the origin they happen to know, which is
right on a LAN and wrong the moment the owner is minting invites from behind
Cloudflare Access. See `.env.example`.

Then, with `LYCEUM_AUTH=true`:

- **A new person** — web app → **Household** (owner only) → invite a member. The
  invite is revealed once, with its QR.
- **Another of your own devices** — web app → **Settings → Your devices**. No
  ownership needed: the session asking already has everything the new one will.
- **From the host, with nothing signed in** — `lyceum mint-token` prints an
  invite and a scannable QR in the terminal. This is the bootstrap path, and the
  one with no client-side fallback if `LYCEUM_MOBILE_BASE_URL` is wrong.

While `LYCEUM_AUTH=false` the `/admin` routes refuse outright — a server that
can't tell who is asking shouldn't be minting credentials — so use
`lyceum mint-token` on the host.

## Why the store build knows nothing about your server

The public build is deliberately shipped with **no server address in it at all**
(LYCM-104). It has to be: an app that arrived from the Play Store already
pointed at somebody's house would be pointing every stranger who installed it at
a private household's books, and it would skip the connect prompt that scanning
starts from.

This is enforced rather than remembered. `mobile/lyceum/tool/check_store_build.sh`
reads the release build config and then the built APK and AAB themselves, and
fails the release if either names a private host or if the Dart snapshot holds
any absolute URL that isn't expected. It runs on every mobile PR and again in
`mobile-release.yml` before anything is attached to a release or pushed to Play.

The tradeoff is the one visible in the store listing: a new install cannot do
anything until someone invites it. That is the correct behaviour for a
self-hosted client, and the listing copy says so up front — see
[`mobile/lyceum/android/store-listing.md`](../mobile/lyceum/android/store-listing.md).

Households that would rather skip even the one scan can build a private,
sideload-only flavour with the address compiled in; it must never reach a public
track. See [the app README](../mobile/lyceum/README.md#a-private-family-build).
