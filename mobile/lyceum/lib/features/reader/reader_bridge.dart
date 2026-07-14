import 'dart:convert';

import '../../api/device.dart';
import '../../auth/session_store.dart';
import '../../prefs/reading_font.dart';

/// The script the reader WebView is bootstrapped with (LYCM-804).
///
/// The page inside that WebView **is the web SPA**, served by the backend, and it
/// makes its own same-origin `fetch` calls — a bearer token held in Dart never
/// reaches it. But the SPA reads its session from exactly
/// `localStorage['lyceum.session_token']` (`web/src/api/http.ts`), and that is a
/// store we can write. So the reader authenticates itself with one `setItem`: no
/// cookie juggling, no second sign-in, no new backend surface.
///
/// The **device id** rides along in the same pass. Without it an Android install
/// has *two* — the native one in SharedPreferences and a separate one the SPA
/// generates for itself inside the WebView — so one phone would occupy two rows
/// in "your devices" and split its reading positions across both.
///
/// Pure and separate from the widget so the exact JS can be asserted on. It runs
/// against a live library holding somebody's only credential, in a WebView that
/// cannot be reached from a unit test — the rest of the reader gets to be a
/// widget; this gets to be a function.
String readerBootstrapScript({
  required String sessionToken,
  required String deviceId,
  required bool dark,
  required ReadingFont font,
}) {
  // With no session (an auth-off server) the key is *removed* rather than left
  // alone: a token from a previously-configured server would otherwise sit in
  // here forever, presenting a stale credential to a library that never asked
  // for one.
  //
  // jsonEncode, not interpolation. These land inside JS string literals, and a
  // credential is the last thing anyone should hand-quote.
  final session = sessionToken.isEmpty
      ? 'localStorage.removeItem(${jsonEncode(kSessionTokenKey)});'
      : 'localStorage.setItem(${jsonEncode(kSessionTokenKey)},'
            '${jsonEncode(sessionToken)});';

  return
  // 1) Session, device id, theme and font into the page's localStorage — all
  //    before the SPA's deferred module script boots and reads them.
  'try{'
      '$session'
      'localStorage.setItem(${jsonEncode(kDeviceIdKey)},${jsonEncode(deviceId)});'
      'localStorage.setItem("lyceum.theme",${jsonEncode(dark ? 'dark' : 'light')});'
      'localStorage.setItem("lyceum.readingFont",${jsonEncode(font.name)});'
      '}catch(e){}'
  // 2) A one-time hook: when the SPA navigates away from a /reader route (its
  //    own "Library" pill), tell the native side, so we pop back to the native
  //    library instead of rendering the web shelf inside the WebView.
  '(function(){if(window.__lyceumNavHook)return;window.__lyceumNavHook=true;'
      'var notify=function(){try{if(!location.pathname.startsWith("/reader")){'
      'LyceumNav.postMessage("exit");}}catch(e){}};'
      'window.addEventListener("popstate",notify);'
      'var w=function(o){return function(){var r=o.apply(this,arguments);notify();return r;};};'
      'history.pushState=w(history.pushState);'
      'history.replaceState=w(history.replaceState);})();';
}
