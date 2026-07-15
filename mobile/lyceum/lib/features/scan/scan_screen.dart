import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../api/models.dart';
import '../../theme/lyceum_colors.dart';
import 'scan_controller.dart';

/// Continuous-batch ISBN scanner (LYCM-602): scan a stack of books, then send
/// them all as one review batch. Reviewing/confirming happens on web/desktop.
class ScanScreen extends ConsumerWidget {
  const ScanScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final phase = ref.watch(scanControllerProvider.select((s) => s.phase));
    final showResult = phase == ScanPhase.sent || phase == ScanPhase.error;
    return Scaffold(
      backgroundColor: Colors.black,
      body: SafeArea(
        child: showResult ? const _ResultView() : const _CameraCapture(),
      ),
    );
  }
}

/// The live camera + capture overlay. Owns the scanner controller.
class _CameraCapture extends ConsumerStatefulWidget {
  const _CameraCapture();

  @override
  ConsumerState<_CameraCapture> createState() => _CameraCaptureState();
}

class _CameraCaptureState extends ConsumerState<_CameraCapture>
    with SingleTickerProviderStateMixin {
  final MobileScannerController _controller = MobileScannerController(
    formats: const [BarcodeFormat.ean13],
    detectionSpeed: DetectionSpeed.noDuplicates,
    detectionTimeoutMs: 400,
  );

  // Scan-feedback toast — drops in from the top so it never covers the Send
  // button at the bottom (LYCM-75). Flutter SnackBars only rise from the bottom,
  // so this is a small self-managed overlay instead.
  late final AnimationController _toastCtl = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 220),
  );
  String? _toastText;
  Timer? _toastTimer;

  @override
  void dispose() {
    _toastTimer?.cancel();
    _toastCtl.dispose();
    _controller.dispose();
    super.dispose();
  }

  Future<void> _onDetect(BarcodeCapture capture) async {
    for (final barcode in capture.barcodes) {
      final raw = barcode.rawValue;
      if (raw == null || raw.isEmpty) continue;
      final outcome = await ref
          .read(scanControllerProvider.notifier)
          .addRaw(raw);
      if (!mounted) return;
      _feedback(outcome);
    }
  }

  void _feedback(ScanOutcome outcome) {
    final message = ref.read(scanControllerProvider).message;
    switch (outcome) {
      case ScanOutcome.added:
        HapticFeedback.mediumImpact();
      case ScanOutcome.duplicate:
      case ScanOutcome.invalid:
        HapticFeedback.selectionClick();
    }
    if (message != null) _toast(message);
  }

  void _toast(String text) {
    _toastTimer?.cancel();
    setState(() => _toastText = text);
    _toastCtl.forward(from: 0);
    _toastTimer = Timer(const Duration(milliseconds: 900), () {
      if (mounted) _toastCtl.reverse();
    });
  }

  Future<void> _manualEntry() async {
    final code = await showDialog<String>(
      context: context,
      builder: (_) => const _ManualEntryDialog(),
    );
    if (code == null || code.trim().isEmpty) return;
    final outcome = await ref
        .read(scanControllerProvider.notifier)
        .addRaw(code, source: 'manual');
    if (mounted) _feedback(outcome);
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final state = ref.watch(scanControllerProvider);

    return Stack(
      children: [
        Positioned.fill(
          child: MobileScanner(controller: _controller, onDetect: _onDetect),
        ),

        // Top row: back + title + torch.
        Positioned(
          top: 8,
          left: 8,
          right: 8,
          child: Row(
            children: [
              _RoundButton(
                icon: Icons.arrow_back,
                onTap: () => context.pop(),
                tooltip: 'Back',
              ),
              const SizedBox(width: 8),
              const Text(
                'Scan books',
                style: TextStyle(
                  color: Colors.white,
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const Spacer(),
              ValueListenableBuilder(
                valueListenable: _controller,
                builder: (_, mscState, _) {
                  final on = mscState.torchState == TorchState.on;
                  return _RoundButton(
                    icon: on ? Icons.flash_on : Icons.flash_off,
                    onTap: () => _controller.toggleTorch(),
                    tooltip: 'Torch',
                  );
                },
              ),
            ],
          ),
        ),

        // A centered reticle hint.
        Center(
          child: Container(
            width: 260,
            height: 150,
            decoration: BoxDecoration(
              border: Border.all(color: Colors.white70, width: 2),
              borderRadius: BorderRadius.circular(12),
            ),
          ),
        ),

        // Bottom panel: count, manual entry, send.
        Positioned(
          left: 0,
          right: 0,
          bottom: 0,
          child: _CapturePanel(
            state: state,
            sending: state.phase == ScanPhase.sending,
            onManual: _manualEntry,
            onSend: () => ref.read(scanControllerProvider.notifier).send(),
            onRemove: (isbn) =>
                ref.read(scanControllerProvider.notifier).removeIsbn(isbn),
          ),
        ),

        if (state.phase == ScanPhase.sending)
          Positioned.fill(
            child: ColoredBox(
              color: Colors.black54,
              child: Center(
                child: CircularProgressIndicator(color: lyc.brassBright),
              ),
            ),
          ),

        // Scan feedback, dropping in from the top (clear of the Send button).
        Positioned(
          top: 60,
          left: 16,
          right: 16,
          child: _TopToast(animation: _toastCtl, text: _toastText),
        ),
      ],
    );
  }
}

/// A transient message that slides + fades in from the top of the scanner, used
/// for scan feedback ("Added …", "Already scanned", "Not a book barcode").
class _TopToast extends StatelessWidget {
  const _TopToast({required this.animation, required this.text});

  final Animation<double> animation;
  final String? text;

  @override
  Widget build(BuildContext context) {
    if (text == null) return const SizedBox.shrink();
    final lyc = context.lyc;
    return IgnorePointer(
      child: Align(
        alignment: Alignment.topCenter,
        child: FadeTransition(
          opacity: animation,
          child: SlideTransition(
            position: Tween<Offset>(
              begin: const Offset(0, -0.7),
              end: Offset.zero,
            ).animate(CurvedAnimation(parent: animation, curve: Curves.easeOut)),
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
              decoration: BoxDecoration(
                color: lyc.bg.withValues(alpha: 0.94),
                borderRadius: BorderRadius.circular(12),
                border: Border.all(color: lyc.borderStrong),
              ),
              child: Text(
                text!,
                style: TextStyle(
                  color: lyc.text,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// The glass bottom sheet over the camera: running count, the session list, a
/// manual-entry button, and Send.
class _CapturePanel extends StatelessWidget {
  const _CapturePanel({
    required this.state,
    required this.sending,
    required this.onManual,
    required this.onSend,
    required this.onRemove,
  });

  final ScanState state;
  final bool sending;
  final VoidCallback onManual;
  final VoidCallback onSend;
  final void Function(String isbn) onRemove;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final count = state.count;
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 20),
      decoration: BoxDecoration(
        color: lyc.bg.withValues(alpha: 0.94),
        borderRadius: const BorderRadius.vertical(top: Radius.circular(18)),
        border: Border(top: BorderSide(color: lyc.borderStrong)),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Text(
                count == 0
                    ? 'Point at a book barcode'
                    : '$count scanned this session',
                style: TextStyle(
                  color: lyc.text,
                  fontWeight: FontWeight.w700,
                  fontSize: 15,
                ),
              ),
              const Spacer(),
              TextButton.icon(
                onPressed: onManual,
                icon: const Icon(Icons.keyboard, size: 18),
                label: const Text('Enter ISBN'),
                style: TextButton.styleFrom(foregroundColor: lyc.brassBright),
              ),
            ],
          ),
          if (count > 0) ...[
            const SizedBox(height: 4),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 132),
              child: ListView(
                shrinkWrap: true,
                children: [
                  for (final s in state.scans.reversed)
                    Dismissible(
                      key: ValueKey(s.isbn),
                      direction: DismissDirection.endToStart,
                      onDismissed: (_) => onRemove(s.isbn),
                      background: Container(
                        alignment: Alignment.centerRight,
                        padding: const EdgeInsets.only(right: 16),
                        child: Icon(Icons.delete_outline, color: lyc.error),
                      ),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(vertical: 6),
                        child: Row(
                          children: [
                            Icon(Icons.qr_code, size: 16, color: lyc.dim),
                            const SizedBox(width: 8),
                            Text(
                              s.isbn,
                              style: TextStyle(
                                color: lyc.muted,
                                fontFeatures: const [
                                  FontFeature.tabularFigures(),
                                ],
                              ),
                            ),
                            if (s.source == 'manual') ...[
                              const SizedBox(width: 6),
                              Text(
                                '· typed',
                                style: TextStyle(color: lyc.dim, fontSize: 12),
                              ),
                            ],
                          ],
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ],
          const SizedBox(height: 8),
          FilledButton(
            onPressed: (count == 0 || sending) ? null : onSend,
            style: FilledButton.styleFrom(
              backgroundColor: lyc.brass,
              foregroundColor: lyc.onBrass,
              padding: const EdgeInsets.symmetric(vertical: 14),
            ),
            child: Text(
              count == 0
                  ? 'Done'
                  : 'Send $count ${count == 1 ? "book" : "books"}',
              style: const TextStyle(fontWeight: FontWeight.w700),
            ),
          ),
        ],
      ),
    );
  }
}

/// The read-only result after a batch is sent (or an error to retry). No
/// editing/approving here — that is a web/desktop step.
class _ResultView extends ConsumerWidget {
  const _ResultView();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final state = ref.watch(scanControllerProvider);
    final notifier = ref.read(scanControllerProvider.notifier);

    if (state.phase == ScanPhase.error) {
      return _Centered(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.cloud_off_outlined, size: 40, color: lyc.error),
            const SizedBox(height: 12),
            Text(
              state.error ?? 'Something went wrong.',
              textAlign: TextAlign.center,
              style: TextStyle(color: lyc.text),
            ),
            const SizedBox(height: 20),
            FilledButton(
              onPressed: notifier.send,
              style: FilledButton.styleFrom(
                backgroundColor: lyc.brass,
                foregroundColor: lyc.onBrass,
              ),
              child: const Text('Try again'),
            ),
            TextButton(
              onPressed: notifier.resume,
              child: Text('Keep scanning', style: TextStyle(color: lyc.muted)),
            ),
          ],
        ),
      );
    }

    final batch = state.result;
    final counts = batch?.counts;
    return Padding(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const SizedBox(height: 8),
          Row(
            children: [
              Icon(Icons.check_circle, color: lyc.success),
              const SizedBox(width: 10),
              Text(
                'Sent ${counts?.total ?? 0} to review',
                style: TextStyle(
                  color: lyc.text,
                  fontSize: 18,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ),
          const SizedBox(height: 6),
          Text(
            'Confirm and acquire these on the web or desktop library.',
            style: TextStyle(color: lyc.muted, fontSize: 13),
          ),
          if (counts != null) ...[
            const SizedBox(height: 14),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                _CountChip(
                  label: 'ready',
                  value: counts.ready,
                  color: lyc.success,
                ),
                _CountChip(
                  label: 'to review',
                  value: counts.review,
                  color: lyc.brassBright,
                ),
                _CountChip(
                  label: 'no match',
                  value: counts.noMatch,
                  color: lyc.error,
                ),
                _CountChip(
                  label: 'duplicate',
                  value: counts.duplicate,
                  color: lyc.dim,
                ),
              ],
            ),
          ],
          const SizedBox(height: 16),
          Expanded(
            child: ListView(
              children: [
                for (final c in batch?.candidates ?? const <Candidate>[])
                  _CandidateRow(candidate: c),
              ],
            ),
          ),
          Row(
            children: [
              Expanded(
                child: OutlinedButton(
                  onPressed: notifier.resume,
                  child: const Text('Scan more'),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: FilledButton(
                  onPressed: () => context.pop(),
                  style: FilledButton.styleFrom(
                    backgroundColor: lyc.brass,
                    foregroundColor: lyc.onBrass,
                  ),
                  child: const Text('Done'),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _CandidateRow extends StatelessWidget {
  const _CandidateRow({required this.candidate});
  final Candidate candidate;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final (label, color) = _statusStyle(candidate.status, lyc);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  candidate.title?.isNotEmpty == true
                      ? candidate.title!
                      : candidate.isbn,
                  style: TextStyle(
                    color: lyc.text,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                if (candidate.author?.isNotEmpty == true)
                  Text(
                    candidate.author!,
                    style: TextStyle(color: lyc.muted, fontSize: 13),
                  ),
              ],
            ),
          ),
          const SizedBox(width: 10),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 3),
            decoration: BoxDecoration(
              color: color.withValues(alpha: 0.16),
              borderRadius: BorderRadius.circular(999),
            ),
            child: Text(
              label,
              style: TextStyle(
                color: color,
                fontSize: 12,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
        ],
      ),
    );
  }

  (String, Color) _statusStyle(String status, LyceumPalette lyc) {
    switch (status) {
      case 'ready':
        return ('ready', lyc.success);
      case 'review':
        return ('needs review', lyc.brassBright);
      case 'duplicate':
        return ('duplicate', lyc.dim);
      case 'no_match':
        return ('no match', lyc.error);
      default:
        return (status, lyc.muted);
    }
  }
}

class _CountChip extends StatelessWidget {
  const _CountChip({
    required this.label,
    required this.value,
    required this.color,
  });
  final String label;
  final int value;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.14),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        '$value $label',
        style: TextStyle(
          color: color,
          fontSize: 12,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}

class _ManualEntryDialog extends StatefulWidget {
  const _ManualEntryDialog();

  @override
  State<_ManualEntryDialog> createState() => _ManualEntryDialogState();
}

class _ManualEntryDialogState extends State<_ManualEntryDialog> {
  final _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Enter ISBN'),
      content: TextField(
        controller: _controller,
        autofocus: true,
        keyboardType: TextInputType.number,
        decoration: const InputDecoration(hintText: '978…'),
        onSubmitted: (v) => Navigator.of(context).pop(v),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(_controller.text),
          child: const Text('Add'),
        ),
      ],
    );
  }
}

class _RoundButton extends StatelessWidget {
  const _RoundButton({required this.icon, required this.onTap, this.tooltip});
  final IconData icon;
  final VoidCallback onTap;
  final String? tooltip;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.black45,
      shape: const CircleBorder(),
      child: IconButton(
        icon: Icon(icon, color: Colors.white),
        tooltip: tooltip,
        onPressed: onTap,
      ),
    );
  }
}

class _Centered extends StatelessWidget {
  const _Centered({required this.child});
  final Widget child;
  @override
  Widget build(BuildContext context) => Center(
    child: Padding(padding: const EdgeInsets.all(28), child: child),
  );
}
