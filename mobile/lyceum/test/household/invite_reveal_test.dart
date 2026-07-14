import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/api/models.dart';
import 'package:lyceum/features/household/invite_reveal.dart';
import 'package:lyceum/theme/lyceum_colors.dart';
import 'package:lyceum/theme/lyceum_theme.dart';

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
  );

  /// Drives the reveal and reports how it closed.
  Future<InviteRevealResult?> open(WidgetTester tester) async {
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

    await tester.tap(find.text('Copy & close'));
    await tester.pumpAndSettle();

    expect(closed, isFalse, reason: 'the key is still on screen and still the only copy');
    expect(result, isNull);
    expect(find.text('lyc_theOnlyCopy'), findsOneWidget);
    expect(find.textContaining("Couldn't reach the clipboard"), findsOneWidget);
  });
}
