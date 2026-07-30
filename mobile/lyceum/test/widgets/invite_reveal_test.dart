import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/api/models.dart';
import 'package:lyceum/widgets/invite_reveal.dart';
import 'package:lyceum/theme/lyceum_colors.dart';
import 'package:lyceum/theme/lyceum_theme.dart';
import 'package:qr_flutter/qr_flutter.dart';

/// The invite key is plaintext exactly once — the server keeps only a hash. So
/// the single question this sheet has to answer correctly, on *every* exit, is
/// "did the key get out?"
///
/// Answer it wrong in one direction and someone loses a credential silently.
/// Answer it wrong in the other and you tell a person who has already sent the
/// key to their housemate that it's gone — they believe you, issue another, and
/// the fresh mint invalidates the key the housemate is holding.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const invite = Invite(
    user: Account(
      id: 2,
      email: 'theo@home.lan',
      displayName: 'Theo',
      isOwner: false,
    ),
    token: 'lyc_theOnlyCopy',
    pairingCode: 'BK4T9Q2M',
  );

  /// Drives the reveal and reports how it closed.
  Future<InviteRevealResult?> open(WidgetTester tester, {String? signInUrl}) async {
    InviteRevealResult? result;
    await tester.pumpWidget(
      MaterialApp(
        theme: buildLyceumTheme(LyceumPalette.dark),
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () async => result = await showInviteReveal(
                  context,
                  invite,
                  signInUrl: signInUrl,
                ),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    return result;
  }

  setUp(() {
    // A working clipboard.
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.setData') return null;
          return null;
        });
  });

  testWidgets('the key is shown, and honestly labelled as once-only', (tester) async {
    await open(tester);
    expect(find.text('lyc_theOnlyCopy'), findsOneWidget);
    expect(find.text('A key for Theo'), findsOneWidget);
    expect(
      find.textContaining("This is the only time you'll see this key."),
      findsOneWidget,
    );
  });

  testWidgets('shows the pairing code, grouped for reading', (tester) async {
    await open(tester);
    // Grouped XXXX-XXXX, not the raw eight characters.
    expect(find.text('BK4T-9Q2M'), findsOneWidget);
    expect(find.textContaining('SHORT CODE'), findsOneWidget);
  });

  testWidgets('renders the key as a QR when given a sign-in URL', (tester) async {
    await open(tester, signInUrl: 'http://192.168.1.9:8080/sign-in?token=lyc_theOnlyCopy');
    expect(find.byType(QrImageView), findsOneWidget);
    expect(find.textContaining('scan this with their camera'), findsOneWidget);
  });

  testWidgets('omits the QR when no sign-in URL is available', (tester) async {
    await open(tester);
    expect(find.byType(QrImageView), findsNothing);
  });

  testWidgets('closing with ✕ WITHOUT copying is a dismissal', (tester) async {
    InviteRevealResult? result;
    await tester.pumpWidget(
      MaterialApp(
        theme: buildLyceumTheme(LyceumPalette.dark),
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () async =>
                    result = await showInviteReveal(context, invite),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.close_rounded));
    await tester.pumpAndSettle();

    expect(
      result,
      InviteRevealResult.dismissed,
      reason: 'they never took the key — offer them the recovery path',
    );
  });

  /// The bug this pins: they tapped "Copy key", switched to a chat app, sent the
  /// key to Theo, came back and tidied up with the ✕. Nothing was lost — and they
  /// must not be told otherwise, because the "recovery" they would then be
  /// offered mints a fresh key and invalidates the one Theo is already holding.
  testWidgets('copy, then ✕, reports saved', (tester) async {
    InviteRevealResult? result;
    await tester.pumpWidget(
      MaterialApp(
        theme: buildLyceumTheme(LyceumPalette.dark),
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () async =>
                    result = await showInviteReveal(context, invite),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Copy key'));
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(Icons.close_rounded));
    await tester.pumpAndSettle();

    expect(
      result,
      InviteRevealResult.saved,
      reason: 'they copied it — the ✕ was just tidying up',
    );
  });

  testWidgets("'I've saved it' is taken at its word", (tester) async {
    InviteRevealResult? result;
    await tester.pumpWidget(
      MaterialApp(
        theme: buildLyceumTheme(LyceumPalette.dark),
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () async =>
                    result = await showInviteReveal(context, invite),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    // The sheet scrolls (key + code + QR); bring the button into view first.
    await tester.ensureVisible(find.text("I've saved it"));
    await tester.tap(find.text("I've saved it"));
    await tester.pumpAndSettle();

    expect(result, InviteRevealResult.saved);
  });

  testWidgets('a blocked clipboard never closes the sheet', (tester) async {
    // "Copy & close" promises two things. When the first one fails, doing the
    // second would destroy the only copy of a key that cannot be shown again.
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.setData') {
            throw PlatformException(code: 'unavailable');
          }
          return null;
        });

    InviteRevealResult? result;
    var closed = false;
    await tester.pumpWidget(
      MaterialApp(
        theme: buildLyceumTheme(LyceumPalette.dark),
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () async {
                  result = await showInviteReveal(context, invite);
                  closed = true;
                },
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    await tester.ensureVisible(find.text('Copy & close'));
    await tester.tap(find.text('Copy & close'));
    await tester.pumpAndSettle();

    expect(closed, isFalse, reason: 'the key is still on screen and still the only copy');
    expect(result, isNull);
    expect(find.text('lyc_theOnlyCopy'), findsOneWidget);
    expect(find.textContaining("Couldn't reach the clipboard"), findsOneWidget);
  });

  /// A key for your own next device is the same secret with a different audience
  /// (LYCM-105). Every instruction in the sheet is addressed to somebody, and read
  /// back at the person who just minted their own key, "hand this to Theo" — where
  /// Theo *is* the reader — is gibberish that makes them look for a Theo.
  group('your own device key', () {
    Future<void> openSelf(WidgetTester tester, {String? signInUrl}) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: buildLyceumTheme(LyceumPalette.dark),
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: ElevatedButton(
                  onPressed: () async => showInviteReveal(
                    context,
                    invite,
                    signInUrl: signInUrl,
                    self: true,
                  ),
                  child: const Text('open'),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
    }

    testWidgets('is addressed to the device, not to a housemate', (tester) async {
      await openSelf(tester);

      expect(find.text('A key for your next device'), findsOneWidget);
      expect(find.textContaining('DEVICE KEY'), findsWidgets);
      expect(find.textContaining('Hand this key to'), findsNothing);
      expect(find.text('A key for Theo'), findsNothing);
    });

    testWidgets('still says the key is shown only once', (tester) async {
      await openSelf(tester);

      // The warning is the point of the sheet and must survive the rewording.
      expect(
        find.textContaining("This is the only time you'll see this key."),
        findsOneWidget,
      );
      expect(find.textContaining('Just issue yourself another.'), findsOneWidget);
      expect(find.text('lyc_theOnlyCopy'), findsOneWidget);
    });

    testWidgets('points the QR at the device being added', (tester) async {
      await openSelf(
        tester,
        signInUrl: 'http://192.168.1.9:8080/sign-in?token=lyc_theOnlyCopy',
      );

      expect(find.byType(QrImageView), findsOneWidget);
      expect(find.textContaining('scan this with the other device'), findsOneWidget);
      expect(find.textContaining('their camera'), findsNothing);
    });
  });

  /// The recovery path, when a reveal was closed without the key getting out.
  group('the lost sheet', () {
    Future<bool?> openLost(WidgetTester tester, {required bool self}) async {
      bool? reissue;
      await tester.pumpWidget(
        MaterialApp(
          theme: buildLyceumTheme(LyceumPalette.dark),
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: ElevatedButton(
                  onPressed: () async =>
                      reissue = await showInviteLostSheet(context, 'Theo', self: self),
                  child: const Text('open'),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
      return reissue;
    }

    testWidgets("offers to re-issue a housemate's invite by name", (tester) async {
      await openLost(tester, self: false);

      expect(find.text('That invite is gone'), findsOneWidget);
      expect(find.text('Issue another invite for Theo'), findsOneWidget);
    });

    // Reassurance matters more here: losing your *own* key on the device you are
    // holding invites the fear that you have locked yourself out of it.
    testWidgets('offers to re-issue your own key, and says nothing broke', (tester) async {
      await openLost(tester, self: true);

      expect(find.text('That key is gone'), findsOneWidget);
      expect(find.text('Issue myself another key'), findsOneWidget);
      expect(
        find.textContaining('every device already signed in are untouched'),
        findsOneWidget,
      );
      expect(find.textContaining('Issue another invite for Theo'), findsNothing);
    });

    testWidgets('takes "Not now" as a no', (tester) async {
      await openLost(tester, self: true);

      await tester.tap(find.text('Not now'));
      await tester.pumpAndSettle();

      expect(find.text('That key is gone'), findsNothing);
    });
  });
}
