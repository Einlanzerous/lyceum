import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/auth/invite_token.dart';

void main() {
  group('extractInviteToken', () {
    test('returns a bare token untouched', () {
      expect(extractInviteToken('lyc_abc123DEF-_'), 'lyc_abc123DEF-_');
    });

    test('strips wrapping whitespace and newlines from a pasted key', () {
      expect(extractInviteToken('  lyc_abc123\n'), 'lyc_abc123');
    });

    test('pulls the token out of a scanned sign-in URL', () {
      expect(
        extractInviteToken('http://192.168.1.9:8080/sign-in?token=lyc_abc123'),
        'lyc_abc123',
      );
    });

    test('url-decodes the token from the query', () {
      expect(
        extractInviteToken('https://lib.example/sign-in?token=lyc_a%2Bb'),
        'lyc_a+b',
      );
    });

    test('rejects a URL with no token param', () {
      expect(extractInviteToken('http://192.168.1.9:8080/sign-in'), isNull);
    });

    test('rejects a non-token string', () {
      expect(extractInviteToken('hello there'), isNull);
    });

    test('rejects the bare prefix with nothing after it', () {
      expect(extractInviteToken('lyc_'), isNull);
    });

    test('rejects empty / whitespace-only input', () {
      expect(extractInviteToken('   '), isNull);
    });
  });

  // The origin is the half that used to be thrown away (LYCM-103): a fresh
  // install has no server address, so the invite naming one is the only way it
  // ever learns where the library is.
  group('extractInvite', () {
    test('returns the origin and the token from a scanned sign-in URL', () {
      expect(
        extractInvite('https://lyceum-direct.example.test/sign-in?token=lyc_a'),
        (token: 'lyc_a', origin: 'https://lyceum-direct.example.test'),
      );
    });

    test('keeps a non-default port', () {
      expect(extractInvite('http://192.168.1.9:8080/sign-in?token=lyc_a'), (
        token: 'lyc_a',
        origin: 'http://192.168.1.9:8080',
      ));
    });

    // The exact inverse of inviteSignInUrl, which appends `/sign-in` to a base
    // that may itself carry a path — a proxy mounting Lyceum under /lyceum.
    // Discarding the path would hand back the proxy's front page.
    test('keeps a path prefix, minus the /sign-in the link added', () {
      expect(
        extractInvite(
          'https://home.example/lyceum/sign-in?token=lyc_a',
        )?.origin,
        'https://home.example/lyceum',
      );
    });

    test('round-trips whatever inviteSignInUrl builds', () {
      const origin = 'https://lyceum-direct.example.test';
      final back = extractInvite(inviteSignInUrl(origin, 'lyc_a+b/c'));
      expect(back, (token: 'lyc_a+b/c', origin: origin));
    });

    test('a bare pasted key names no library', () {
      expect(extractInvite('  lyc_abc123\n'), (
        token: 'lyc_abc123',
        origin: null,
      ));
    });

    // A QR encoding something this app could never talk to must not be allowed
    // to overwrite a server address that works.
    test('ignores an origin it could not reach anyway', () {
      expect(extractInvite('lyceum://open?token=lyc_a')?.origin, isNull);
      expect(extractInvite('file:///tmp/x?token=lyc_a')?.origin, isNull);
    });

    test('rejects the same things extractInviteToken does', () {
      expect(extractInvite('http://192.168.1.9:8080/sign-in'), isNull);
      expect(extractInvite('hello there'), isNull);
      expect(extractInvite('lyc_'), isNull);
      expect(extractInvite('   '), isNull);
    });
  });

  group('pairing codes', () {
    test('normalizes case, hyphen, and spaces', () {
      expect(normalizePairingCode('bk4t-9q2m'), 'BK4T9Q2M');
      expect(normalizePairingCode(' bk 4t '), 'BK4T');
    });

    test('drops glyphs outside the alphabet', () {
      expect(normalizePairingCode('0O1ILU'), '');
    });

    test('recognises a code but never a token', () {
      expect(looksLikePairingCode('BK4T-9Q2M'), isTrue);
      expect(looksLikePairingCode('bk4t9q2m'), isTrue);
      expect(looksLikePairingCode('lyc_abc123'), isFalse);
      expect(looksLikePairingCode('BK4T'), isFalse);
    });
  });

  group('inviteSignInUrl', () {
    test('builds a redemption URL and encodes the token', () {
      expect(
        inviteSignInUrl('http://192.168.1.9:8080', 'lyc_a+b'),
        'http://192.168.1.9:8080/sign-in?token=lyc_a%2Bb',
      );
    });

    test('does not double a trailing slash on the origin', () {
      expect(
        inviteSignInUrl('http://host/', 'lyc_x'),
        'http://host/sign-in?token=lyc_x',
      );
    });
  });
}
