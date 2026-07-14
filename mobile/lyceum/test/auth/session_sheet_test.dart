import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/api_providers.dart';
import 'package:lyceum/api/server_store.dart';
import 'package:lyceum/app.dart';
import 'package:lyceum/auth/auth_controller.dart';
import 'package:lyceum/auth/session_store.dart';
import 'package:lyceum/prefs/prefs.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_controller_test_support.dart';

/// The signed-out sheet has to raise itself from wherever the 401 landed —
/// including mid-chapter, from a screen that is not the one listening. It is
/// mounted *above* the router, which is precisely where a naive
/// `showModalBottomSheet` finds no Navigator in scope and throws. So: drive a
/// real session ending through a real app and watch what happens.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  final owner = jsonEncode({
    'id': 1,
    'email': 'you@home.lan',
    'display_name': 'You',
    'is_owner': true,
  });

  /// A server that signs us in and hands back an empty shelf, so the app settles
  /// signed-in and the only session ending is the one the test causes.
  Future<http.Response> serving(http.Request req) async => switch (req.url.path) {
    '/auth/me' => http.Response(owner, 200, headers: {'content-type': 'application/json'}),
    '/library' => http.Response('[]', 200, headers: {'content-type': 'application/json'}),
    _ => http.Response('not found', 404),
  };

  Future<ProviderContainer> pumpApp(WidgetTester tester) async {
    SharedPreferences.setMockInitialValues({});
    final sp = await SharedPreferences.getInstance();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          prefsProvider.overrideWithValue(sp),
          tokenStoreProvider.overrideWithValue(FakeTokenStore('lyc_live')),
          initialSessionTokenProvider.overrideWithValue('lyc_live'),
          httpClientProvider.overrideWithValue(MockClient(serving)),
          serverUrlProvider.overrideWith(FixedServerUrl.new),
        ],
        child: const LyceumApp(),
      ),
    );
    await tester.pumpAndSettle();

    final container = ProviderScope.containerOf(
      tester.element(find.byType(MaterialApp)),
    );
    expect(
      container.read(authControllerProvider).isSignedIn,
      isTrue,
      reason: 'the app should boot signed in',
    );
    return container;
  }

  testWidgets('a session that stops resolving raises the sheet, not an exception', (
    tester,
  ) async {
    final container = await pumpApp(tester);

    // A 401 arrives from somewhere in the app — a cover, a sync, a page turn.
    await container
        .read(authControllerProvider.notifier)
        .unauthorized(hadToken: true);
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(find.text("You've been signed out."), findsOneWidget);
    expect(find.textContaining('Your place is saved.'), findsOneWidget);
    expect(find.text('Sign in'), findsOneWidget);
  });

  testWidgets('dismissing it lands on the front door', (tester) async {
    final container = await pumpApp(tester);

    await container
        .read(authControllerProvider.notifier)
        .unauthorized(hadToken: true);
    await tester.pumpAndSettle();

    await tester.tap(find.text('Sign in'));
    await tester.pumpAndSettle();

    // The bounce happens on dismissal, not on the 401 — a 401 mid-chapter puts a
    // calm sheet over the page rather than yanking the book away.
    expect(find.text("You've been handed a key."), findsOneWidget);
    expect(container.read(authControllerProvider).sessionEnded, isFalse);
  });

  testWidgets('the front door is what a signed-out device sees', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final sp = await SharedPreferences.getInstance();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          prefsProvider.overrideWithValue(sp),
          tokenStoreProvider.overrideWithValue(FakeTokenStore()),
          initialSessionTokenProvider.overrideWithValue(''),
          // Enforcement on, no credential held.
          httpClientProvider.overrideWithValue(
            MockClient((_) async => http.Response('nope', 401)),
          ),
          serverUrlProvider.overrideWith(FixedServerUrl.new),
        ],
        child: const LyceumApp(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text("You've been handed a key."), findsOneWidget);
    // Never signed in, so nothing "ended" — no sheet, no alarm.
    expect(find.text("You've been signed out."), findsNothing);
    expect(
      find.textContaining('No invite? Ask whoever runs this library'),
      findsOneWidget,
    );
  });
}
