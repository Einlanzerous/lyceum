import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/api/models.dart';
import 'package:lyceum/auth/invite_flow.dart';

/// Which origin the owner's own phone puts in the QR it hands out (LYCM-102).
///
/// An owner minting an invite from the sofa is on the LAN address. Encoding that
/// produces a key that works only for someone already inside the house — not the
/// person being invited, who is by definition somewhere else. The server knows
/// the reachable origin, so when it says, it wins.
void main() {
  const account = {
    'id': 2,
    'email': 'theo@home.lan',
    'display_name': 'Theo',
    'is_owner': false,
  };

  Invite inviteWith({String? signInUrl}) => Invite.fromJson({
    'user': account,
    'invite_token': 'lyc_abc',
    'pairing_code': 'ABCD2345',
    // Absent rather than null when unset, matching a payload from a server with
    // no mobile origin configured (the field is `omitempty`).
    'sign_in_url': ?signInUrl,
  });

  group('revealSignInUrl', () {
    test('prefers the server-advertised link over this phone address', () {
      final url = revealSignInUrl(
        inviteWith(
          signInUrl: 'https://lyceum-direct.example.test/sign-in?token=lyc_abc',
        ),
        'http://192.168.1.9:8080',
      );

      expect(url, 'https://lyceum-direct.example.test/sign-in?token=lyc_abc');
      expect(url, isNot(contains('192.168.1.9')));
    });

    test('falls back to this phone address when the server sends none', () {
      expect(
        revealSignInUrl(inviteWith(), 'http://192.168.1.9:8080'),
        'http://192.168.1.9:8080/sign-in?token=lyc_abc',
      );
    });

    // Blank is a misconfiguration, not an origin — it must not beat the fallback.
    test('falls back when the advertised link is blank', () {
      expect(
        revealSignInUrl(
          inviteWith(signInUrl: '   '),
          'http://192.168.1.9:8080',
        ),
        'http://192.168.1.9:8080/sign-in?token=lyc_abc',
      );
    });

    test('offers no QR when there is no address at all', () {
      expect(revealSignInUrl(inviteWith(), ''), isNull);
    });

    // The server's answer stands on its own: it does not need this device to be
    // configured, which is what makes it usable before the fallback would exist.
    test('uses the advertised link even with no configured address', () {
      expect(
        revealSignInUrl(
          inviteWith(
            signInUrl:
                'https://lyceum-direct.example.test/sign-in?token=lyc_abc',
          ),
          '',
        ),
        'https://lyceum-direct.example.test/sign-in?token=lyc_abc',
      );
    });
  });
}
