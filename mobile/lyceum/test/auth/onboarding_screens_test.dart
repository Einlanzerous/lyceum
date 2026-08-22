import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/api_providers.dart';
import 'package:lyceum/features/auth/sign_in_screen.dart';
import 'package:lyceum/features/library/library_screen.dart';
import 'package:lyceum/prefs/prefs.dart';
import 'package:lyceum/auth/session_store.dart';
import 'package:lyceum/theme/lyceum_colors.dart';
import 'package:lyceum/theme/lyceum_theme.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'auth_controller_test_support.dart';

/// What an unconfigured install is offered (LYCM-103).
///
/// The pivot is a claim about *order of offer*, not just about parsing: the QR
/// is the one path that needs nothing typed, so it has to be the thing in front
/// of you, and the address field it replaced has to still be reachable — a bare
/// v1 token, a pairing code and a LAN box all arrive without an origin.
///
/// Nothing here taps Scan: the scanner is a camera, and a widget test has none.
/// What is pinned is the shape of the door.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  /// A screen with no server address configured — a fresh install.
  Future<void> pump(WidgetTester tester, Widget screen) async {
    SharedPreferences.setMockInitialValues({});
    final sp = await SharedPreferences.getInstance();

    final container = ProviderContainer(
      overrides: [
        prefsProvider.overrideWithValue(sp),
        tokenStoreProvider.overrideWithValue(FakeTokenStore()),
        initialSessionTokenProvider.overrideWithValue(''),
        httpClientProvider.overrideWithValue(
          MockClient((_) async => http.Response('not found', 404)),
        ),
      ],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          theme: buildLyceumTheme(LyceumPalette.dark),
          home: screen,
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  group('the front door', () {
    testWidgets('leads with Scan invite when there is no server yet', (
      tester,
    ) async {
      await pump(tester, const SignInScreen());

      expect(find.text('Scan invite'), findsOneWidget);
      // The address field is the fallback, so it is not what greets you.
      expect(find.text('Server URL'), findsNothing);
      expect(find.text('Enter a server address instead'), findsOneWidget);
    });

    testWidgets('still lets you type an address', (tester) async {
      await pump(tester, const SignInScreen());

      await tester.tap(find.text('Enter a server address instead'));
      await tester.pumpAndSettle();

      expect(find.text('Server URL'), findsOneWidget);
      expect(find.text('Save'), findsOneWidget);
    });
  });

  // Where a fresh install actually lands: the router only sends people to
  // /sign-in once a server has said they are signed out, and an app with no
  // address has asked nobody.
  group('the library connect prompt', () {
    testWidgets('offers the same scan, and the same fallback', (tester) async {
      await pump(tester, const LibraryScreen());

      expect(find.text('Scan invite'), findsOneWidget);
      expect(find.text('Server URL'), findsNothing);

      await tester.tap(find.text('Enter a server address instead'));
      await tester.pumpAndSettle();

      expect(find.text('Server URL'), findsOneWidget);
    });
  });
}
