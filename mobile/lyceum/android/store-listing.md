# Play listing copy

Source of truth for what goes in the Play Console listing fields. Kept in the
repo because the listing is doing real work: it is the only thing standing
between a store browser and an app that, correctly, does nothing on its own.

**The expectation to set, in the first line the reader sees: Lyceum needs a
server you run, and an invite from whoever runs it.** A store install starts with
an empty server field on purpose — the invite QR supplies the address (LYCM-104)
— and someone who arrives expecting a bookstore will read that empty field as a
broken app and leave a one-star review saying so. Say it before they find out.

---

## App name (30 chars max)

```
Lyceum
```

## Short description (80 chars max)

Shown under the title in search results. It has one job here.

```
Reader for your own self-hosted Lyceum library. Needs your server + an invite.
```

## Full description (4000 chars max)

```
Lyceum is the Android reader for a Lyceum ebook server — one that you, or
someone in your household, runs.

REQUIRES A SERVER AND AN INVITE
This is a client, not a service. There is no Lyceum account to sign up for and
no catalogue to browse. Before the app can show you anything you need:

  • a Lyceum server (free and open source — you host it yourself), and
  • an invite from whoever runs it.

If you don't have both, this app will have nothing to show you.

GETTING IN TAKES ONE SCAN
Install it, and the first screen asks to be connected to your library. The owner
shows you an invite QR; you scan it. That single scan both points the app at the
right server and signs this device in — there is no address to type and no
password to remember. (A typed address is still there behind "Enter a server
address instead", for a library on your own LAN or a key that arrived without
one.)

READING
  • Your whole shelf, with covers, search and sorting
  • Reading position syncs across every device on the library — put a book down
    on your phone and pick it up in the browser at the same paragraph
  • Your place is yours: a household shares one shelf, but everyone keeps their
    own progress
  • Adjustable reading font and a light/dark theme
  • Scan a book's barcode to add it to the library

PRIVACY
The app talks to your server and to nothing else. No analytics, no advertising,
no tracking, no accounts with us — there is no "us" in the data path at all.
Your library and your reading history live on your own machine, under your own
rules.

DRM-free EPUB only. Lyceum does not open books from other stores' protected
formats.

Open source: https://github.com/Einlanzerous/lyceum
```

## Other listing fields

| Field | Value |
| --- | --- |
| Category | Books & Reference |
| Contains ads | No |
| In-app purchases | No |
| Content rating | Everyone (no user-generated content is exchanged through us — the app talks only to the user's own server) |
| Privacy policy URL | the hosted copy of [`PRIVACY.md`](../../../PRIVACY.md) |
| Data safety | No data collected, no data shared. The server address and session token are stored on-device and never sent to the developer. |

## App access (the one Play reviewers get stuck on)

Play's reviewers cannot use this app without credentials, and "you need to run a
server first" is not something a reviewer will do. Under **App access**, declare
that *all or some functionality is restricted* and give instructions plus working
demo credentials:

- a reachable demo Lyceum server URL, and
- a **long-lived** invite or session for it (a normal invite is single-use and
  expires in 7 days — it will be dead by the time anyone looks at it, and the
  review comes back rejected for a login that doesn't work).

Point them at *Enter a server address instead* on the first screen, since they
will have a URL and a token rather than a QR to scan.
