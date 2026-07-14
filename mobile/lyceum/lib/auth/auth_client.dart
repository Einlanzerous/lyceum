import 'package:http/http.dart' as http;

/// The one place a credential is attached to an outgoing request (LYCM-804).
///
/// The web client had to rewrite eighteen bare `fetch()` calls to get here
/// (`web/src/api/http.ts`); Flutter does not, because [LyceumClient] already
/// routes every request through a single injected [http.Client]. Swapping that
/// one object for this subclass authenticates the entire app — uploads, sync,
/// covers, batches — without touching a single call site.
class AuthClient extends http.BaseClient {
  AuthClient(this._inner, this._token, this._onUnauthorized);


  final http.Client _inner;

  /// Read live, per request — not captured at construction. Signing in must
  /// authenticate the requests already queued behind it.
  final String Function() _token;

  /// Fires when the server rejects us, with whether we were actually *holding* a
  /// credential at the time. The request still fails normally afterwards, so the
  /// caller's own error path runs and nothing else has to know about sessions.
  ///
  /// The distinction is the whole point. A rejected token means a session we had
  /// has stopped working, and the person deserves to be told. A rejection with no
  /// token means only that this server wants a sign-in and we never did one —
  /// which is not an event, it is just Tuesday on a device that has never signed
  /// in. Announcing "you've been signed out" there is a lie with an alarm on it.
  final void Function({required bool hadToken}) _onUnauthorized;

  /// A depth counter, not a flag.
  ///
  /// A rejected invite *is* a 401, and firing "you've been signed out" at
  /// someone in the act of signing *in* is absurd. Two suppressed calls can also
  /// overlap (the boot `/auth/me` and a sign-in), and with a bare boolean
  /// whichever finished first would re-arm the handler while the other was still
  /// in flight. (The web hit exactly this; same fix.)
  int _suppressDepth = 0;

  /// Run [fn] with the app-wide 401 reaction suppressed — the caller handles its
  /// own.
  Future<T> suppressUnauthorized<T>(Future<T> Function() fn) async {
    _suppressDepth++;
    try {
      return await fn();
    } finally {
      _suppressDepth--;
    }
  }

  /// The headers an `Image.network` needs to fetch a gated cover.
  ///
  /// This is where Flutter has it easier than the browser: an `<img>` tag cannot
  /// carry an `Authorization` header, which is the entire reason the web build
  /// needs a session cookie and the Wails shell needs the object-URL dance in
  /// `web/src/api/coverSrc.ts`. `Image.network` just takes headers.
  Map<String, String> get authHeaders {
    final t = _token();
    return t.isEmpty ? const {} : {'Authorization': 'Bearer $t'};
  }

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    final t = _token();
    if (t.isNotEmpty) request.headers['Authorization'] = 'Bearer $t';

    final res = await _inner.send(request);

    if (res.statusCode == 401 && _suppressDepth == 0) {
      _onUnauthorized(hadToken: t.isNotEmpty);
    }
    return res;
  }

  @override
  void close() => _inner.close();
}
