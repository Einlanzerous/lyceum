import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/api_providers.dart';
import 'package:lyceum/api/client.dart';
import 'package:lyceum/api/server_store.dart';
import 'package:lyceum/auth/auth_client.dart';
import 'package:lyceum/auth/auth_controller.dart';
import 'package:lyceum/auth/session_store.dart';
import 'package:lyceum/prefs/prefs.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_controller_test_support.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const owner = {
    'id': 1,
    'email': 'you@home.lan',
    'display_name': 'You',
    'is_owner': true,
  };

  /// A container wired to [handler], with a fake keystore and prefs.
  Future<({ProviderContainer container, FakeTokenStore store})> build({
    required Future<http.Response> Function(http.Request) handler,
    String initialToken = '',
    Map<String, Object> prefs = const {},
  }) async {
    SharedPreferences.setMockInitialValues(prefs);
    final sp = await SharedPreferences.getInstance();
    final store = FakeTokenStore(initialToken);

    final container = ProviderContainer(
      overrides: [
        prefsProvider.overrideWithValue(sp),
        tokenStoreProvider.overrideWithValue(store),
        initialSessionTokenProvider.overrideWithValue(initialToken),
        httpClientProvider.overrideWithValue(MockClient(handler)),
        serverUrlProvider.overrideWith(FixedServerUrl.new),
      ],
    );
    addTearDown(container.dispose);
    return (container: container, store: store);
  }

  http.Response json(Object body, [int status = 200]) => http.Response(
    jsonEncode(body),
    status,
    headers: {'content-type': 'application/json'},
  );

  group('load', () {
    test('a 200 with no token held means the server does not enforce auth', () async {
      final h = await build(handler: (_) async => json(owner));
      await h.container.read(authControllerProvider.notifier).load();

      final auth = h.container.read(authControllerProvider);
      expect(auth.isSignedIn, isTrue);
      expect(auth.isOwner, isTrue);
      // The whole test for auth-off: signed in, holding nothing.
      expect(h.container.read(enforcedProvider), isFalse);
    });

    test('a token that resolves means enforcement is on', () async {
      final h = await build(
        handler: (_) async => json(owner),
        initialToken: 'lyc_live',
      );
      await h.container.read(authControllerProvider.notifier).load();

      expect(h.container.read(authControllerProvider).isSignedIn, isTrue);
      expect(h.container.read(enforcedProvider), isTrue);
    });

    test('a 401 at boot is the front door, not a session that ended', () async {
      final h = await build(handler: (_) async => http.Response('nope', 401));
      await h.container.read(authControllerProvider.notifier).load();

      final auth = h.container.read(authControllerProvider);
      expect(auth.status, AuthStatus.signedOut);
      // Crucially: no sheet. Nobody was signed out — they were never signed in.
      expect(auth.endedReason, isNull);
    });
  });

  group('signIn', () {
    test('stores the session and adopts nothing when the name is real', () async {
      final h = await build(
        handler: (req) async => req.url.path == '/auth/session'
            ? json({
                'user': {...owner, 'display_name': 'Ada'},
                'session_token': 'lyc_new',
              })
            : http.Response('unexpected ${req.url.path}', 500),
      );

      await h.container
          .read(authControllerProvider.notifier)
          .signIn('  lyc_invite  ', deviceLabel: 'Pixel 8');

      expect(h.store.token, 'lyc_new');
      expect(h.container.read(sessionTokenProvider), 'lyc_new');
      expect(h.container.read(authControllerProvider).displayName, 'Ada');
    });

    test('a rejected invite throws instead of raising the signed-out sheet', () async {
      final h = await build(
        handler: (_) async => http.Response('invalid or already-used invite token', 401),
      );

      await expectLater(
        h.container.read(authControllerProvider.notifier).signIn('lyc_bad'),
        throwsA(isA<ApiException>().having((e) => e.isUnauthorized, 'is 401', isTrue)),
      );

      final auth = h.container.read(authControllerProvider);
      expect(auth.endedReason, isNull, reason: 'a bad invite is not a session ending');
      expect(h.store.token, isEmpty);
    });
  });

  group('adoptLegacyName', () {
    /// The pre-accounts local label (LYCM-700) that must not simply evaporate.
    const legacy = {'lyceum.profile_name': 'Ada'};

    test('fills a placeholder name and consumes the local key', () async {
      var patched = false;
      final h = await build(
        prefs: legacy,
        handler: (req) async {
          if (req.method == 'PATCH') {
            patched = true;
            final body = jsonDecode(req.body) as Map<String, dynamic>;
            expect(body['display_name'], 'Ada');
            return json({...owner, 'display_name': 'Ada'});
          }
          return json({...owner, 'display_name': 'Reader'});
        },
      );

      await h.container.read(authControllerProvider.notifier).load();

      expect(patched, isTrue);
      expect(h.container.read(authControllerProvider).displayName, 'Ada');
      // Consumed, not merely read: otherwise a rename made later on another
      // phone would be silently reverted by this stale local label on every boot.
      expect(
        h.container.read(prefsProvider).getString('lyceum.profile_name'),
        isNull,
      );
    });

    test('treats the email the server defaults to as a placeholder', () async {
      var patched = false;
      final h = await build(
        prefs: legacy,
        handler: (req) async {
          if (req.method == 'PATCH') {
            patched = true;
            return json({...owner, 'display_name': 'Ada'});
          }
          // CreateUser defaults display_name to the email when given none.
          return json({...owner, 'display_name': 'you@home.lan'});
        },
      );

      await h.container.read(authControllerProvider.notifier).load();
      expect(patched, isTrue);
    });

    test('never overwrites a name deliberately set on the server', () async {
      var patched = false;
      final h = await build(
        prefs: legacy,
        handler: (req) async {
          if (req.method == 'PATCH') patched = true;
          return json({...owner, 'display_name': 'Ada Lovelace'});
        },
      );

      await h.container.read(authControllerProvider.notifier).load();

      expect(patched, isFalse);
      expect(h.container.read(authControllerProvider).displayName, 'Ada Lovelace');
      // Still consumed — the fold-in happens once either way, or it would keep
      // trying on every boot forever.
      expect(h.container.read(prefsProvider).getString('lyceum.profile_name'), isNull);
    });

    test('a failed rename never blocks the sign-in', () async {
      final h = await build(
        prefs: legacy,
        handler: (req) async => req.method == 'PATCH'
            ? http.Response('boom', 500)
            : json({...owner, 'display_name': 'Reader'}),
      );

      await expectLater(
        h.container.read(authControllerProvider.notifier).load(),
        completes,
      );
      expect(h.container.read(authControllerProvider).isSignedIn, isTrue);
    });
  });

  group('signOut', () {
    test('drops the local token', () async {
      final h = await build(
        handler: (req) async => req.method == 'DELETE'
            ? http.Response('', 204)
            : json(owner),
        initialToken: 'lyc_live',
      );

      await h.container.read(authControllerProvider.notifier).load();
      await h.container.read(authControllerProvider.notifier).signOut();

      expect(h.store.token, isEmpty);
      expect(h.container.read(authControllerProvider).status, AuthStatus.signedOut);
    });

    test('drops it even when the server never hears about it', () async {
      // Leaving behind a token that still opens the library is the worst possible
      // outcome — far worse than an un-revoked row on a server we can't reach.
      final h = await build(
        handler: (req) async => req.method == 'DELETE'
            ? http.Response('gateway is down', 502)
            : json(owner),
        initialToken: 'lyc_live',
      );

      await h.container.read(authControllerProvider.notifier).load();
      await expectLater(
        h.container.read(authControllerProvider.notifier).signOut(),
        throwsA(isA<ApiException>()),
      );

      expect(h.store.token, isEmpty, reason: 'the credential must be forgotten regardless');
      expect(h.container.read(authControllerProvider).status, AuthStatus.signedOut);
    });

    test('a 401 on the way out is not a session "ending"', () async {
      // The session was already gone — which is exactly where we were headed.
      // Firing the "you've been signed out" sheet at someone who *just tapped
      // Sign out* would be a jump scare, not information.
      final h = await build(
        handler: (req) async =>
            req.method == 'DELETE' ? http.Response('nope', 401) : json(owner),
        initialToken: 'lyc_stale',
      );

      await h.container.read(authControllerProvider.notifier).load();
      await expectLater(
        h.container.read(authControllerProvider.notifier).signOut(),
        throwsA(isA<ApiException>()),
      );

      final auth = h.container.read(authControllerProvider);
      expect(auth.status, AuthStatus.signedOut);
      expect(auth.endedReason, isNull, reason: 'no alarm sheet — they asked for this');
      expect(h.store.token, isEmpty);
    });
  });

  group('sessionEnded', () {
    test('a burst of 401s raises the sheet exactly once', () async {
      // One shelf render fires a request per cover. Every one of them 401s.
      final h = await build(
        handler: (_) async => http.Response('nope', 401),
        initialToken: 'lyc_stale',
      );
      final notifier = h.container.read(authControllerProvider.notifier);

      await notifier.load(); // resolves to signedOut
      final reasons = <SessionEndReason?>[];
      h.container.listen(
        authControllerProvider,
        (_, next) => reasons.add(next.endedReason),
      );

      await notifier.sessionEnded(SessionEndReason.expired);
      await notifier.sessionEnded(SessionEndReason.expired);

      expect(reasons, isEmpty, reason: 'already signed out — nothing to end');
    });

    test('clears the credential and records why', () async {
      final h = await build(handler: (_) async => json(owner), initialToken: 'lyc_live');
      final notifier = h.container.read(authControllerProvider.notifier);
      await notifier.load();

      await notifier.sessionEnded(SessionEndReason.expired);

      final auth = h.container.read(authControllerProvider);
      expect(auth.status, AuthStatus.signedOut);
      expect(auth.endedReason, SessionEndReason.expired);
      expect(h.store.token, isEmpty);

      notifier.clearEnded();
      expect(h.container.read(authControllerProvider).endedReason, isNull);
    });
  });
}
