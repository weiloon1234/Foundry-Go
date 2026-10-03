# AVIF codec regression fixtures

These unmodified fixtures come from `github.com/gen2brain/gav1d` v0.2.5,
`avif/testdata`, the version already used by Foundry. They cover 8/10/12-bit
planes, subsampling, alpha, grids, clean aperture, rotation, mirroring and a
sequence. Tests compare decoded dimensions, pixels, timing and loop policy;
malformed variants are built in memory without modifying these fixtures.

The upstream BSD-2-Clause notices and patent terms are retained in `COPYING`
and `PATENTS`. Source: https://github.com/gen2brain/gav1d/tree/v0.2.5/avif/testdata
