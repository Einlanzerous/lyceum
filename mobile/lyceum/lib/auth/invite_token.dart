/// Parsing the invite out of whatever a scan or paste hands us (LYCM-88).
///
/// The QR handoff encodes the invite as a `<origin>/sign-in?token=…` URL rather
/// than a bare token, so a phone's native camera can just open it. When *this*
/// app scans that QR it gets the whole URL, so it has to pull the token back out;
/// a pasted key arrives bare. Both normalise here, or to null when there's
/// nothing token-shaped to redeem. Mirrors the web `extractInviteToken`.
///
/// The URL carries a second thing the app needs, and for a while threw away: the
/// **origin**. A fresh install has no server address, so the only way to reach
/// the library is for the invite to say where it is (LYCM-103). Hence
/// [extractInvite], which returns both halves; [extractInviteToken] stays for
/// the paste path, where there is no origin to find.
library;

/// The prefix every invite/session token carries (see store.newToken).
const _tokenPrefix = 'lyc_';

/// An invite as it arrived: the key to redeem, and the library it names.
///
/// [origin] is null whenever the input carried no address — a bare pasted key,
/// a pairing code, or a v1 QR minted before the link form. Those still redeem;
/// they just need a server address from somewhere else.
typedef ScannedInvite = ({String token, String? origin});

/// Pull the invite out of a bare key or a `<origin>/sign-in?token=…` URL, both
/// halves of it. Whitespace (from wrapped chat/log pastes) is stripped. Returns
/// null when the input isn't a plausible token.
ScannedInvite? extractInvite(String raw) {
  var candidate = raw.trim();
  if (candidate.isEmpty) return null;

  // A scanned QR (or forwarded link) is a URL carrying ?token=. Anything that
  // doesn't parse as an absolute URL is treated as the token itself.
  String? origin;
  final uri = Uri.tryParse(candidate);
  if (uri != null && uri.hasScheme) {
    final fromQuery = uri.queryParameters['token'];
    if (fromQuery != null) {
      candidate = fromQuery;
      origin = _serverBase(uri);
    }
  }

  final token = candidate.replaceAll(RegExp(r'\s+'), '');
  if (!token.startsWith(_tokenPrefix) || token.length == _tokenPrefix.length) {
    return null;
  }
  return (token: token, origin: origin);
}

/// Pull just the token out. The paste path's shape: a key typed or pasted into
/// the sign-in field names no server, and nothing there wants one.
String? extractInviteToken(String raw) => extractInvite(raw)?.token;

/// The server address a sign-in link was built from — the exact inverse of
/// [inviteSignInUrl], and of Go's `invite.SignInURL`.
///
/// Undoing the `/sign-in` suffix rather than discarding the path is deliberate:
/// a base may carry a path prefix (a reverse proxy mounting Lyceum under
/// `/lyceum`), and throwing the path away would hand back an address that
/// resolves to the proxy's front page instead of the API.
///
/// Null for anything this app could not talk to anyway — a non-http(s) scheme,
/// or a URL with no host — so a QR encoding something else can never overwrite
/// a working server address with one that cannot be reached.
String? _serverBase(Uri uri) {
  if (uri.scheme != 'http' && uri.scheme != 'https') return null;
  if (uri.host.isEmpty) return null;

  var path = uri.path.replaceAll(RegExp(r'/+$'), '');
  if (path.endsWith('/sign-in')) {
    path = path.substring(0, path.length - '/sign-in'.length);
  }
  // `Uri.origin` omits a default port, so http://host:80 and http://host both
  // normalise to the one address — the same shape `normalizeServerUrl` keeps.
  return '${uri.origin}$path';
}

/// Build the QR/redemption URL that carries an invite to another device.
String inviteSignInUrl(String origin, String token) {
  final base = origin.trim().replaceAll(RegExp(r'/+$'), '');
  return '$base/sign-in?token=${Uri.encodeQueryComponent(token)}';
}

// --- Pairing codes (LYCM-88) ---

/// Crockford base32 with the ambiguous glyphs removed — mirrors
/// store.pairingAlphabet.
const _pairingAlphabet = '23456789ABCDEFGHJKMNPQRSTVWXYZ';
const _pairingCodeLen = 8;

/// Fold a typed code to its canonical form: upper-cased, non-alphabet stripped.
String normalizePairingCode(String raw) {
  final b = StringBuffer();
  for (final ch in raw.toUpperCase().split('')) {
    if (_pairingAlphabet.contains(ch)) b.write(ch);
  }
  return b.toString();
}

/// Whether the field holds a pairing code rather than a token, so the one
/// sign-in input can accept either. A `lyc_` token never looks like a code.
bool looksLikePairingCode(String raw) {
  if (raw.contains('lyc_')) return false;
  return normalizePairingCode(raw).length == _pairingCodeLen;
}
