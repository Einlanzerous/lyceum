import 'dart:async';
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
    this.timeout = const Duration(seconds: 12),
  }) : _http = httpClient ?? http.Client();

  final String baseUrl;
  final String deviceId;
  final http.Client _http;

  /// Per-request timeout. Dart's [http.Client] never times out on its own, so
  /// without this an unreachable server leaves the library stuck on its loading
  /// skeleton forever instead of surfacing the "Can't reach the library" error
  /// card. A [TimeoutException] propagates to callers, where `AsyncValue.guard`
  /// turns it into that error state.
  final Duration timeout;

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
    throw ApiException(
      r.statusCode,
      body.isEmpty ? 'HTTP ${r.statusCode}' : body,
    );
  }

  /// `GET /healthz` — used by the connection tester. Returns true on 200.
  Future<bool> ping() async {
    final r = await _http.get(_uri('/healthz')).timeout(timeout);
    return r.statusCode == 200;
  }

  /// `GET /library` — the digital shelf.
  Future<List<Book>> listLibrary() async {
    final r = await _http.get(_uri('/library')).timeout(timeout);
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
      req.files.add(
        http.MultipartFile.fromBytes('file', bytes, filename: filename),
      );
    } else if (path != null) {
      req.files.add(
        await http.MultipartFile.fromPath('file', path, filename: filename),
      );
    } else {
      throw ArgumentError('uploadBook needs either bytes or a path');
    }
    final streamed = await _http.send(req).timeout(timeout);
    final r = await http.Response.fromStream(streamed);
    if (r.statusCode != 201) _throw(r);
    return Book.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  /// `GET /sync?book_id=&device_id=` — the saved position for this device,
  /// falling back to the latest across devices. 404 → null (fresh book).
  Future<Position?> getPosition(int bookId) async {
    final r = await _http
        .get(_uri('/sync', {'book_id': '$bookId', 'device_id': deviceId}))
        .timeout(timeout);
    if (r.statusCode == 404) return null;
    if (r.statusCode != 200) _throw(r);
    return Position.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  /// `PUT /sync` — upsert a position (last-write-wins by `updated_at`).
  Future<Position> putPosition(Position pos) async {
    final r = await _http
        .put(
          _uri('/sync'),
          headers: {'Content-Type': 'application/json'},
          body: jsonEncode(pos.toJson()),
        )
        .timeout(timeout);
    if (r.statusCode != 200) _throw(r);
    return Position.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  /// `PUT /books/{id}/finished` — mark a book read (true) or unread (false).
  Future<void> setBookFinished(int id, bool finished) async {
    final r = await _http
        .put(
          _uri('/books/$id/finished'),
          headers: {'Content-Type': 'application/json'},
          body: jsonEncode({'finished': finished}),
        )
        .timeout(timeout);
    if (r.statusCode != 204) _throw(r);
  }

  /// `POST /ingest/batches` — flush a set of scanned ISBNs as one review batch
  /// (LYCM-602). The server resolves each scan to a candidate and returns the
  /// batch with per-status counts; confirming/reviewing them is a web/desktop
  /// step. [sourceDevice] defaults to this device's id. Returns the created
  /// [Batch] (201); errors surface as [ApiException].
  Future<Batch> createBatch(
    List<ScannedIsbn> scans, {
    String? sourceDevice,
  }) async {
    final r = await _http
        .post(
          _uri('/ingest/batches'),
          headers: {'Content-Type': 'application/json'},
          body: jsonEncode({
            'source_device': sourceDevice ?? deviceId,
            'scans': scans.map((s) => s.toJson()).toList(),
          }),
        )
        .timeout(timeout);
    if (r.statusCode != 201) _throw(r);
    return Batch.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  void dispose() => _http.close();
}
