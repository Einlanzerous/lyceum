import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/api_providers.dart';
import 'package:lyceum/api/server_store.dart';
import 'package:lyceum/auth/session_store.dart';
import 'package:lyceum/features/library/book_card.dart';
import 'package:lyceum/features/library/library_controller.dart';
import 'package:lyceum/prefs/prefs.dart';
import 'package:lyceum/theme/lyceum_colors.dart';
import 'package:lyceum/theme/lyceum_theme.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../auth/auth_controller_test_support.dart';

/// LYCM-109: a duplicate that reaches the shelf was permanent — the app had no
/// delete at all. Long-pressing a tile now offers one, behind a confirm.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  final library = jsonEncode([
    {'id': 22, 'title': 'The Dinosaur Knights', 'author': 'Victor Milán'},
    {'id': 37, 'title': 'The Dinosaur Knights', 'author': 'Victor Milán'},
  ]);

  late List<String> calls;
  var deleteStatus = 204;

  Future<http.Response> serving(http.Request req) async {
    calls.add('${req.method} ${req.url.path}');
    return switch ('${req.method} ${req.url.path}') {
      'GET /library' => http.Response(
        library,
        200,
        headers: {'content-type': 'application/json'},
      ),
      'DELETE /books/37' => http.Response('', deleteStatus),
      _ => http.Response('not found', 404),
    };
  }

  /// A container whose shelf loads from [serving]. prefs is overridden because
  /// the client keys sync calls on a persisted device id.
  Future<ProviderContainer> shelfContainer() async {
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
    return container;
  }

  /// Pump the duplicate's tile with a loaded shelf behind it.
  Future<ProviderContainer> pumpCard(WidgetTester tester) async {
    final container = await shelfContainer();

    final books = await container.read(libraryControllerProvider.future);
    final duplicate = books.firstWhere((b) => b.id == 37);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          theme: buildLyceumTheme(LyceumPalette.dark),
          // Scaffold so the failure path has a messenger to post to.
          home: Scaffold(
            body: Center(
              child: SizedBox(width: 160, child: BookCard(book: duplicate)),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    return container;
  }

  Future<void> longPressTile(WidgetTester tester) async {
    await tester.longPress(find.byType(BookCard));
    await tester.pumpAndSettle();
  }

  setUp(() {
    calls = [];
    deleteStatus = 204;
  });

  testWidgets('long-press offers Remove, and confirming deletes the book', (
    tester,
  ) async {
    final container = await pumpCard(tester);
    await longPressTile(tester);

    expect(find.text('Remove from library'), findsOneWidget);
    await tester.tap(find.text('Remove from library'));
    await tester.pumpAndSettle();

    // It asks first, naming the book.
    expect(find.text('Remove book?'), findsOneWidget);
    expect(find.textContaining('The Dinosaur Knights'), findsWidgets);

    await tester.tap(find.widgetWithText(TextButton, 'Remove'));
    await tester.pumpAndSettle();

    expect(calls, contains('DELETE /books/37'));
    final shelf = container.read(libraryControllerProvider).value!;
    expect(shelf.map((b) => b.id), [22]);
  });

  testWidgets('cancelling the confirm leaves the book alone', (tester) async {
    final container = await pumpCard(tester);
    await longPressTile(tester);

    await tester.tap(find.text('Remove from library'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Cancel'));
    await tester.pumpAndSettle();

    expect(calls, isNot(contains('DELETE /books/37')));
    final shelf = container.read(libraryControllerProvider).value!;
    expect(shelf.map((b) => b.id), [22, 37]);
  });

  testWidgets('a refused delete restores the tile and says so', (tester) async {
    deleteStatus = 500;
    final container = await pumpCard(tester);
    await longPressTile(tester);

    await tester.tap(find.text('Remove from library'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Remove'));
    await tester.pumpAndSettle();

    expect(calls, contains('DELETE /books/37'));
    // The book is back on the shelf rather than silently vanished.
    final shelf = container.read(libraryControllerProvider).value!;
    expect(shelf.map((b) => b.id), containsAll(<int>[22, 37]));
    expect(find.byType(SnackBar), findsOneWidget);
  });

  test('the controller drops the book optimistically', () async {
    final container = await shelfContainer();
    calls = [];

    await container.read(libraryControllerProvider.future);
    await container.read(libraryControllerProvider.notifier).remove(37);

    final shelf = container.read(libraryControllerProvider).value!;
    expect(shelf.map((b) => b.id), [22]);

    // An id that is not on the shelf is a no-op, not a stray request.
    await container.read(libraryControllerProvider.notifier).remove(999);
    expect(calls, isNot(contains('DELETE /books/999')));
  });
}
