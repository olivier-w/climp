# climp-aac-decoder

Apache-2.0 licensed AAC file support for `climp`.

Current implementation notes:

- supports local `.aac`, `.m4a`, and `.m4b` inputs
- exposes a seekable PCM reader via `aacfile.Reader`
- parses ADTS and progressive MP4 AAC locally
- decodes AAC-LC to PCM in Go without `ffmpeg`
- rejects HE-AAC, HE-AACv2, SBR, and PS bitstreams
- uses `ffmpeg` only as a local validation oracle via `cmd/aacparity`
- still under active parity work before it should be treated as a production-grade standalone decoder
