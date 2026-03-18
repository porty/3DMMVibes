# Chunky files

A chunky file extractor implementation is in `../go/chunky.go`.

For a full catalogue of chunk types used by 3D Movie Maker movies see [movie.md](movie.md).
Sound-related chunks (`MSND`, `SND `, `GLMS`) are documented in [sounds.md](sounds.md).

## Tags

Entries have a 32-bit / 4-byte tag which maps directly to ASCII.

### `MBMP`

"Masked Bitmap" - an 8-bit / 256 indexed color bitmap with 8-bit transparency mask.
Decoder implemented in `../go/mbmp.go`.

