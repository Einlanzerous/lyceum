import 'dart:convert';
import 'dart:io';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/api_providers.dart';
import 'package:lyceum/api/server_store.dart';
import 'package:lyceum/auth/onboarding.dart';
import 'package:lyceum/auth/session_store.dart';
import 'package:lyceum/prefs/prefs.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_controller_test_support.dart';

/// Scan-to-onboard: one QR has to settle both halves (LYCM-103).
///
/// Every test here is really about *order*. Pointing the app at a library and
/// signing it in are two operations that interfere — the first deliberately
/// drops the session, the second mints one — so the sequence is the behaviour,
/// and getting it backwards fails in ways a screen test would never show.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const owner = {
    'id': 1,
    'email': 'you@home.lan',
    'display_name': 'You',
    'is_owner': true,
  };

  http.Response json(Object body, [int status = 200]) => http.Response(
    jsonEncode(body),
    status,
    headers: {'content-type': 'application/json'},
  );

  /// A container on a real (prefs-backed) [ServerUrlController], so the address
  /// can actually move mid-test — the fixed one used elsewhere can't.
  Future<({ProviderContainer container, FakeTokenStore store})> build({
    required Future<http.Response> Function(http.Request) handler,
    String server = '',
    String initialToken = '',
  }) async {
    SharedPreferences.setMockInitialValues({
      if (server.isNotEmpty) 'lyceum.server_url': server,
    });
    final sp = await SharedPreferences.getInstance();
    final store = FakeTokenStore(initialToken);

    final container = ProviderContainer(
      overrides: [
        prefsProvider.overrideWithValue(sp),
        tokenStoreProvider.overrideWithValue(store),
        initialSessionTokenProvider.overrideWithValue(initialToken),
        httpClientProvider.overrideWithValue(MockClient(handler)),
      ],
    );
    addTearDown(container.dispose);
    return (container: container, store: store);
  }

  group('connect — a QR that carries an origin', () {
    test('points the app at the library, then redeems against it', () async {
      final calls = <String>[];
      late ProviderContainer container;
      String? serverWhenRedeemed;

      final h = await build(
        handler: (r) async {
          calls.add('${r.method} ${r.url}');
          switch (r.url.path) {
            case '/healthz':
              return http.Response('ok', 200);
            case '/auth/me':
              return http.Response('unauthorized', 401);
            case '/auth/session':
              // The assertion that matters: the address is already the new one
              // by the time the key is spent, so the session that comes back
              // belongs to the library that issued it.
              serverWhenRedeemed = container.read(serverUrlProvider);
              return json({'user': owner, 'session_token': 'lyc_session'});
          }
          return http.Response('not found', 404);
        },
      );
      container = h.container;

      final result = await container.read(onboarderProvider).connect((
        token: 'lyc_invite',
        origin: 'https://direct.example.test',
      ), deviceLabel: 'Pixel 8');

      expect(result, isA<Onboarded>());
      expect(container.read(serverUrlProvider), 'https://direct.example.test');
      expect(serverWhenRedeemed, 'https://direct.example.test');
      expect(container.read(sessionTokenProvider), 'lyc_session');
      expect(h.store.token, 'lyc_session');
      expect(
        calls,
        containsAllInOrder([
          'GET https://direct.example.test/healthz',
          'POST https://direct.example.test/auth/session',
        ]),
      );
    });

    test('reaches the server named in the QR, not the one it was on', () async {
      final hosts = <String>{};
      final h = await build(
        server: 'http://192.168.1.9:8080',
        handler: (r) async {
          hosts.add(r.url.host);
          if (r.url.path == '/healthz') return http.Response('ok', 200);
          if (r.url.path == '/auth/me') {
            return http.Response('unauthorized', 401);
          }
          return json({'user': owner, 'session_token': 'lyc_session'});
        },
      );

      await h.container.read(onboarderProvider).connect((
        token: 'lyc_invite',
        origin: 'https://direct.example.test',
      ));

      expect(hosts, {'direct.example.test'});
    });

    // The old library's credential is meaningless to the new one, and carrying
    // it would make a brand-new auth-off server look enforced (see
    // ServerUrlController.set). It has to be gone before the first request.
    test('does not carry the old session to the new library', () async {
      final authed = <String, bool>{};
      final h = await build(
        server: 'http://192.168.1.9:8080',
        initialToken: 'lyc_old',
        handler: (r) async {
          authed[r.url.path] = r.headers.containsKey('authorization');
          if (r.url.path == '/healthz') return http.Response('ok', 200);
          if (r.url.path == '/auth/me') {
            return http.Response('unauthorized', 401);
          }
          return json({'user': owner, 'session_token': 'lyc_new'});
        },
      );

      final result = await h.container.read(onboarderProvider).connect((
        token: 'lyc_invite',
        origin: 'https://direct.example.test',
      ));

      expect(result, isA<Onboarded>());
      expect(authed['/auth/me'], isFalse);
      expect(authed['/auth/session'], isFalse);
      expect(h.store.token, 'lyc_new');
    });

    // Committing a dead address costs the working one it replaced *and* the
    // session that went with it — so the candidate is pinged while it is still
    // only a candidate.
    test(
      'leaves a working address alone when the scanned one is dead',
      () async {
        final calls = <String>[];
        final h = await build(
          server: 'http://192.168.1.9:8080',
          initialToken: 'lyc_old',
          handler: (r) async {
            calls.add(r.url.toString());
            if (r.url.path == '/healthz') return http.Response('down', 502);
            return json({'user': owner, 'session_token': 'lyc_new'});
          },
        );

        final result = await h.container.read(onboarderProvider).connect((
          token: 'lyc_invite',
          origin: 'https://dead.example.test',
        ));

        expect(result, isA<ServerUnreachable>());
        expect(
          (result as ServerUnreachable).address,
          'https://dead.example.test',
        );
        expect(h.container.read(serverUrlProvider), 'http://192.168.1.9:8080');
        expect(h.store.token, 'lyc_old');
        expect(calls, ['https://dead.example.test/healthz']);
      },
    );

    test(
      'treats a connection that throws as unreachable, not refused',
      () async {
        final h = await build(
          handler: (r) async => throw const SocketException('no route to host'),
        );

        final result = await h.container.read(onboarderProvider).connect((
          token: 'lyc_invite',
          origin: 'https://direct.example.test',
        ));

        expect(result, isA<ServerUnreachable>());
        expect(h.container.read(serverUrlProvider), isEmpty);
      },
    );

    // Re-scanning an invite for the library you are already on must not churn
    // the address — set() would drop the session for no reason at all.
    test('does not re-point when the QR names the current library', () async {
      final calls = <String>[];
      final h = await build(
        server: 'https://direct.example.test',
        initialToken: 'lyc_old',
        handler: (r) async {
          calls.add(r.url.path);
          return json({'user': owner, 'session_token': 'lyc_new'});
        },
      );

      final result = await h.container.read(onboarderProvider).connect((
        token: 'lyc_invite',
        origin: 'https://direct.example.test',
      ));

      expect(result, isA<Onboarded>());
      expect(calls, ['/auth/session']); // no probe, no re-resolve
      expect(h.store.token, 'lyc_new');
    });
  });

  group('connect — an invite that names no library', () {
    test('asks for a server address when there is none', () async {
      final calls = <String>[];
      final h = await build(
        handler: (r) async {
          calls.add(r.url.toString());
          return http.Response('not found', 404);
        },
      );

      final result = await h.container.read(onboarderProvider).connect((
        token: 'lyc_invite',
        origin: null,
      ));

      expect(result, isA<NeedsServerAddress>());
      expect(calls, isEmpty); // nowhere to send it, so nothing was sent
    });

    test('redeems against the configured server when there is one', () async {
      final h = await build(
        server: 'http://192.168.1.9:8080',
        handler: (r) async =>
            json({'user': owner, 'session_token': 'lyc_session'}),
      );

      final result = await h.container.read(onboarderProvider).connect((
        token: 'lyc_invite',
        origin: null,
      ));

      expect(result, isA<Onboarded>());
      expect(h.store.token, 'lyc_session');
    });
  });

  group('redeem', () {
    test('a refused key is rejected, never reported as unreachable', () async {
      final h = await build(
        server: 'http://192.168.1.9:8080',
        handler: (r) async => http.Response('invalid invite', 401),
      );

      expect(
        await h.container.read(onboarderProvider).redeem('lyc_spent'),
        isA<InviteRejected>(),
      );
      expect(h.store.token, isEmpty);
    });

    test('a throttled pairing code is a caution, not a bad key', () async {
      String? body;
      final h = await build(
        server: 'http://192.168.1.9:8080',
        handler: (r) async {
          body = r.body;
          return http.Response('slow down', 429);
        },
      );

      final result = await h.container
          .read(onboarderProvider)
          .redeem('bk4t-9q2m');

      expect(result, isA<InviteThrottled>());
      // Routed by shape: a code goes as `code`, normalized, not as a token.
      expect(jsonDecode(body!), containsPair('code', 'BK4T9Q2M'));
    });

    test('a flat network is the server, not the key', () async {
      final h = await build(
        server: 'http://192.168.1.9:8080',
        handler: (r) async => throw const SocketException('connection refused'),
      );

      final result = await h.container
          .read(onboarderProvider)
          .redeem('lyc_invite');

      expect(result, isA<ServerUnreachable>());
      expect((result as ServerUnreachable).address, 'http://192.168.1.9:8080');
    });

    test('an unconfigured app has nowhere to send a pasted key', () async {
      final h = await build(handler: (r) async => http.Response('', 404));

      expect(
        await h.container.read(onboarderProvider).redeem('lyc_invite'),
        isA<NeedsServerAddress>(),
      );
    });
  });
}
