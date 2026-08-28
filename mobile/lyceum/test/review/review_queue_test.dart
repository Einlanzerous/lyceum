import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/api_providers.dart';
import 'package:lyceum/api/server_store.dart';
import 'package:lyceum/auth/session_store.dart';
import 'package:lyceum/features/library/library_controller.dart';
import 'package:lyceum/features/review/review_controller.dart';
import 'package:lyceum/features/review/review_screen.dart';
import 'package:lyceum/prefs/prefs.dart';
import 'package:lyceum/theme/lyceum_colors.dart';
import 'package:lyceum/theme/lyceum_theme.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../auth/auth_controller_test_support.dart';

/// LYCM-72: the ingest-QC queue reached the phone. Books ingest held back are
/// correctable and approvable without going to a desktop.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  final queue = jsonEncode([
    {
      'id': 12,
      'title': 'D&D - Dragonlance - Chronicles 03',
      'author': 'Weis & Hickman # 3',
      'review_state': 'pending',
      'review_flags': ['no_isbn', 'suspicious_title'],
      'cover_url': '/books/12/cover',
    },
    {
      'id': 13,
      'title': 'Piranesi',
      'author': 'Clarke, Susanna',
      'review_state': 'pending',
      'review_flags': ['possible_duplicate'],
      'duplicate_of': 4,
    },
  ]);

  final onShelf = jsonEncode({
    'id': 4,
    'title': 'Piranesi',
    'author': 'Susanna Clarke',
    'cover_url': '/books/4/cover',
  });

  late List<String> calls;
  var approveStatus = 200;
  var refetchStatus = 200;
  var matchStatus = 200;

  Future<http.Response> serving(http.Request req) async {
    calls.add('${req.method} ${req.url.path}');
    Map<String, String> json() => {'content-type': 'application/json'};
    return switch ('${req.method} ${req.url.path}') {
      'GET /ingest/review' => http.Response(queue, 200, headers: json()),
      'GET /library' => http.Response('[]', 200, headers: json()),
      'GET /books/4' =>
        matchStatus == 200
            ? http.Response(onShelf, 200, headers: json())
            : http.Response('boom', matchStatus),
      'POST /books/12/approve' =>
        approveStatus == 200
            ? http.Response(
                jsonEncode({'id': 12, 'title': 'Fixed', 'author': 'Weis'}),
                200,
                headers: json(),
              )
            : http.Response('nope', approveStatus),
      'POST /books/13/approve' => http.Response(
        jsonEncode({'id': 13, 'title': 'Piranesi', 'author': 'Clarke'}),
        200,
        headers: json(),
      ),
      'PATCH /books/12' => http.Response(
        jsonEncode({
          'id': 12,
          'title': 'Dragons of Spring Dawning',
          'author': 'Weis & Hickman',
          'review_state': 'pending',
          'review_flags': ['no_isbn'],
        }),
        200,
        headers: json(),
      ),
      'POST /books/12/cover/refetch' =>
        refetchStatus == 200
            ? http.Response(
                // A real re-fetch answers with the whole book, cover_url
                // included — the bytes changed underneath a URL that did not.
                jsonEncode({
                  'id': 12,
                  'title': 'D&D - Dragonlance - Chronicles 03',
                  'author': 'Weis & Hickman # 3',
                  'cover_url': '/books/12/cover',
                  'review_state': 'pending',
                  'review_flags': ['no_isbn', 'suspicious_title'],
                }),
                200,
                headers: json(),
              )
            : http.Response('cover fetch is not configured', refetchStatus),
      'DELETE /books/12' => http.Response('', 204),
      _ => http.Response('not found', 404),
    };
  }

  Future<ProviderContainer> container() async {
    SharedPreferences.setMockInitialValues({});
    final sp = await SharedPreferences.getInstance();
    final c = ProviderContainer(
      overrides: [
        prefsProvider.overrideWithValue(sp),
        tokenStoreProvider.overrideWithValue(FakeTokenStore('lyc_live')),
        initialSessionTokenProvider.overrideWithValue('lyc_live'),
        httpClientProvider.overrideWithValue(MockClient(serving)),
        serverUrlProvider.overrideWith(FixedServerUrl.new),
      ],
    );
    addTearDown(c.dispose);
    return c;
  }

  Future<ProviderContainer> pumpScreen(WidgetTester tester) async {
    final c = await container();
    await c.read(reviewControllerProvider.future);
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: c,
        child: MaterialApp(
          theme: buildLyceumTheme(LyceumPalette.dark),
          home: const ReviewScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();
    return c;
  }

  setUp(() {
    calls = [];
    approveStatus = 200;
    refetchStatus = 200;
    matchStatus = 200;
  });

  testWidgets('lists held books with their flags as readable labels', (
    tester,
  ) async {
    await pumpScreen(tester);

    // The codes are stable strings; the screen is what turns them into English.
    expect(find.text('No ISBN'), findsOneWidget);
    expect(find.text('Odd title'), findsOneWidget);
    expect(find.text('Possible duplicate'), findsOneWidget);
    expect(find.text('no_isbn'), findsNothing);
  });

  testWidgets('mangled title and author are editable in place', (tester) async {
    final c = await pumpScreen(tester);

    final title = find.widgetWithText(
      TextField,
      'D&D - Dragonlance - Chronicles 03',
    );
    expect(title, findsOneWidget);
    await tester.enterText(title, 'Dragons of Spring Dawning');
    await tester.tap(find.text('Save details').first);
    await tester.pumpAndSettle();

    expect(calls, contains('PATCH /books/12'));
    // The row stays in the queue: an edit is not an approval.
    final held = c.read(reviewControllerProvider).value!;
    expect(held.map((b) => b.id), [12, 13]);
    expect(held.first.title, 'Dragons of Spring Dawning');
  });

  testWidgets('approving publishes the book and drops it from the queue', (
    tester,
  ) async {
    final c = await pumpScreen(tester);
    // Load the shelf first, so a later fetch can only mean it was invalidated —
    // an unloaded provider would fetch on first read either way.
    await c.read(libraryControllerProvider.future);
    calls.clear();

    await tester.tap(find.text('Approve'));
    await tester.pumpAndSettle();

    expect(calls, contains('POST /books/12/approve'));
    expect(c.read(reviewControllerProvider).value!.map((b) => b.id), [13]);

    // The shelf is invalidated, so the next read of it is fresh rather than the
    // grid the library was showing before this book landed on it. Invalidation
    // does not fetch on its own — nothing is listening here — which is why this
    // reads the provider rather than asserting on the call log directly.
    await c.read(libraryControllerProvider.future);
    expect(calls, contains('GET /library'));
  });

  testWidgets('a refused approve puts the book back in the queue', (
    tester,
  ) async {
    approveStatus = 500;
    final c = await pumpScreen(tester);

    await tester.tap(find.text('Approve'));
    await tester.pumpAndSettle();

    // Dropping it optimistically and then failing would strand it pending with
    // nothing left pointing at it.
    expect(c.read(reviewControllerProvider).value!.map((b) => b.id), [12, 13]);
  });

  testWidgets('delete asks first, naming the book', (tester) async {
    final c = await pumpScreen(tester);

    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();
    expect(find.text('Delete this book?'), findsOneWidget);
    expect(find.textContaining('Dragonlance'), findsWidgets);

    await tester.tap(find.widgetWithText(TextButton, 'Cancel'));
    await tester.pumpAndSettle();
    expect(calls, isNot(contains('DELETE /books/12')));

    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Delete').last);
    await tester.pumpAndSettle();

    expect(calls, contains('DELETE /books/12'));
    expect(c.read(reviewControllerProvider).value!.map((b) => b.id), [13]);
  });

  testWidgets('a server with no art source says so rather than "try again"', (
    tester,
  ) async {
    refetchStatus = 503;
    await pumpScreen(tester);

    await tester.tap(find.text('Re-fetch cover').first);
    await tester.pumpAndSettle();

    expect(
      find.text('This server has no cover art source configured.'),
      findsOneWidget,
    );
  });

  testWidgets('a suspected duplicate shows the book it matched', (
    tester,
  ) async {
    await pumpScreen(tester);

    expect(calls, contains('GET /books/4'));
    expect(
      find.textContaining('another copy of a book you already have'),
      findsOneWidget,
    );
    expect(find.text('ALREADY ON THE SHELF'), findsOneWidget);
    // Both spellings of the author render — that is the difference being judged.
    expect(find.text('Susanna Clarke'), findsOneWidget);
    // And the actions are framed as a choice about the pair.
    expect(find.text('Keep both'), findsOneWidget);
    expect(find.text('Delete this copy'), findsOneWidget);
  });

  testWidgets('the surviving card keeps its own book after one is approved', (
    tester,
  ) async {
    await pumpScreen(tester);

    // Unkeyed rows reconcile by position, so the card that moves up inherits the
    // approved card's State — and its edit fields, which initialize once and
    // never re-sync. Saving that card would then write the approved book's title
    // onto a different book, with nothing on screen to suggest it.
    await tester.tap(find.text('Approve'));
    await tester.pumpAndSettle();

    final fields = tester
        .widgetList<TextField>(find.byType(TextField))
        .map((f) => f.controller!.text)
        .toList();
    // Title, author, then the (empty) series and number fields (LYCM-129).
    expect(fields, ['Piranesi', 'Clarke, Susanna', '', '']);

    // And saving sends the surviving book's own values.
    await tester.tap(find.text('Save details'));
    await tester.pumpAndSettle();
    expect(calls, isNot(contains('PATCH /books/12')));
  });

  testWidgets('a re-fetched cover is actually re-requested', (tester) async {
    await pumpScreen(tester);

    String coverUrl() => tester
        .widgetList<Image>(find.byType(Image))
        .map((i) => (i.image as NetworkImage).url)
        .firstWhere((u) => u.contains('/books/12/cover'));

    final before = coverUrl();
    await tester.tap(find.text('Re-fetch cover').first);
    await tester.pumpAndSettle();

    // Flutter's image cache is keyed on the URL, so an unchanged URL means the
    // old bytes stay on screen and the button looks like it did nothing — which
    // is the entire point of it for a book held on low_quality_cover.
    expect(coverUrl(), isNot(before));
  });

  testWidgets('a failed lookup of the other copy does not advise approving', (
    tester,
  ) async {
    matchStatus = 500;
    await pumpScreen(tester);

    expect(find.textContaining('has since been deleted'), findsNothing);
    expect(find.textContaining("Couldn't load the other copy"), findsOneWidget);
  });

  test('the pending count backs the library badge', () async {
    final c = await container();
    // Zero before the queue has loaded: the badge invites you to go and look,
    // and a count guessed from a pending load would send you to an empty screen.
    expect(c.read(pendingReviewCountProvider), 0);

    await c.read(reviewControllerProvider.future);
    expect(c.read(pendingReviewCountProvider), 2);

    await c.read(reviewControllerProvider.notifier).approve(12);
    expect(c.read(pendingReviewCountProvider), 1);
  });

  test(
    'an id that is not in the queue is a no-op, not a stray request',
    () async {
      final c = await container();
      await c.read(reviewControllerProvider.future);
      calls = [];

      await c.read(reviewControllerProvider.notifier).approve(999);
      expect(calls, isEmpty);
    },
  );

  test(
    'the library is left alone until something actually leaves the queue',
    () async {
      final c = await container();
      await c.read(reviewControllerProvider.future);
      // Loading the queue must not drag the shelf in behind it.
      expect(calls, isNot(contains('GET /library')));
    },
  );
}
