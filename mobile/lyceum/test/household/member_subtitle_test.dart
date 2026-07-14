import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/api/models.dart';
import 'package:lyceum/features/household/household_screen.dart';

/// The household row's second line is the only place the owner can tell a
/// housemate from an invite nobody redeemed. It has to be true.
void main() {
  final now = DateTime.utc(2026, 7, 14, 12);

  Member member({
    bool isOwner = false,
    DateTime? lastSeenAt,
    DateTime? inviteExpiresAt,
    int sessionCount = 0,
  }) => Member(
    account: Account(
      id: isOwner ? 1 : 2,
      email: isOwner ? 'you@home.lan' : 'theo@home.lan',
      displayName: isOwner ? 'Reader' : 'Theo',
      isOwner: isOwner,
    ),
    lastSeenAt: lastSeenAt,
    inviteExpiresAt: inviteExpiresAt,
    sessionCount: sessionCount,
  );

  test('someone who signed out everywhere is not "Active"', () {
    // Caught on a real phone. Theo redeemed an invite, read a book, and signed
    // out. The server derives last_seen_at from his sessions, so revoking the
    // last one took the timestamp with it — and the row read
    // "Active · 0 devices · never signed in". Three claims, two false, all three
    // contradicting each other.
    final theo = member(sessionCount: 0);

    final line = memberSubtitle(theo, now: now);
    expect(line, 'No devices signed in');
    expect(line, isNot(contains('Active')));
    expect(line, isNot(contains('never signed in')));
  });

  test('an invite nobody redeemed is pending, with its real expiry', () {
    final mara = member(
      inviteExpiresAt: now.add(const Duration(days: 7) - const Duration(seconds: 3)),
    );
    expect(
      memberSubtitle(mara, now: now),
      // Not "6 days": the row sits directly under a sheet promising 7.
      'Invite pending · expires in 7 days · never signed in',
    );
  });

  test('an active housemate reads as one', () {
    final theo = member(
      sessionCount: 2,
      lastSeenAt: now.subtract(const Duration(hours: 2)),
    );
    expect(memberSubtitle(theo, now: now), 'Active · 2 devices · last seen today');
  });

  test('a re-invited member who has signed in is active, not pending', () {
    final theo = member(
      sessionCount: 1,
      lastSeenAt: now.subtract(const Duration(hours: 2)),
      inviteExpiresAt: now.add(const Duration(days: 5)),
    );
    expect(memberSubtitle(theo, now: now), startsWith('Active'));
  });

  test('the owner row shows the address and the device count', () {
    expect(
      memberSubtitle(member(isOwner: true, sessionCount: 2), now: now),
      'you@home.lan · 2 devices',
    );
  });
}
