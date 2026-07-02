import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/api_providers.dart';
import '../../api/client.dart';
import '../../api/server_store.dart';
import '../../theme/lyceum_colors.dart';

/// The backend-connection editor. Mirrors `ServerSettings.vue`: a URL field
/// plus **Test** (`GET {url}/healthz`) and **Save** (normalize + persist).
/// Used both in Settings → Connection and as the Library first-run prompt.
class ServerSettings extends ConsumerStatefulWidget {
  const ServerSettings({super.key, this.onSaved});

  /// Called after a successful save (e.g. to reload the shelf).
  final VoidCallback? onSaved;

  @override
  ConsumerState<ServerSettings> createState() => _ServerSettingsState();
}

class _ServerSettingsState extends ConsumerState<ServerSettings> {
  late final TextEditingController _controller;
  String? _status;
  bool _ok = false;
  bool _testing = false;

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController(text: ref.read(serverUrlProvider));
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _test() async {
    final url = normalizeServerUrl(_controller.text);
    if (url.isEmpty) {
      setState(() {
        _ok = false;
        _status = 'Enter a server URL first.';
      });
      return;
    }
    setState(() {
      _testing = true;
      _status = null;
    });
    final client = LyceumClient(
      baseUrl: url,
      deviceId: '',
      httpClient: ref.read(httpClientProvider),
    );
    try {
      final reached = await client.ping();
      setState(() {
        _ok = reached;
        _status = reached ? 'Reached the server.' : 'Server did not respond OK.';
      });
    } catch (_) {
      setState(() {
        _ok = false;
        _status = "Couldn't reach the server. Check the address and that it's running.";
      });
    } finally {
      if (mounted) setState(() => _testing = false);
    }
  }

  Future<void> _save() async {
    final url = normalizeServerUrl(_controller.text);
    await ref.read(serverUrlProvider.notifier).set(url);
    _controller.text = url;
    widget.onSaved?.call();
    if (mounted) {
      setState(() {
        _ok = true;
        _status = 'Saved.';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Server URL',
            style: TextStyle(
                fontSize: 13, fontWeight: FontWeight.w700, color: lyc.text)),
        const SizedBox(height: 8),
        TextField(
          controller: _controller,
          keyboardType: TextInputType.url,
          autocorrect: false,
          decoration: const InputDecoration(hintText: 'http://192.168.1.10:8080'),
          onChanged: (_) => setState(() => _status = null),
        ),
        const SizedBox(height: 12),
        Row(
          children: [
            OutlinedButton(
              onPressed: _testing ? null : _test,
              child: _testing
                  ? const SizedBox(
                      width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2))
                  : const Text('Test'),
            ),
            const SizedBox(width: 10),
            FilledButton(onPressed: _save, child: const Text('Save')),
          ],
        ),
        if (_status != null) ...[
          const SizedBox(height: 10),
          Text(
            _status!,
            style: TextStyle(
              fontSize: 12.5,
              color: _ok ? lyc.success : lyc.error,
            ),
          ),
        ],
      ],
    );
  }
}
