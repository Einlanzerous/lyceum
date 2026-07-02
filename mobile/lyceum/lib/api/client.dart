import 'dart:convert';

import 'package:http/http.dart' as http;

import 'models.dart';

/// Raised for non-success responses. The Lyceum backend returns **plain-text**
/// error bodies (Go `http.Error`), so [message] is the trimmed response text.
class ApiException implements Exception {
  ApiException(this.statusCode, this.message);
  final int statusCode;
  final String message;

  bool get isDuplicate => statusCode == 409;
  bool get isNotFound => statusCode == 404;

  @override
  String toString() => 'ApiException($statusCode): $message';
}

/// Typed client over the Lyceum HTTP API. Mirrors `web/src/api/client.ts`.
/// Every path is prefixed with [baseUrl]; core reader routes need no auth.
class LyceumClient {
  LyceumClient({
    required this.baseUrl,
    required this.deviceId,
    http.Client? httpClient,
  }) : _http = httpClient ?? http.Client();

  final String baseUrl;
  final String deviceId;
  final http.Client _http;

  Uri _uri(String path, [Map<String, String>? query]) =>
      Uri.parse('$baseUrl$path').replace(queryParameters: query);

  // --- Absolute URLs for image / webview / file consumers -------------------
  String coverUrl(int id) => '$baseUrl/books/$id/cover';
  String bookFileUrl(int id) => '$baseUrl/books/$id/file';

  /// The backend-served epub.js reader page for a book. Loaded in the WebView
  /// reader; same-origin with the API, so sync just works.
  String readerUrl(int id) => '$baseUrl/reader/$id';

  Never _throw(http.Response r) {
    final body = r.body.trim();
    throw ApiException(r.statusCode, body.isEmpty ? 'HTTP ${r.statusCode}' : body);
  }

  /// `GET /healthz` — used by the connection tester. Returns true on 200.
  Future<bool> ping() async {
    final r = await _http.get(_uri('/healthz'));
    return r.statusCode == 200;
  }

  /// `GET /library` — the digital shelf.
  Future<List<Book>> listLibrary() async {
    final r = await _http.get(_uri('/library'));
    if (r.statusCode != 200) _throw(r);
    final data = jsonDecode(r.body) as List<dynamic>;
    return data
        .map((e) => Book.fromJson(e as Map<String, dynamic>))
        .toList(growable: false);
  }

  /// `POST /upload` — multipart, field name `file`. A 409 means the book is
  /// already on the shelf (surfaced via [ApiException.isDuplicate]).
  Future<Book> uploadBook({
    required String filename,
    String? path,
    List<int>? bytes,
  }) async {
    final req = http.MultipartRequest('POST', _uri('/upload'));
    if (bytes != null) {
      req.files.add(http.MultipartFile.fromBytes('file', bytes, filename: filename));
    } else if (path != null) {
      req.files.add(await http.MultipartFile.fromPath('file', path, filename: filename));
    } else {
      throw ArgumentError('uploadBook needs either bytes or a path');
    }
    final streamed = await _http.send(req);
    final r = await http.Response.fromStream(streamed);
    if (r.statusCode != 201) _throw(r);
    return Book.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  /// `GET /sync?book_id=&device_id=` — the saved position for this device,
  /// falling back to the latest across devices. 404 → null (fresh book).
  Future<Position?> getPosition(int bookId) async {
    final r = await _http.get(_uri('/sync', {
      'book_id': '$bookId',
      'device_id': deviceId,
    }));
    if (r.statusCode == 404) return null;
    if (r.statusCode != 200) _throw(r);
    return Position.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  /// `PUT /sync` — upsert a position (last-write-wins by `updated_at`).
  Future<Position> putPosition(Position pos) async {
    final r = await _http.put(
      _uri('/sync'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode(pos.toJson()),
    );
    if (r.statusCode != 200) _throw(r);
    return Position.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  void dispose() => _http.close();
}
