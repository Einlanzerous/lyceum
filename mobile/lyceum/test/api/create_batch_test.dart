import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/client.dart';
import 'package:lyceum/api/models.dart';

void main() {
  final captured = DateTime.utc(2026, 7, 12, 1, 30, 0);

  test('createBatch POSTs the scans and parses the batch response', () async {
    late http.Request seen;
    final mock = MockClient((req) async {
      seen = req;
      return http.Response(
        jsonEncode({
          'id': 42,
          'status': 'open',
          'counts': {'ready': 1, 'review': 1, 'no_match': 1},
          'candidates': [
            {
              'isbn': '9780140449334',
              'status': 'ready',
              'title': 'The Odyssey',
            },
            {
              'isbn': '9780765382115',
              'status': 'review',
              'title': 'The Dinosaur Lords',
            },
            {'isbn': '9781234567897', 'status': 'no_match'},
          ],
        }),
        201,
        headers: {'content-type': 'application/json'},
      );
    });

    final client = LyceumClient(
      baseUrl: 'http://lib.test',
      deviceId: 'pixel-9',
      httpClient: mock,
    );

    final batch = await client.createBatch([
      ScannedIsbn(isbn: '9780140449334', capturedAt: captured),
      ScannedIsbn(
        isbn: '9780765382115',
        capturedAt: captured,
        source: 'manual',
      ),
    ]);

    // Request shape.
    expect(seen.method, 'POST');
    expect(seen.url.toString(), 'http://lib.test/ingest/batches');
    final body = jsonDecode(seen.body) as Map<String, dynamic>;
    expect(body['source_device'], 'pixel-9'); // defaults to the device id
    final scans = body['scans'] as List<dynamic>;
    expect(scans, hasLength(2));
    expect(scans[0], {
      'isbn': '9780140449334',
      'captured_at': '2026-07-12T01:30:00.000Z',
      'source': 'camera',
    });
    expect((scans[1] as Map)['source'], 'manual');

    // Parsed response.
    expect(batch.id, 42);
    expect(batch.counts.ready, 1);
    expect(batch.counts.review, 1);
    expect(batch.counts.noMatch, 1);
    expect(batch.counts.total, 3);
    expect(batch.candidates, hasLength(3));
    expect(batch.candidates.first.title, 'The Odyssey');
    expect(batch.candidates.last.status, 'no_match');
  });

  test('createBatch surfaces a non-201 as ApiException', () async {
    final mock = MockClient((_) async => http.Response('boom', 500));
    final client = LyceumClient(
      baseUrl: 'http://lib.test',
      deviceId: 'd',
      httpClient: mock,
    );
    expect(
      () => client.createBatch([
        ScannedIsbn(isbn: '9780140449334', capturedAt: captured),
      ]),
      throwsA(isA<ApiException>()),
    );
  });
}
