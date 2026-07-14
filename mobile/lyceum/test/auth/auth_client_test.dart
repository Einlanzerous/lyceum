import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/auth/auth_client.dart';

void main() {
  /// Build an AuthClient whose inner transport always answers [status], and
  /// record what it saw.
  ({AuthClient client, List<String?> authHeaders, List<SessionEndReason> fired})
  harness(String token, {int status = 200}) {
    final authHeaders = <String?>[];
    final fired = <SessionEndReason>[];
    final inner = MockClient((req) async {
      authHeaders.add(req.headers['Authorization']);
      return http.Response('', status);
    });
    final client = AuthClient(inner, () => token, fired.add);
    return (client: client, authHeaders: authHeaders, fired: fired);
  }

  group('credential', () {
    test('attaches the session as a bearer header', () async {
      final h = harness('lyc_abc');
      await h.client.get(Uri.parse('http://lib.test/library'));
      expect(h.authHeaders.single, 'Bearer lyc_abc');
    });

    test('sends no header when this device holds no session', () async {
      final h = harness('');
      await h.client.get(Uri.parse('http://lib.test/library'));
      expect(h.authHeaders.single, isNull);
    });

    test('reads the token per request, not at construction', () async {
      // Signing in must authenticate the requests already queued behind it.
      var token = '';
      final seen = <String?>[];
      final client = AuthClient(
        MockClient((req) async {
          seen.add(req.headers['Authorization']);
          return http.Response('', 200);
        }),
        () => token,
        (_) {},
      );

      await client.get(Uri.parse('http://lib.test/library'));
      token = 'lyc_fresh';
      await client.get(Uri.parse('http://lib.test/library'));

      expect(seen, [null, 'Bearer lyc_fresh']);
    });

    test('authHeaders backs an authenticated Image.network', () async {
      expect(harness('lyc_abc').client.authHeaders, {
        'Authorization': 'Bearer lyc_abc',
      });
      expect(harness('').client.authHeaders, isEmpty);
    });
  });

  group('401', () {
    test('a rejected token we held reads as expired', () async {
      final h = harness('lyc_stale', status: 401);
      await h.client.get(Uri.parse('http://lib.test/library'));
      expect(h.fired, [SessionEndReason.expired]);
    });

    test('a 401 with no token held reads as removed', () async {
      final h = harness('', status: 401);
      await h.client.get(Uri.parse('http://lib.test/library'));
      expect(h.fired, [SessionEndReason.removed]);
    });

    test('a success never fires it', () async {
      final h = harness('lyc_abc');
      await h.client.get(Uri.parse('http://lib.test/library'));
      expect(h.fired, isEmpty);
    });

    test('the response still comes back, so the caller errors normally', () async {
      final h = harness('lyc_stale', status: 401);
      final res = await h.client.get(Uri.parse('http://lib.test/library'));
      expect(res.statusCode, 401);
    });
  });

  group('suppression', () {
    test('a rejected invite does not raise the signed-out sheet', () async {
      // The whole point: a bad invite IS a 401, and telling someone in the act of
      // signing *in* that they have been signed out is absurd.
      final h = harness('', status: 401);
      await h.client.suppressUnauthorized(
        () => h.client.post(Uri.parse('http://lib.test/auth/session')),
      );
      expect(h.fired, isEmpty);
    });

    test('re-arms once the suppressed call finishes', () async {
      final h = harness('lyc_stale', status: 401);
      await h.client.suppressUnauthorized(
        () => h.client.get(Uri.parse('http://lib.test/auth/me')),
      );
      await h.client.get(Uri.parse('http://lib.test/library'));
      expect(h.fired, [SessionEndReason.expired]);
    });

    test('overlapping suppressed calls do not re-arm each other', () async {
      // A depth counter, not a flag. The boot /auth/me and a sign-in can overlap;
      // with a bare boolean whichever settled first would re-arm the handler while
      // the other was still in flight, and its 401 would fire the sheet.
      final h = harness('lyc_stale', status: 401);

      late Future<void> inner;
      await h.client.suppressUnauthorized(() async {
        inner = h.client.suppressUnauthorized(
          () => h.client.get(Uri.parse('http://lib.test/auth/me')),
        );
        // The outer call settles first, while the inner one is still running.
        await h.client.get(Uri.parse('http://lib.test/auth/me'));
      });
      await inner;

      expect(h.fired, isEmpty);
    });

    test('still re-arms after a suppressed call throws', () async {
      final h = harness('lyc_stale', status: 401);
      await expectLater(
        h.client.suppressUnauthorized(() => Future<void>.error(StateError('boom'))),
        throwsStateError,
      );
      await h.client.get(Uri.parse('http://lib.test/library'));
      expect(h.fired, [SessionEndReason.expired]);
    });
  });
}
