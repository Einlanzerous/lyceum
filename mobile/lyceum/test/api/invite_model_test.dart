import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/api/models.dart';

/// Parsing `sign_in_url` off a minted invite (LYCM-102).
///
/// The server sends it only when it has been told which origin a phone can
/// reach; every other case has to come back null so the caller falls through to
/// the address this device is configured with, rather than building a QR out of
/// an empty string.
void main() {
  const user = {
    'id': 2,
    'email': 'theo@home.lan',
    'display_name': 'Theo',
    'is_owner': false,
  };

  Invite parse(Map<String, dynamic> extra) => Invite.fromJson({
    'user': user,
    'invite_token': 'lyc_abc',
    'pairing_code': 'ABCD2345',
    ...extra,
  });

  group('Invite.signInUrl', () {
    test('carries the server-built link when one is sent', () {
      final invite = parse({
        'sign_in_url':
            'https://lyceum-direct.example.test/sign-in?token=lyc_abc',
      });

      expect(
        invite.signInUrl,
        'https://lyceum-direct.example.test/sign-in?token=lyc_abc',
      );
      // The rest of the payload is untouched by the addition.
      expect(invite.token, 'lyc_abc');
      expect(invite.pairingCode, 'ABCD2345');
    });

    test('is null when the server sends no link', () {
      expect(parse(const {}).signInUrl, isNull);
    });

    // A blank or whitespace-only value is a misconfiguration, not a link. Left
    // as an empty string it would read as truthy to the reveal flow and produce
    // a QR that resolves nowhere.
    test('is null for an empty or whitespace link', () {
      expect(parse(const {'sign_in_url': ''}).signInUrl, isNull);
      expect(parse(const {'sign_in_url': '   '}).signInUrl, isNull);
    });

    test('is null when the field is explicitly null', () {
      expect(parse(const {'sign_in_url': null}).signInUrl, isNull);
    });
  });
}
