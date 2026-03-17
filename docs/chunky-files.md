# Chunky files

A chunky file extractor implementation is in `../go/chunky.go`.

## Tags

Entries have a 32-bit / 4-byte tag which maps directly to ASCII.

### `MBMP`

"Masked Bitmap" - an 8-bit / 256 indexed color bitmap with 8-bit transparency mask.
Decoder implemented in `../go/mbmp.go`.

