# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## Project

climp is a standalone CLI media player built with Go and Bubble Tea.

- Local playback: MP3, WAV, FLAC, OGG
- URL playback has two paths:
  - finite download path via `yt-dlp` (`yt-dlp -> temp WAV -> player.New(path)`)
  - live stream path via `ffmpeg` for suffix-routed URLs (`.m3u8`, `.m3u`, `.aac`) using `player.NewStream(url)`
- Queue support for local directory playback, YouTube playlists/radio, and local playlist files with mixed local/URL entries
- Repeat, shuffle, speed control, real-time visualizers

Usage: `climp <file|url|playlist>`

## Build & Run

```bash
go build -o climp.exe .
go vet ./...
go test ./...
```

If `go` is not on PATH on Windows, use `"C:\Program Files\Go\bin\go.exe"` directly.

## Architecture

### Audio Engine (`internal/player/`)

- Oto v3 outputs 16-bit LE PCM.
- File decoders in `decoder.go` implement `audioDecoder` and normalize to PCM.
- Live streams in `stream.go` run ffmpeg:
  - `ffmpeg ... -ac 2 -ar 44100 -f s16le pipe:1`
- Playback pipeline:
  - decoder -> `countingReader` -> `speedReader` -> Oto
- `countingReader` tracks playback position and fills visualizer ring buffer.
- `Player.CanSeek()` distinguishes seekable file playback from non-seekable live streams.

### Seeking and Repeat

- Seeking/restart recreates the Oto player (no in-place seek).
- Live streams are non-seekable:
  - seek keys are no-op
  - repeat-one restart is skipped for live tracks

### Downloader (`internal/downloader/`)

- `Download()` uses yt-dlp to download one URL as WAV into a temp folder.
- `ExtractPlaylist()` uses `yt-dlp --flat-playlist` (up to 50 entries).
- `IsLiveBySuffix()` routes `.m3u8`, `.m3u`, `.aac` to live-first behavior.
- For suffix-routed URLs:
  - try `player.NewStream(url)` first
  - if live setup fails, fallback to yt-dlp download path
- Non-suffix URLs stay on yt-dlp path (no reverse fallback to live).

### Queue (`internal/queue/` + `internal/ui/model.go`)

- Queue is mutated only in Bubble Tea update loop.
- Failed tracks are skipped when advancing.
- Live URL entries are playable without local temp file paths.
- Wrap/reset logic avoids turning completed live entries back into `Pending`.
- Shuffle mapping stays separate from track storage.

### UI (`internal/ui/`)

- 200ms `tick` updates elapsed/volume/pause.
- 50ms `vizTick` updates visualizers.
- Seekable tracks show normal progress bar and duration.
- Live tracks show elapsed + `LIVE`.
- `s` (save) is enabled only for downloaded URL playback, not live streams.

## Keybindings

Defined in `internal/ui/keys.go` and handled in `internal/ui/model.go`.

- `left/right` (`h/l`) seek only when current player is seekable.
- `x` cycles speed; speed persists across track changes.
- `r` cycles repeat off/song/playlist.
- `z` toggles shuffle for queue playback.
- `s` saves downloaded URL playback to MP3; disabled for live streams.

## External Tools

- `yt-dlp`:
  - finite URL playback
  - playlist extraction
- `ffmpeg`:
  - live URL playback decode path (`player.NewStream`)
  - save downloaded URL playback to MP3 (`s` key)

## Platform Notes

- Primary development platform: Windows.
- Do not use `/dev/null`, `nul`, or `NUL` in shell commands on this repo.

## Known Issue

Visualizer FPS can drop when queue list is visible (especially non-Matrix modes). App-side caching improvements are already in place in `internal/ui/model.go`; remaining bottleneck is likely renderer/terminal-side.
