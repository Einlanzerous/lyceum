import 'package:flutter/material.dart';

import '../theme/lyceum_colors.dart';
import '../theme/lyceum_theme.dart';

/// The Lyceum brand lockup: a 45°-rotated brass square + the "LYCEUM"
/// wordmark, matching the web top bar.
class BrandMark extends StatelessWidget {
  const BrandMark({super.key, this.showWordmark = true, this.size = 16});

  final bool showWordmark;
  final double size;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Transform.rotate(
          angle: 0.785398, // 45°
          child: Container(
            width: size,
            height: size,
            decoration: BoxDecoration(
              color: lyc.brass,
              borderRadius: BorderRadius.circular(3),
            ),
          ),
        ),
        if (showWordmark) ...[
          const SizedBox(width: 12),
          Text(
            'LYCEUM',
            style: TextStyle(
              fontFamily: kDisplayFont,
              fontSize: 16,
              fontWeight: FontWeight.w800,
              letterSpacing: 3.0,
              color: lyc.text,
            ),
          ),
        ],
      ],
    );
  }
}
