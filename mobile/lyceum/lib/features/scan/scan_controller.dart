import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/api_providers.dart';
import '../../api/client.dart';
import '../../api/models.dart';
import 'isbn.dart';
import 'scan_queue.dart';

/// Where the scan session is in its lifecycle.
enum ScanPhase { scanning, sending, sent, error }

/// Result of trying to add a scan, for immediate UI feedback (beep/haptic/toast).
enum ScanOutcome { added, duplicate, invalid }

/// Immutable state of a scan session: the pending scans, the phase, the last
/// transient feedback message, and (once sent) the server's batch result or an
/// error.
class ScanState {
  const ScanState({
    this.scans = const [],
    this.phase = ScanPhase.scanning,
    this.result,
    this.error,
    this.message,
  });

  final List<ScannedIsbn> scans;
  final ScanPhase phase;
  final Batch? result;
  final String? error;

  /// Transient feedback for the last add attempt (e.g. "Already scanned").
  final String? message;

  int get count => scans.length;

  ScanState copyWith({
    List<ScannedIsbn>? scans,
    ScanPhase? phase,
    Batch? result,
    String? error,
    String? message,
  }) => ScanState(
    scans: scans ?? this.scans,
    phase: phase ?? this.phase,
    result: result ?? this.result,
    error: error,
    message: message,
  );
}

/// Drives a continuous-batch scan session (LYCM-602): validate + dedupe each
/// scan into a persisted queue, then flush the whole set as one review batch.
/// Review/confirm is a web/desktop step, so this never edits or approves.
class ScanController extends Notifier<ScanState> {
  @override
  ScanState build() => ScanState(scans: ref.read(scanQueueProvider).load());

  ScanQueue get _queue => ref.read(scanQueueProvider);

  /// Validates a raw code (barcode payload or typed text), and if it is a new
  /// valid ISBN, appends it to the queue. Returns an outcome for UI feedback.
  Future<ScanOutcome> addRaw(String raw, {String source = 'camera'}) async {
    final isbn = normalizeIsbn(raw);
    if (isbn == null) {
      state = state.copyWith(
        phase: ScanPhase.scanning,
        message: 'Not a book barcode',
      );
      return ScanOutcome.invalid;
    }
    if (state.scans.any((s) => s.isbn == isbn)) {
      state = state.copyWith(
        phase: ScanPhase.scanning,
        message: 'Already scanned',
      );
      return ScanOutcome.duplicate;
    }
    final scan = ScannedIsbn(
      isbn: isbn,
      capturedAt: DateTime.now().toUtc(),
      source: source,
    );
    final r = await _queue.add(scan);
    state = state.copyWith(
      scans: r.scans,
      phase: ScanPhase.scanning,
      message: 'Added $isbn',
    );
    return r.added ? ScanOutcome.added : ScanOutcome.duplicate;
  }

  /// Removes a queued scan (swipe-to-remove in the session list).
  Future<void> removeIsbn(String isbn) async {
    final next = await _queue.remove(isbn);
    state = state.copyWith(scans: next, phase: ScanPhase.scanning);
  }

  /// Flushes the queue as one batch. On success the queue is cleared and the
  /// result is shown; on failure the queue is kept so it can be retried (e.g.
  /// after reconnecting).
  Future<void> send() async {
    if (state.scans.isEmpty) return;
    state = state.copyWith(phase: ScanPhase.sending);
    try {
      final batch = await ref
          .read(lyceumClientProvider)
          .createBatch(state.scans);
      await _queue.clear();
      state = ScanState(phase: ScanPhase.sent, result: batch);
    } on ApiException catch (e) {
      state = state.copyWith(phase: ScanPhase.error, error: e.message);
    } catch (_) {
      state = state.copyWith(
        phase: ScanPhase.error,
        error: "Couldn't reach the library. Your scans are saved — try again.",
      );
    }
  }

  /// Returns to scanning after viewing a result or error (reloads any queue that
  /// survived a failed send).
  void resume() {
    state = ScanState(scans: _queue.load());
  }
}

final scanControllerProvider = NotifierProvider<ScanController, ScanState>(
  ScanController.new,
);
