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

  group('inviteSignInUrl', () {
    test('builds a redemption URL and encodes the token', () {
      expect(
        inviteSignInUrl('http://192.168.1.9:8080', 'lyc_a+b'),
        'http://192.168.1.9:8080/sign-in?token=lyc_a%2Bb',
      );
    });

    test('does not double a trailing slash on the origin', () {
      expect(inviteSignInUrl('http://host/', 'lyc_x'), 'http://host/sign-in?token=lyc_x');
    });
  });
}
