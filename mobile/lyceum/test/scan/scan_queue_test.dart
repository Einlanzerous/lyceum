import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/api/models.dart';
import 'package:lyceum/features/scan/scan_queue.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  final t = DateTime.utc(2026, 7, 12, 1, 0, 0);
  ScannedIsbn scan(String isbn, {String source = 'camera'}) =>
      ScannedIsbn(isbn: isbn, capturedAt: t, source: source);

  Future<ScanQueue> freshQueue([Map<String, Object> seed = const {}]) async {
    SharedPreferences.setMockInitialValues(seed);
    return ScanQueue(await SharedPreferences.getInstance());
  }

  test('starts empty', () async {
    final q = await freshQueue();
    expect(q.load(), isEmpty);
  });

  test('add appends new ISBNs in order and persists', () async {
    final q = await freshQueue();
    final r1 = await q.add(scan('9780140449334'));
    expect(r1.added, isTrue);
    final r2 = await q.add(scan('9780765382115', source: 'manual'));
    expect(r2.added, isTrue);
    expect(r2.scans.map((s) => s.isbn), ['9780140449334', '9780765382115']);
    // Persisted: a fresh handle over the same store sees them.
    final reloaded = ScanQueue(await SharedPreferences.getInstance());
    final got = reloaded.load();
    expect(got, hasLength(2));
    expect(got.last.isbn, '9780765382115');
    expect(got.last.source, 'manual');
  });

  test('add dedupes a repeated ISBN', () async {
    final q = await freshQueue();
    await q.add(scan('9780140449334'));
    final dup = await q.add(scan('9780140449334'));
    expect(dup.added, isFalse);
    expect(dup.scans, hasLength(1));
  });

  test('remove drops one ISBN', () async {
    final q = await freshQueue();
    await q.add(scan('9780140449334'));
    await q.add(scan('9780765382115'));
    final left = await q.remove('9780140449334');
    expect(left.map((s) => s.isbn), ['9780765382115']);
  });

  test('clear empties the queue', () async {
    final q = await freshQueue();
    await q.add(scan('9780140449334'));
    await q.clear();
    expect(q.load(), isEmpty);
  });

  test('loads a queue persisted by a previous session', () async {
    // First session queues one, second session (new handle) sees it.
    final first = await freshQueue();
    await first.add(scan('9780140449334'));
    final second = ScanQueue(await SharedPreferences.getInstance());
    expect(second.load().single.isbn, '9780140449334');
  });
}
