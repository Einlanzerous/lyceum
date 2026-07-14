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
  bool get isUnauthorized => statusCode == 401;

  @override
  String toString() => 'ApiException($statusCode): $message';
}

/// A `/admin/*` route refused because the server runs with `LYCEUM_AUTH=false`.
///
/// This is not a permissions failure to apologise for. A server that cannot tell
/// who is asking refuses to mint credentials — otherwise anyone who could reach
/// the port could issue themselves an invite, redeem it for a durable session,
/// and still hold it after the operator turned enforcement on, walking straight
/// through the step meant to close the door. The Household screen gives it its
/// own explained state rather than an error toast.
class AdminDisabledException extends ApiException {
  AdminDisabledException(String message) : super(403, message);
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

  // --- Accounts (LYCM-804) --------------------------------------------------
  //
  // The session rides on every one of these automatically: the injected
  // http.Client is an AuthClient, which attaches the bearer header. Nothing here
  // handles a credential itself except [redeemInvite], which mints one.

  /// `POST /auth/session` — redeem a single-use invite for a durable session.
  ///
  /// All whitespace is stripped from [inviteToken], not merely trimmed: invites
  /// arrive out of chat apps and terminal logs, hard-wrapped and padded, and a
  /// key broken across two lines is still the key someone was given. (The
  /// sign-in copy promises exactly this.)
  ///
  /// A wrong, spent, or expired invite all come back as the same **401** — the
  /// server genuinely cannot tell them apart, deliberately, so that probing
  /// can't distinguish a used invite from one that never existed. Callers should
  /// run this inside `AuthClient.suppressUnauthorized`: a rejected invite is
  /// expected input at the front door, not a session that ended.
  Future<({Account user, String sessionToken})> redeemInvite(
    String inviteToken, {
    required String deviceLabel,
  }) async {
    final r = await _http
        .post(
          _uri('/auth/session'),
          headers: {'Content-Type': 'application/json'},
          body: jsonEncode({
            'token': inviteToken.replaceAll(RegExp(r'\s+'), ''),
            'device_label': deviceLabel,
          }),
        )
        .timeout(timeout);
    if (r.statusCode != 200) _throw(r);
    final json = jsonDecode(r.body) as Map<String, dynamic>;
    return (
      user: Account.fromJson(json['user'] as Map<String, dynamic>),
      sessionToken: (json['session_token'] as String?) ?? '',
    );
  }

  /// `GET /auth/me` — the signed-in account, or null on 401.
  ///
  /// A **200 with no token held is how a client detects an auth-off server**:
  /// with `LYCEUM_AUTH=false` the backend serves every caller as the owner.
  /// There is no separate endpoint that reports the mode.
  Future<Account?> fetchMe() async {
    final r = await _http.get(_uri('/auth/me')).timeout(timeout);
    if (r.statusCode == 401) return null;
    if (r.statusCode != 200) _throw(r);
    return Account.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  /// `PATCH /auth/me` — rename yourself. The name follows you across devices.
  Future<Account> updateDisplayName(String name) async {
    final r = await _http
        .patch(
          _uri('/auth/me'),
          headers: {'Content-Type': 'application/json'},
          body: jsonEncode({'display_name': name}),
        )
        .timeout(timeout);
    if (r.statusCode != 200) _throw(r);
    return Account.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  /// `DELETE /auth/session` — sign out **this device only**. 204, no body.
  Future<void> signOut() async {
    final r = await _http.delete(_uri('/auth/session')).timeout(timeout);
    if (r.statusCode != 204) _throw(r);
  }

  /// `GET /auth/sessions` — your own signed-in devices, most recently used
  /// first (the server orders them; do not re-sort).
  Future<List<DeviceSession>> listDevices() async {
    final r = await _http.get(_uri('/auth/sessions')).timeout(timeout);
    if (r.statusCode != 200) _throw(r);
    return (jsonDecode(r.body) as List<dynamic>)
        .map((e) => DeviceSession.fromJson(e as Map<String, dynamic>))
        .toList(growable: false);
  }

  /// `DELETE /auth/sessions/{id}` — cut off one of *your own* devices. Scoped
  /// server-side: someone else's session id simply 404s. 204, no body.
  Future<void> revokeDevice(int id) async {
    final r = await _http.delete(_uri('/auth/sessions/$id')).timeout(timeout);
    if (r.statusCode != 204) _throw(r);
  }

  // --- Household administration (owner only) --------------------------------

  /// `/admin/*` answers **two different 403s**, and confusing them tells a lie.
  ///
  ///   - `household administration requires LYCEUM_AUTH…` — enforcement is off.
  ///     Nothing is wrong with *you*; the server refuses to mint credentials it
  ///     cannot attribute. This gets the explained, locked panel.
  ///   - `owner only` — a signed-in member asked for the owner's screen.
  ///
  /// Showing the second one "run `export LYCEUM_AUTH=true` on the server" would
  /// send a housemate off to fix a machine that is working perfectly. (The UI
  /// never offers Household to a member, so this is a guard rail rather than a
  /// live path — but a guard rail that lies is worse than none.)
  Never _throwAdmin(http.Response r) {
    final body = r.body.trim();
    if (r.statusCode == 403 && body.contains('LYCEUM_AUTH')) {
      throw AdminDisabledException(body);
    }
    _throw(r);
  }

  /// `GET /admin/users` — the household, owner first then creation order.
  Future<List<Member>> listMembers() async {
    final r = await _http.get(_uri('/admin/users')).timeout(timeout);
    if (r.statusCode != 200) _throwAdmin(r);
    return (jsonDecode(r.body) as List<dynamic>)
        .map((e) => Member.fromJson(e as Map<String, dynamic>))
        .toList(growable: false);
  }

  /// `POST /admin/users` — add a member and mint their one-time invite (201).
  /// The plaintext token comes back **once and never again**.
  Future<Invite> inviteMember({
    required String email,
    required String displayName,
  }) async {
    final r = await _http
        .post(
          _uri('/admin/users'),
          headers: {'Content-Type': 'application/json'},
          body: jsonEncode({'email': email, 'display_name': displayName}),
        )
        .timeout(timeout);
    if (r.statusCode != 201) _throwAdmin(r);
    return Invite.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  /// `POST /admin/users/{id}/invite` — a fresh key for an existing member: a
  /// second device, or a replacement for one they lost (201).
  Future<Invite> reinviteMember(int id) async {
    final r = await _http
        .post(_uri('/admin/users/$id/invite'))
        .timeout(timeout);
    if (r.statusCode != 201) _throwAdmin(r);
    return Invite.fromJson(jsonDecode(r.body) as Map<String, dynamic>);
  }

  /// `DELETE /admin/users/{id}` — remove a member, deleting their reading
  /// positions. The shared shelf is untouched; the owner cannot be removed.
  /// 204, no body.
  Future<void> removeMember(int id) async {
    final r = await _http.delete(_uri('/admin/users/$id')).timeout(timeout);
    if (r.statusCode != 204) _throwAdmin(r);
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
