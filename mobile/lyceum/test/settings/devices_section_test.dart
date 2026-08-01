import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/api_providers.dart';
import 'package:lyceum/api/server_store.dart';
import 'package:lyceum/auth/session_store.dart';
import 'package:lyceum/features/settings/account_section.dart';
import 'package:lyceum/prefs/prefs.dart';
import 'package:lyceum/theme/lyceum_colors.dart';
import 'package:lyceum/theme/lyceum_theme.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../auth/auth_controller_test_support.dart';

/// "Add a device" (LYCM-105) lives in the devices list but does not depend on it:
/// it mints against the caller's own session. Hanging it off the list's loaded
/// state would take it away from exactly the person most likely to need it —
/// someone whose server is slow or erroring, trying to get a key onto a phone.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<void> pumpDevices(
    WidgetTester tester,
    http.Response Function() reply,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final sp = await SharedPreferences.getInstance();

    final container = ProviderContainer(
      // Riverpod 3 retries a failed provider on a backoff timer, which outlives
      // the widget tree and trips the test binding's pending-timer check. The
      // error state itself is what's under test, not how it recovers.
      retry: (_, _) => null,
      overrides: [
        prefsProvider.overrideWithValue(sp),
        tokenStoreProvider.overrideWithValue(FakeTokenStore('lyc_live')),
        initialSessionTokenProvider.overrideWithValue('lyc_live'),
        httpClientProvider.overrideWithValue(MockClient((_) async => reply())),
        serverUrlProvider.overrideWith(FixedServerUrl.new),
      ],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          theme: buildLyceumTheme(LyceumPalette.dark),
          home: const Scaffold(
            body: SingleChildScrollView(child: DevicesSection()),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  final json = {'content-type': 'application/json'};

  testWidgets('is offered alongside a normal device list', (tester) async {
    await pumpDevices(
      tester,
      () => http.Response(
        jsonEncode([
          {
            'id': 1,
            'device_label': 'Pixel 8',
            'created_at': '2026-07-01T10:00:00Z',
            'last_seen_at': '2026-07-29T09:00:00Z',
            'current': true,
          },
        ]),
        200,
        headers: json,
      ),
    );

    expect(find.text('Pixel 8'), findsOneWidget);
    expect(find.text('Add a device'), findsOneWidget);
  });

  testWidgets('survives an empty list', (tester) async {
    await pumpDevices(tester, () => http.Response('[]', 200, headers: json));

    expect(find.text('No other devices are signed in.'), findsOneWidget);
    expect(find.text('Add a device'), findsOneWidget);
  });

  testWidgets('survives a server that cannot list devices', (tester) async {
    await pumpDevices(tester, () => http.Response('boom', 500));

    // The list says what went wrong, and the way out is still on screen.
    expect(find.text('Add a device'), findsOneWidget);
    expect(find.text('Get a key'), findsOneWidget);
  });
}
