# Native image fixtures

These synthetic fixtures were created for Foundry's image tests. They contain no
user photos or identifying metadata and may be redistributed with the framework.

- `linear-srgb.icc`: LittleCMS-created RGB profile with D65/sRGB primaries and
  linear (gamma 1) tone curves.
- `linear-srgb.png`: 24×16 pixels, two synthetic color blocks, embedded profile.
- `converted-srgb.png`: independently converted to sRGB by Pillow/ImageCms with
  perceptual intent. Compare with a small rounding tolerance across library builds.
- `oriented-exif.jpg`: the same blocks with EXIF orientation 6 and the synthetic
  manufacturer string “Foundry synthetic camera”.
- `animated.jxl`: four 16×12 synthetic RGBA frames, durations 30/70/110/130 ms,
  loop count 2; converted from the synthetic WebP encoder-check output by libvips.

Generated with the existing Pillow runtime and Homebrew LittleCMS on 2026-10-03.
