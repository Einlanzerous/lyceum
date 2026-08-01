import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:lyceum/api/client.dart';

void main() {
  test('deleteBook issues DELETE /books/{id}', () async {
    late http.Request seen;
    final mock = MockClient((req) async {
      seen = req;
      return http.Response('', 204);
    });

    final client = LyceumClient(
      baseUrl: 'http://lib.test',
      deviceId: 'pixel-9',
      httpClient: mock,
    );

    await client.deleteBook(37);

    expect(seen.method, 'DELETE');
    expect(seen.url.path, '/books/37');
  });

  test('deleteBook throws on a non-204 response', () async {
    final mock = MockClient(
      (req) async => http.Response('book not found', 404),
    );

    final client = LyceumClient(
      baseUrl: 'http://lib.test',
      deviceId: 'pixel-9',
      httpClient: mock,
    );

    await expectLater(client.deleteBook(999), throwsA(isA<Exception>()));
  });
}
