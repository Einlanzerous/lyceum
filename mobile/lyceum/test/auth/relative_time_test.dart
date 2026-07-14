import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/auth/device_label.dart';
import 'package:lyceum/auth/relative_time.dart';

void main() {
  final now = DateTime.utc(2026, 7, 14, 12);
  DateTime ago(Duration d) => now.subtract(d);

  group('deviceUsedAt', () {
    test('a session minted but never used', () {
      expect(deviceUsedAt(null, now: now), 'not used yet');
    });

    test('within the hour is live', () {
      expect(deviceUsedAt(ago(const Duration(minutes: 5)), now: now), 'active now');
      expect(deviceUsedAt(ago(const Duration(minutes: 59)), now: now), 'active now');
    });

    test('crosses to today at one hour, not at midnight', () {
      expect(deviceUsedAt(ago(const Duration(hours: 1)), now: now), 'last used today');
      expect(deviceUsedAt(ago(const Duration(hours: 23)), now: now), 'last used today');
    });

    test('yesterday spans the whole second day', () {
      expect(deviceUsedAt(ago(const Duration(hours: 24)), now: now), 'last used yesterday');
      expect(deviceUsedAt(ago(const Duration(hours: 47)), now: now), 'last used yesterday');
    });

    test('then counts days', () {
      expect(deviceUsedAt(ago(const Duration(days: 2)), now: now), 'last used 2 days ago');
      expect(deviceUsedAt(ago(const Duration(days: 90)), now: now), 'last used 90 days ago');
    });
  });

  group('memberSeenAt', () {
    test('an invite nobody redeemed', () {
      expect(memberSeenAt(null, now: now), 'never signed in');
    });

    test('says seen, not used — this is a person, not a handset', () {
      expect(memberSeenAt(ago(const Duration(minutes: 5)), now: now), 'last seen today');
      expect(memberSeenAt(ago(const Duration(hours: 30)), now: now), 'last seen yesterday');
      expect(memberSeenAt(ago(const Duration(days: 5)), now: now), 'last seen 5 days ago');
    });

    test('stops counting past a month', () {
      expect(memberSeenAt(ago(const Duration(days: 29)), now: now), 'last seen 29 days ago');
      expect(memberSeenAt(ago(const Duration(days: 30)), now: now), 'last seen a while ago');
    });
  });

  group('inviteExpiresIn', () {
    DateTime ahead(Duration d) => now.add(d);

    test('a fresh invite lives 7 days', () {
      expect(inviteExpiresIn(ahead(const Duration(days: 7)), now: now), 'expires in 7 days');
    });

    test('singular reads properly', () {
      expect(inviteExpiresIn(ahead(const Duration(days: 1, hours: 2)), now: now), 'expires in 1 day');
    });

    test('under a day, counts hours — rounded, never "0 hours"', () {
      expect(inviteExpiresIn(ahead(const Duration(hours: 5)), now: now), 'expires in 5 hours');
      expect(inviteExpiresIn(ahead(const Duration(minutes: 50)), now: now), 'expires in 1 hour');
      expect(inviteExpiresIn(ahead(const Duration(minutes: 2)), now: now), 'expires in 1 hour');
    });

    test('a lapsed invite says so', () {
      expect(inviteExpiresIn(ago(const Duration(hours: 1)), now: now), 'expired');
    });
  });

  group('deviceCount', () {
    test('pluralizes', () {
      expect(deviceCount(0), '0 devices');
      expect(deviceCount(1), '1 device');
      expect(deviceCount(3), '3 devices');
    });
  });

  group('composeDeviceLabel', () {
    test('leads with the make, because "SM-G991B" means nothing alone', () {
      expect(
        composeDeviceLabel(manufacturer: 'samsung', model: 'SM-G991B'),
        'Samsung SM-G991B',
      );
      expect(
        composeDeviceLabel(manufacturer: 'Google', model: 'Pixel 8'),
        'Google Pixel 8',
      );
    });

    test('does not stutter when the model already carries the make', () {
      expect(
        composeDeviceLabel(manufacturer: 'OnePlus', model: 'OnePlus 12'),
        'OnePlus 12',
      );
    });

    test('survives a device that reports nothing useful', () {
      expect(composeDeviceLabel(manufacturer: '', model: ''), 'Android device');
      expect(composeDeviceLabel(manufacturer: 'Nokia', model: ''), 'Nokia');
      expect(composeDeviceLabel(manufacturer: '', model: 'Pixel 8'), 'Pixel 8');
    });
  });
}
