import 'package:flutter_test/flutter_test.dart';
import 'package:lyceum/features/reader/reader_bridge.dart';
import 'package:lyceum/prefs/reading_font.dart';

/// The reader is a WebView of the web SPA, so this script is the *only* thing
/// standing between a signed-in reader and a book that won't open. It runs
/// somewhere no unit test can follow it, against a live library, carrying
/// somebody's only credential — so the script itself gets pinned here.
void main() {
  String script({
    String sessionToken = 'lyc_abc123',
    String deviceId = 'd1e2v3',
    bool dark = true,
    ReadingFont font = ReadingFont.serif,
  }) => readerBootstrapScript(
    sessionToken: sessionToken,
    deviceId: deviceId,
    dark: dark,
    font: font,
  );

  test('hands the SPA the session under the key it actually reads', () {
    // web/src/api/http.ts: TOKEN_KEY = 'lyceum.session_token'. If this ever
    // drifts, the reader silently stops authenticating and every book 401s.
    expect(
      script(),
      contains('localStorage.setItem("lyceum.session_token","lyc_abc123")'),
    );
  });

  test('collapses the two device ids a phone would otherwise have', () {
    // The native id (SharedPreferences) and the one the SPA would generate for
    // itself in here. Two ids = two rows in "your devices" and reading positions
    // split across both, for one phone.
    expect(script(), contains('localStorage.setItem("lyceum.device_id","d1e2v3")'));
  });

  test('removes a stale token when this device holds none', () {
    // Auth-off server, or a server the user re-pointed the app away from. Leaving
    // an old credential in the page would keep presenting it to a library that
    // never asked for one.
    final s = script(sessionToken: '');
    expect(s, contains('localStorage.removeItem("lyceum.session_token")'));
    expect(s, isNot(contains('setItem("lyceum.session_token"')));
  });

  test('escapes the credential instead of hand-quoting it into JS', () {
    // Tokens are base64url today and could not break out — but this string is
    // built from a secret and evaluated as code, and "it can't contain a quote"
    // is not a property worth betting a library on.
    final s = script(sessionToken: 'a"b\\c\nd');
    expect(s, contains(r'"a\"b\\c\nd"'));
    expect(
      s.split('\n').length,
      1,
      reason: 'a raw newline in the token would split the statement',
    );
  });

  test('still carries the theme and font it always did', () {
    expect(script(dark: true), contains('localStorage.setItem("lyceum.theme","dark")'));
    expect(script(dark: false), contains('localStorage.setItem("lyceum.theme","light")'));
    expect(
      script(font: ReadingFont.publisher),
      contains('localStorage.setItem("lyceum.readingFont","publisher")'),
    );
  });

  test('keeps the nav hook that returns the SPA\'s "Library" pill to native', () {
    final s = script();
    expect(s, contains('__lyceumNavHook'));
    expect(s, contains('LyceumNav.postMessage("exit")'));
    expect(s, contains('history.pushState'));
  });

  test('every write is inside the try, so one failure loses none of the others', () {
    final s = script();
    final body = s.substring(s.indexOf('try{'), s.indexOf('}catch(e){}'));
    for (final key in [
      'lyceum.session_token',
      'lyceum.device_id',
      'lyceum.theme',
      'lyceum.readingFont',
    ]) {
      expect(body, contains(key), reason: '$key must be written inside the guard');
    }
  });
}
