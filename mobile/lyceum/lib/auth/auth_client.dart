import 'package:http/http.dart' as http;

/// Why a request came back 401.
///
/// [removed] is not something the server tells us — a removed account and an
/// expired session both simply stop resolving. It is inferred from whether we
/// were holding a token at all, and the copy for each is written to be true
/// either way. (Mirrors `SessionEndReason` in `web/src/api/http.ts`.)
enum SessionEndReason {
  /// We held a token and the server rejected it.
  expired,

  /// We held no token and were rejected anyway — which means the server
  /// enforces auth and this device's account is gone (or enforcement was just
  /// switched on under an auth-off client).
  removed,
}

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

  /// Fires when the server rejects us. The app surfaces the "you've been signed
  /// out" sheet; the request still fails normally so the caller's own error path
  /// runs and nothing else has to know about sessions.
  final void Function(SessionEndReason) _onUnauthorized;

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
      _onUnauthorized(
        t.isNotEmpty ? SessionEndReason.expired : SessionEndReason.removed,
      );
    }
    return res;
  }

  @override
  void close() => _inner.close();
}
