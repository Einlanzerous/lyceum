/// Relative-time copy for the account surfaces (LYCM-804).
///
/// The two lists deliberately speak different vocabularies. "Your devices" says
/// **used** — you are looking at your own things and asking which one is still
/// live. The household list says **seen** — you are looking at people and asking
/// whether they ever turned up. Same timestamps, different question. Mirrors the
/// web's `seenAt` / `lastSeen` / `expiresIn` in `SettingsView.vue` and
/// `HouseholdView.vue`.
library;

/// "Your devices" sub-line. [now] is injectable so the boundaries are testable.
String deviceUsedAt(DateTime? at, {DateTime? now}) {
  if (at == null) return 'not used yet';
  final hours = (now ?? DateTime.now()).difference(at).inHours;
  if (hours < 1) return 'active now';
  if (hours < 24) return 'last used today';
  final days = hours ~/ 24;
  if (days == 1) return 'last used yesterday';
  return 'last used $days days ago';
}

/// Household row sub-line.
String memberSeenAt(DateTime? at, {DateTime? now}) {
  if (at == null) return 'never signed in';
  final hours = (now ?? DateTime.now()).difference(at).inHours;
  if (hours < 24) return 'last seen today';
  final days = hours ~/ 24;
  if (days == 1) return 'last seen yesterday';
  if (days < 30) return 'last seen $days days ago';
  return 'last seen a while ago';
}

/// How long an outstanding invite has left. Invites live 7 days.
///
/// Rounded throughout, never truncated. `Duration.inDays` floors, so an invite
/// minted three seconds ago has 6 days 23:59:57 left and would announce itself as
/// **"expires in 6 days"** — on a row sitting directly beneath a reveal sheet
/// that says "Expires in 7 days". The same reasoning covers the hours: 50 minutes
/// left is "expires in 1 hour", which is truer than "expires in 0 hours".
String inviteExpiresIn(DateTime? at, {DateTime? now}) {
  if (at == null) return '';
  final left = at.difference(now ?? DateTime.now());
  if (left.isNegative) return 'expired';
  if (left.inHours < 24) {
    final hours = (left.inMinutes / 60).round().clamp(1, 23);
    return 'expires in $hours ${hours == 1 ? 'hour' : 'hours'}';
  }
  final days = (left.inHours / 24).round();
  return 'expires in $days ${days == 1 ? 'day' : 'days'}';
}

/// "1 device" / "3 devices".
String deviceCount(int n) => '$n ${n == 1 ? 'device' : 'devices'}';
