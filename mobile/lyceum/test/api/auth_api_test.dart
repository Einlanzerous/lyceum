import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/client.dart';

void main() {
  LyceumClient clientFor(MockClient mock) =>
      LyceumClient(baseUrl: 'http://lib.test', deviceId: 'dev-1', httpClient: mock);

  const user = {
    'id': 2,
    'email': 'theo@home.lan',
    'display_name': 'Theo',
    'is_owner': false,
  };

  group('redeemInvite', () {
    test('strips every scrap of whitespace from a pasted key', () async {
      // Invites arrive out of chat apps and terminal logs — hard-wrapped, padded,
      // sometimes broken across two lines. It is still the key they were given.
      //
      // The fixture is deliberately a readable, low-entropy string rather than a
      // realistic-looking key: secret scanners flag the latter on sight, and a
      // test that cries wolf on every CI run is a test people learn to ignore.
      late http.Request seen;
      final client = clientFor(
        MockClient((req) async {
          seen = req;
          return http.Response(
            jsonEncode({'user': user, 'session_token': 'lyc_example_session'}),
            200,
            headers: {'content-type': 'application/json'},
          );
        }),
      );

      final result = await client.redeemInvite(
        '  lyc_example\ninvite_key \t',
        deviceLabel: 'Pixel 8',
      );

      final body = jsonDecode(seen.body) as Map<String, dynamic>;
      expect(body['token'], 'lyc_exampleinvite_key');
      expect(body['device_label'], 'Pixel 8');
      expect(result.sessionToken, 'lyc_example_session');
      expect(result.user.displayName, 'Theo');
    });

    test('a wrong, spent, or expired invite is one indistinguishable 401', () async {
      final client = clientFor(
        MockClient((_) async => http.Response('invalid or already-used invite token', 401)),
      );
      await expectLater(
        client.redeemInvite('lyc_nope', deviceLabel: 'Pixel 8'),
        throwsA(
          isA<ApiException>()
              .having((e) => e.isUnauthorized, 'isUnauthorized', isTrue)
              .having((e) => e.message, 'message', 'invalid or already-used invite token'),
        ),
      );
    });
  });

  group('fetchMe', () {
    test('200 with no token held is how an auth-off server is detected', () async {
      final client = clientFor(
        MockClient(
          (_) async => http.Response(
            jsonEncode({
              'id': 1,
              'email': 'you@home.lan',
              'display_name': 'You',
              'is_owner': true,
            }),
            200,
            headers: {'content-type': 'application/json'},
          ),
        ),
      );
      final me = await client.fetchMe();
      expect(me?.isOwner, isTrue);
      expect(me?.initial, 'Y');
    });

    test('401 is an answer, not an error — we are simply signed out', () async {
      final client = clientFor(MockClient((_) async => http.Response('nope', 401)));
      expect(await client.fetchMe(), isNull);
    });
  });

  group('empty bodies', () {
    // The pitfall that bit the web: DELETE answers 204 No Content, and parsing an
    // empty body throws — so a *successful* revoke blew up in the caller and the
    // list never refreshed.
    test('signOut accepts 204 without trying to parse a body', () async {
      final client = clientFor(MockClient((_) async => http.Response('', 204)));
      await expectLater(client.signOut(), completes);
    });

    test('revokeDevice accepts 204', () async {
      late http.Request seen;
      final client = clientFor(
        MockClient((req) async {
          seen = req;
          return http.Response('', 204);
        }),
      );
      await client.revokeDevice(9);
      expect(seen.method, 'DELETE');
      expect(seen.url.path, '/auth/sessions/9');
    });

    test('removeMember accepts 204', () async {
      final client = clientFor(MockClient((_) async => http.Response('', 204)));
      await expectLater(client.removeMember(3), completes);
    });
  });

  group('devices', () {
    test('parses the list and trusts the server to mark the current one', () async {
      final client = clientFor(
        MockClient(
          (_) async => http.Response(
            jsonEncode([
              {
                'id': 1,
                'device_label': 'Pixel 8',
                'created_at': '2026-07-01T10:00:00Z',
                'last_seen_at': '2026-07-14T09:00:00Z',
                'current': true,
              },
              {
                'id': 2,
                'device_label': 'Surface Laptop',
                'created_at': '2026-06-01T10:00:00Z',
                'last_seen_at': null,
                'current': false,
              },
            ]),
            200,
            headers: {'content-type': 'application/json'},
          ),
        ),
      );

      final devices = await client.listDevices();
      expect(devices.map((d) => d.deviceLabel), ['Pixel 8', 'Surface Laptop']);
      expect(devices[0].current, isTrue);
      expect(devices[1].lastSeenAt, isNull);
    });
  });

  group('household', () {
    test('the auth-off 403 is the explained, locked state', () async {
      // Verbatim from the server (internal/api/session.go requireOwner).
      final client = clientFor(
        MockClient(
          (_) async => http.Response(
            'household administration requires LYCEUM_AUTH; use `lyceum '
            'mint-token` on the server to issue a sign-in invite',
            403,
          ),
        ),
      );
      await expectLater(client.listMembers(), throwsA(isA<AdminDisabledException>()));
      await expectLater(
        client.inviteMember(email: 'x@y.z', displayName: ''),
        throwsA(isA<AdminDisabledException>()),
      );
      await expectLater(client.removeMember(2), throwsA(isA<AdminDisabledException>()));
    });

    test("a member's 403 is NOT the auth-off state", () async {
      // /admin answers two different 403s. Telling a housemate who tripped the
      // owner-only check to go and `export LYCEUM_AUTH=true` on the server would
      // send them off to fix a machine that is working perfectly.
      final client = clientFor(MockClient((_) async => http.Response('owner only', 403)));
      await expectLater(
        client.listMembers(),
        throwsA(
          isA<ApiException>().having(
            (e) => e,
            'is not AdminDisabled',
            isNot(isA<AdminDisabledException>()),
          ),
        ),
      );
    });

    test('the owner cannot be removed — 403, also not the auth-off state', () async {
      final client = clientFor(
        MockClient((_) async => http.Response('the owner account cannot be removed', 403)),
      );
      await expectLater(
        client.removeMember(1),
        throwsA(isNot(isA<AdminDisabledException>())),
      );
    });

    test('an invite comes back 201 with the plaintext key, once', () async {
      final client = clientFor(
        MockClient(
          (_) async => http.Response(
            jsonEncode({'user': user, 'invite_token': 'lyc_theOnlyCopy'}),
            201,
            headers: {'content-type': 'application/json'},
          ),
        ),
      );
      final invite = await client.inviteMember(email: 'theo@home.lan', displayName: 'Theo');
      expect(invite.token, 'lyc_theOnlyCopy');
      expect(invite.user.displayName, 'Theo');
    });

    test('a duplicate email is a 409, distinct from the admin-off 403', () async {
      final client = clientFor(
        MockClient((_) async => http.Response('email is already registered', 409)),
      );
      await expectLater(
        client.inviteMember(email: 'theo@home.lan', displayName: ''),
        throwsA(
          isA<ApiException>()
              .having((e) => e.isDuplicate, 'isDuplicate', isTrue)
              .having((e) => e, 'is not AdminDisabled', isNot(isA<AdminDisabledException>())),
        ),
      );
    });

    test('pending is derived: an invite outstanding and never signed in', () async {
      final client = clientFor(
        MockClient(
          (_) async => http.Response(
            jsonEncode([
              {
                'id': 1,
                'email': 'you@home.lan',
                'display_name': 'You',
                'is_owner': true,
                'last_seen_at': '2026-07-14T09:00:00Z',
                'invite_expires_at': null,
                'session_count': 2,
              },
              {
                ...user,
                'last_seen_at': null,
                'invite_expires_at': '2026-07-20T09:00:00Z',
                'session_count': 0,
              },
              {
                'id': 3,
                'email': 'mara@home.lan',
                'display_name': 'Mara',
                'is_owner': false,
                // Re-invited for a second device: has an outstanding invite AND
                // has signed in. She is active, not pending.
                'last_seen_at': '2026-07-13T09:00:00Z',
                'invite_expires_at': '2026-07-20T09:00:00Z',
                'session_count': 1,
              },
            ]),
            200,
            headers: {'content-type': 'application/json'},
          ),
        ),
      );

      final members = await client.listMembers();
      expect(members[0].isOwner, isTrue);
      expect(members[0].isPending, isFalse);
      expect(members[1].isPending, isTrue);
      expect(members[2].isPending, isFalse);
    });
  });
}
