# AGENTS.md

This file gives coding agents fast context for working in this repository.

## Project

`climp` is a standalone CLI media player written in Go with a Bubble Tea TUI.

- Local playback: MP3, WAV, FLAC, OGG
- URL playback:
  - finite downloads via `yt-dlp`
  - live streams via `ffmpeg` for probe-routed live URLs (HLS, ICY/Icecast, many `.mp3`/`.ogg`, and some no-extension endpoints)
- Queue support for local directories, YouTube playlists/radio, local playlist files, and remote playlist URL expansion (`.pls`/`.m3u` wrappers)
- Real-time visualizers, progress, repeat, shuffle, speed control

Entry point: `main.go`

## Build and Verify

Use these commands before handing off changes:

```bash
go build -o climp.exe .
go vet ./...
go test ./...
```

If Go is missing from PATH on Windows, use:

```bash
"C:\Program Files\Go\bin\go.exe" build -o climp.exe .
```

## Architecture Map

- `internal/player/`: audio engine and decoder pipeline
  - decoders normalize to 16-bit LE PCM
  - live stream path: ffmpeg subprocess -> PCM pipe (`player.NewStream`)
  - pipeline: decoder -> countingReader -> speedReader -> Oto
  - `countingReader` also feeds visualizer ring buffer
- `internal/ui/`: Bubble Tea model, key handling, queue UI, download UI
- `internal/queue/`: playlist ordering, shuffle mapping, navigation
- `internal/downloader/`: `yt-dlp` integration, playlist extraction, URL classification
  - probe router: `ResolveURLRoute` / `IsLiveURL` in `internal/downloader/route.go`
- `internal/visualizer/`: all visualization modes + FFT analysis

## Visualizer Context

Visualizers are updated in the UI `vizTick` loop (every 50ms).

- Interface: `internal/visualizer/visualizer.go`
- Shared analysis: `internal/visualizer/fftbands.go`
- Shared color pipeline: `internal/visualizer/color.go`
- Shared motion smoothing: `internal/visualizer/spring_field.go` (Harmonica)

Current visualization modes include:

- vu meter
- spectrum
- waterfall
- waveform
- lissajous
- braille
- dense
- matrix
- hatching

Notes:

- Keep visual output cross-terminal (ANSI-based), with graceful color fallback.
- Respect `NO_COLOR`.
- Prefer low-allocation updates for per-frame paths.

## Queue and Playback Rules

- Queue state is mutated from Bubble Tea's update loop.
- Skip failed tracks when advancing.
- Speed setting persists across track changes.
- Seeking/restart recreates Oto player (no in-place seek).
- Live stream tracks are non-seekable (`Player.CanSeek() == false`).
- Repeat-one applies only to seekable tracks.
- Save (`s`) is enabled only for downloaded URL tracks (not live streams).

## External Tools

- `yt-dlp`: URL playback and playlist extraction
- `ffmpeg`:
  - live URL playback decode path (`ffmpeg -> s16le PCM pipe`)
  - save downloaded URL tracks to MP3 (`s` key)

## Platform Notes

- Primary dev platform is Windows.
- Do not use `/dev/null`, `nul`, or `NUL` in shell commands on this repo.

## Editing Guidelines

- Follow existing package structure and naming.
- Keep changes focused; avoid unrelated refactors.
- Do not introduce backend/cloud dependencies.
- Update `README.md` when keybindings or user-visible behavior changes.

## Common Tasks

### Add a new visualizer mode

1. Create a new file in `internal/visualizer/` implementing `Visualizer` (`Name`, `Update`, `View`).
2. Reuse shared helpers where possible:
   - FFT analysis: `FFTBands`
   - Color output: `color.go`
   - Motion smoothing: `spring_field.go`
3. Register the mode in `Modes()` at `internal/visualizer/visualizer.go`.
4. Update mode docs in `README.md` visualizer section and keybinding list.
5. Verify with `go build -o climp.exe .`, `go vet ./...`, and `go test ./...`.

### Add or change a keybinding

1. Update bindings in `internal/ui/keys.go` (definition + help text).
2. Handle the key in `Model.handleMsg` in `internal/ui/model.go`.
3. If behavior is user-visible, update the keybindings table in `README.md`.
4. Run build/vet/test commands.

### Change queue behavior

1. Start in `internal/ui/model.go` queue flow helpers (`skipToNext`, `jumpToSelected`, `removeSelected`, `findNextPlayable`).
2. If ordering logic changes, inspect `internal/queue/` (especially shuffle mapping).
3. Preserve current guarantees:
   - failed tracks are skipped
   - repeat/shuffle rules remain consistent
   - queue updates happen in Bubble Tea update loop
4. Run build/vet/test commands and sanity-check queue navigation keys.

## Live / HLS Status (Feb 2026)

Implemented behavior:

- URL routing is probe-first via `downloader.ResolveURLRoute`:
  - detects remote playlist wrappers (`.pls`, `.m3u`, `.m3u8`)
  - detects live streams via HLS/ICY/audio-stream signals (including `.mp3`, `.ogg`, and some no-extension stream URLs)
  - routes remaining URLs to finite `yt-dlp` downloads
- Direct URL startup:
  - live route -> try `player.NewStream(url)` first
  - if live setup fails, fallback is the existing finite `yt-dlp` download path
- Remote playlist URL behavior:
  - wrapper URLs are parsed and expanded into queue entries
  - entries are resolved to absolute HTTP(S) URLs
  - trailing semicolon URL artifacts (e.g. `http://.../;`) are normalized
  - nested remote wrappers are expanded recursively with depth cap (`maxRemotePlaylistDepth = 2`)
- Live playback path:
  - `ffmpeg` subprocess decodes input to PCM:
    - `-ac 2 -ar 44100 -f s16le pipe:1`
  - Player is non-seekable (`CanSeek=false`, unknown duration).
  - UI shows elapsed time + `LIVE`; seek keys no-op.
- Queue integration:
  - live URL entries can be `Ready` without a local temp file path
  - live-vs-finite checks use `downloader.IsLiveURL` (suffix or cached probe result)
  - previous/next and wrap logic treat live entries as playable and avoid resetting them to `Pending`

Current limitations / scope:

- probe routing adds a small network classification step per URL (timeout-based best effort)
- if probing fails unexpectedly, current behavior falls back to finite download path
- No app-level retry loop for live startup beyond ffmpeg reconnect flags.
- Save (`s`) remains download-only; no live recording mode.
