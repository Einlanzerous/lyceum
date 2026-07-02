import 'package:flutter/material.dart';

import '../theme/lyceum_colors.dart';

/// A pill segmented control matching the web `.seg`: a rounded container with
/// the active segment filled brass (onBrass text) and inactive segments dim.
class LycSegmentedControl<T> extends StatelessWidget {
  const LycSegmentedControl({
    super.key,
    required this.options,
    required this.selected,
    required this.onChanged,
  });

  final List<({T value, String label})> options;
  final T selected;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Container(
      padding: const EdgeInsets.all(3),
      decoration: BoxDecoration(
        color: lyc.bg,
        borderRadius: BorderRadius.circular(LycRadii.pill),
        border: Border.all(color: lyc.border),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (final opt in options)
            _Segment(
              label: opt.label,
              active: opt.value == selected,
              onTap: () => onChanged(opt.value),
            ),
        ],
      ),
    );
  }
}

class _Segment extends StatelessWidget {
  const _Segment({
    required this.label,
    required this.active,
    required this.onTap,
  });

  final String label;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return GestureDetector(
      onTap: onTap,
      behavior: HitTestBehavior.opaque,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 160),
        curve: Curves.easeOut,
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
        decoration: BoxDecoration(
          color: active ? lyc.brass : Colors.transparent,
          borderRadius: BorderRadius.circular(LycRadii.pill),
        ),
        child: Text(
          label,
          style: TextStyle(
            fontSize: 13,
            fontWeight: active ? FontWeight.w700 : FontWeight.w600,
            color: active ? lyc.onBrass : lyc.dim,
          ),
        ),
      ),
    );
  }
}
