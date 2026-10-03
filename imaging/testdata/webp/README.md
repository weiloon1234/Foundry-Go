# Synthetic WebP fixtures

Framework-owned 16×12 RGBA canvases, encoded by the already installed Pillow /
libwebp runtime. `animation-lossless.webp` uses lossless compression;
`animation-lossy.webp` uses quality 80. Both have four frames, durations
30/70/110/130 milliseconds and a total play count of 2. The PNG files are the
composed RGBA canvases decoded independently by Pillow/libwebp.

The source canvases start transparent. Frame 0 contains an opaque red rectangle
at (0,0)..(9,7); frame 1 contains a blue rectangle with alpha 128 at (4,2)..(15,11);
frame 2 contains the red rectangle and an opaque green rectangle at
(8,4)..(15,11); frame 3 contains an opaque white square at (2,2)..(5,5).
Pillow's `save_all=True`, `append_images`, `duration=[30,70,110,130]`, `loop=2`
and `lossless=True` or `lossless=False, quality=80` produce the animations.
The resulting ANMF chunks exercise nonzero offsets, both disposal values,
replacement and alpha blending. Lossy RGB conversion/chroma upsampling can vary
between decoders; tests compare alpha and lossless pixels exactly and lossy RGB within three
channel levels of libwebp.
No additional dependency or external image license is required.
