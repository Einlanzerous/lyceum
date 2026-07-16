/// Parsing the invite out of whatever a scan or paste hands us (LYCM-88).
///
/// The QR handoff encodes the invite as a `<origin>/sign-in?token=…` URL rather
/// than a bare token, so a phone's native camera can just open it. When *this*
/// app scans that QR it gets the whole URL, so it has to pull the token back out;
/// a pasted key arrives bare. Both normalise here, or to null when there's
/// nothing token-shaped to redeem. Mirrors the web `extractInviteToken`.
library;

/// The prefix every invite/session token carries (see store.newToken).
const _tokenPrefix = 'lyc_';

/// Pull the invite token out of a bare key or a `…/sign-in?token=…` URL.
/// Whitespace (from wrapped chat/log pastes) is stripped. Returns null when the
/// input isn't a plausible token.
String? extractInviteToken(String raw) {
  var candidate = raw.trim();
  if (candidate.isEmpty) return null;

  // A scanned QR (or forwarded link) is a URL carrying ?token=. Anything that
  // doesn't parse as an absolute URL is treated as the token itself.
  final uri = Uri.tryParse(candidate);
  if (uri != null && uri.hasScheme) {
    final fromQuery = uri.queryParameters['token'];
    if (fromQuery != null) candidate = fromQuery;
  }

  final token = candidate.replaceAll(RegExp(r'\s+'), '');
  return token.startsWith(_tokenPrefix) && token.length > _tokenPrefix.length
      ? token
      : null;
}

/// Build the QR/redemption URL that carries an invite to another device.
String inviteSignInUrl(String origin, String token) {
  final base = origin.trim().replaceAll(RegExp(r'/+$'), '');
  return '$base/sign-in?token=${Uri.encodeQueryComponent(token)}';
}
