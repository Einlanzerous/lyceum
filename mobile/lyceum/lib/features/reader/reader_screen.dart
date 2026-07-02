import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:webview_flutter/webview_flutter.dart';

import '../../api/api_providers.dart';
import '../../api/server_store.dart';
import '../../prefs/reading_font.dart';
import '../../prefs/theme_controller.dart';
import '../../theme/lyceum_colors.dart';

/// Full-screen reader. Loads the backend's own epub.js reader page in a
/// WebView, so EPUB-CFI generation and `/sync` stay byte-identical with the
/// web reader (same-origin with the API). The app's theme + reading-font
/// preferences are pushed into the page's localStorage so an opened book
/// matches the native app's chosen look.
class ReaderScreen extends ConsumerStatefulWidget {
  const ReaderScreen({super.key, required this.bookId});
  final int bookId;

  @override
  ConsumerState<ReaderScreen> createState() => _ReaderScreenState();
}

class _ReaderScreenState extends ConsumerState<ReaderScreen> {
  late final WebViewController _controller;
  late final String _origin;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    final client = ref.read(lyceumClientProvider);
    _origin = ref.read(serverUrlProvider);
    final readerUrl = client.readerUrl(widget.bookId);
    final dark = ref.read(themeControllerProvider) == LyceumThemeMode.dark;
    final font = ref.read(readingFontProvider);

    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(dark ? const Color(0xFF171717) : const Color(0xFFF7F5F0))
      ..setNavigationDelegate(
        NavigationDelegate(
          // Inject the app's theme/font into the reader's localStorage as early
          // as possible (before the SPA's deferred module script runs), so the
          // book renders in the right theme WITHOUT a reload. A reload here
          // would spawn a second epub.js init whose pre-locations `relocate`
          // saves progress=0 and clobbers the real sync position. The same
          // injection also installs a hook that routes the SPA's own "Library"
          // pill back to the NATIVE library (see [_injectBridge]).
          onPageStarted: (_) => _injectBridge(dark, font),
          onPageFinished: (_) {
            _injectBridge(dark, font); // belt-and-suspenders for next open
            if (mounted) setState(() => _loading = false);
          },
          onWebResourceError: (err) {
            // Ignore sub-resource errors; only surface main-frame failures.
            if (err.isForMainFrame ?? true) {
              setState(() {
                _error = err.description;
                _loading = false;
              });
            }
          },
          onNavigationRequest: (req) {
            final url = req.url;
            final allowed = url.startsWith(_origin) ||
                url.startsWith('blob:') ||
                url.startsWith('data:') ||
                url.startsWith('about:');
            if (!allowed) return NavigationDecision.prevent;
            // A main-frame navigation away from the reader page (e.g. tapping
            // the page's own "Library" pill) returns to the NATIVE library
            // instead of loading the web shelf inside the WebView. Sub-resource
            // and iframe loads (book content, assets) are not main-frame.
            if (req.isMainFrame && url.startsWith(_origin)) {
              final path = Uri.parse(url).path;
              if (!path.startsWith('/reader')) {
                Future.microtask(() {
                  if (mounted) context.go('/');
                });
                return NavigationDecision.prevent;
              }
            }
            return NavigationDecision.navigate;
          },
        ),
      )
      // The reader's "Library" pill is a Vue client-side route change
      // (history.pushState), which does NOT fire onNavigationRequest. This
      // channel lets the injected hook tell us the SPA left /reader so we can
      // pop back to the native library instead of showing the web shelf inside
      // the WebView.
      ..addJavaScriptChannel(
        'LyceumNav',
        onMessageReceived: (_) {
          if (mounted) context.go('/');
        },
      )
      ..loadRequest(Uri.parse(readerUrl));
  }

  @override
  void dispose() {
    // The WebView/epub.js page can flip the Activity into an immersive mode
    // that hides the status bar; restore the normal system bars so the native
    // library/settings screens keep their safe-area insets.
    SystemChrome.setEnabledSystemUIMode(
      SystemUiMode.edgeToEdge,
      overlays: SystemUiOverlay.values,
    );
    super.dispose();
  }

  Future<void> _injectBridge(bool dark, ReadingFont font) async {
    final theme = dark ? 'dark' : 'light';
    try {
      await _controller.runJavaScript(
        // 1) Push the app's theme/font into the reader's localStorage.
        "try{localStorage.setItem('lyceum.theme','$theme');"
        "localStorage.setItem('lyceum.readingFont','${font.name}');}catch(e){}"
        // 2) Install a one-time hook: whenever the SPA navigates away from a
        //    /reader route (e.g. its "Library" pill), notify the native side so
        //    we pop back to the native library instead of rendering the web
        //    shelf inside the WebView.
        "(function(){if(window.__lyceumNavHook)return;window.__lyceumNavHook=true;"
        "var notify=function(){try{if(!location.pathname.startsWith('/reader')){"
        "LyceumNav.postMessage('exit');}}catch(e){}};"
        "window.addEventListener('popstate',notify);"
        "var w=function(o){return function(){var r=o.apply(this,arguments);notify();return r;};};"
        "history.pushState=w(history.pushState);"
        "history.replaceState=w(history.replaceState);})();",
      );
    } catch (_) {
      // localStorage not ready yet on this callback — the onPageFinished pass
      // (or the next open) will set it.
    }
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Scaffold(
      backgroundColor: lyc.bg,
      body: Stack(
        children: [
          if (_error == null) WebViewWidget(controller: _controller),
          if (_loading && _error == null)
            _Overlay(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  SizedBox(
                    width: 26,
                    height: 26,
                    child: CircularProgressIndicator(
                        strokeWidth: 2.5, color: lyc.brass),
                  ),
                  const SizedBox(height: 16),
                  Text('Finding your place…',
                      style: TextStyle(color: lyc.muted, fontSize: 13.5)),
                ],
              ),
            ),
          if (_error != null)
            _Overlay(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(Icons.error_outline, color: lyc.error, size: 32),
                  const SizedBox(height: 12),
                  Text("This book won't open",
                      style: Theme.of(context).textTheme.titleMedium),
                  const SizedBox(height: 6),
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 40),
                    child: Text(_error!,
                        textAlign: TextAlign.center,
                        maxLines: 3,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 12.5, color: lyc.muted)),
                  ),
                  const SizedBox(height: 18),
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      OutlinedButton(
                        onPressed: () => context.go('/'),
                        child: const Text('Library'),
                      ),
                      const SizedBox(width: 10),
                      FilledButton(
                        onPressed: () {
                          setState(() {
                            _error = null;
                            _loading = true;
                          });
                          _controller.reload();
                        },
                        child: const Text('Retry'),
                      ),
                    ],
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _Overlay extends StatelessWidget {
  const _Overlay({required this.child});
  final Widget child;
  @override
  Widget build(BuildContext context) {
    return Positioned.fill(
      child: ColoredBox(
        color: context.lyc.bg,
        child: Center(child: child),
      ),
    );
  }
}
