import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/api_providers.dart';
import 'package:lyceum/api/server_store.dart';
import 'package:lyceum/auth/auth_controller.dart';
import 'package:lyceum/auth/session_store.dart';
import 'package:lyceum/features/household/household_screen.dart';
import 'package:lyceum/prefs/prefs.dart';
import 'package:lyceum/theme/lyceum_colors.dart';
import 'package:lyceum/theme/lyceum_theme.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../auth/auth_controller_test_support.dart';

/// The bug (LYCM-105): the owner's own household row was the one row with no
/// action on it, so the person who owns the library was the only one who could not
/// get a key onto a second device. The mobile app had the same gap as the web UI,
/// which matters more here — a phone is usually the *second* device.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const owner = {
    'id': 1,
    'email': 'you@home.lan',
    'display_name': 'You',
    'is_owner': true,
  };
  const mara = {
    'id': 3,
    'email': 'mara@home.lan',
    'display_name': 'Mara',
    'is_owner': false,
  };

  final household = jsonEncode([
    {
      ...owner,
      'last_seen_at': '2026-07-29T09:00:00Z',
      'invite_expires_at': null,
      'session_count': 1,
    },
    {
      ...mara,
      'last_seen_at': '2026-07-28T09:00:00Z',
      'invite_expires_at': null,
      'session_count': 2,
    },
  ]);

  final deviceKey = jsonEncode({
    'user': owner,
    'invite_token': 'lyc_theOnlyCopy',
    'pairing_code': 'BK4T9Q2M',
  });

  /// Every request the screen makes, plus a record of what it asked for.
  late List<String> calls;

  Future<http.Response> serving(http.Request req) async {
    calls.add('${req.method} ${req.url.path}');
    final json = {'content-type': 'application/json'};
    return switch ('${req.method} ${req.url.path}') {
      'GET /auth/me' => http.Response(jsonEncode(owner), 200, headers: json),
      'GET /admin/users' => http.Response(household, 200, headers: json),
      'POST /auth/invite' => http.Response(deviceKey, 201, headers: json),
      _ => http.Response('not found', 404),
    };
  }

  /// Pump the household screen signed in as the owner.
  Future<void> pumpHousehold(WidgetTester tester) async {
    SharedPreferences.setMockInitialValues({});
    final sp = await SharedPreferences.getInstance();

    final container = ProviderContainer(
      overrides: [
        prefsProvider.overrideWithValue(sp),
        tokenStoreProvider.overrideWithValue(FakeTokenStore('lyc_live')),
        initialSessionTokenProvider.overrideWithValue('lyc_live'),
        httpClientProvider.overrideWithValue(MockClient(serving)),
        serverUrlProvider.overrideWith(FixedServerUrl.new),
      ],
    );
    addTearDown(container.dispose);

    // The screen reads "am I this row?" off the auth controller, so it has to have
    // resolved before the list renders — otherwise nobody is you.
    await container.read(authControllerProvider.notifier).load();
    expect(container.read(authControllerProvider).user?.id, 1);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          theme: buildLyceumTheme(LyceumPalette.dark),
          home: const HouseholdScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  setUp(() => calls = []);

  testWidgets('your own row offers a way to add a device', (tester) async {
    await pumpHousehold(tester);

    expect(find.text('Add a device'), findsOneWidget);
    // And it is still the row that cannot be deleted.
    expect(find.text("Can't be removed"), findsOneWidget);
  });

  testWidgets('a housemate is offered a re-invite, not a device key', (tester) async {
    await pumpHousehold(tester);

    expect(find.text('Re-invite'), findsOneWidget);
    expect(find.text('Remove'), findsOneWidget);
    // One "Add a device", on your row — not one per member.
    expect(find.text('Add a device'), findsOneWidget);
  });

  testWidgets('tapping it mints through the self route and reveals your key', (tester) async {
    await pumpHousehold(tester);

    await tester.tap(find.text('Add a device'));
    await tester.pumpAndSettle();

    expect(calls, contains('POST /auth/invite'));
    // Not the owner-only admin route: members need this path too.
    expect(calls.any((c) => c.contains('/admin/users/1/invite')), isFalse);

    expect(find.text('A key for your next device'), findsOneWidget);
    expect(find.text('lyc_theOnlyCopy'), findsOneWidget);
    expect(find.textContaining('Hand this key to'), findsNothing);
  });
}
