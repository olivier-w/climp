# AGENTS.md

This file gives coding agents fast context for working in this repository.

## Project

`climp` is a standalone CLI media player written in Go with a Bubble Tea TUI.

- Local playback: MP3, WAV, FLAC, OGG
- URL playback via `yt-dlp`
- Queue support for local directories and YouTube playlists/radio
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
  - pipeline: decoder -> countingReader -> speedReader -> Oto
  - `countingReader` also feeds visualizer ring buffer
- `internal/ui/`: Bubble Tea model, key handling, queue UI, download UI
- `internal/queue/`: playlist ordering, shuffle mapping, navigation
- `internal/downloader/`: `yt-dlp` integration and playlist extraction
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

## External Tools

- `yt-dlp`: URL playback and playlist extraction
- `ffmpeg`: save current URL stream to MP3 (`s` key)

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

## Recent Radio URL Investigation (Feb 2026)

Context: many public `.m3u` / `.m3u8` radio URLs were failing or hanging on `Fetching info...` / `Starting download...`.

What was tried:

- Added mixed local+URL local playlist parsing (`internal/media/playlist.go`) and queue construction for URL entries (`main.go`).
- Added yt-dlp inactivity watchdog logic in `internal/downloader/downloader.go`:
  - 15s cap in fetching.
  - timeout handling in downloading/converting phases.
  - short user-facing error summaries in UI (`internal/ui/model.go`).
- Added URL normalization and scheme validation (`http`/`https` only).
- Added "likely live stream" classification heuristic after timeout.

What we learned:

- Current URL playback path is finite-download oriented: `yt-dlp -> temp WAV -> player.New(filePath)`.
- Many radio endpoints are infinite streams (Shoutcast/Icecast/HLS) and do not produce finite completion behavior.
- Some endpoints emit enough yt-dlp output to keep naive "any activity" watchdogs alive, while still not making real progress.
- Example repro URL:
  - `https://sonic.portalfoxmix.cl:7028/stream/1/`
  - `curl -I` shows `Content-Type: audio/mpeg` plus ICY headers (`icy-notice`, `icy-br`, etc), which indicates live stream behavior.
- Even when UI moves from `Fetching info...` to `Starting download...`, it can still hang because there is no finite artifact to finish.

Why this is happening technically:

- `internal/player.Player` expects file-backed, seekable decoders (`audioDecoder` is `io.ReadSeeker` with finite `Length()`).
- There is no live stream decoder path in `internal/player`.
- No ffmpeg stream decode path exists in this repo right now; ffmpeg is only used for save-to-mp3.

## Live / HLS Support Ideas (Earlier Proposals)

1. Minimal viable live path (recommended first):
   - Add `player.NewStream(url)` backed by ffmpeg subprocess:
     - `ffmpeg -i <url> -f s16le -ac 2 -ar 44100 pipe:1`
   - Wrap stdout with a non-seekable decoder implementation.
   - Expose `CanSeek=false` and unknown duration for live tracks.
   - UI behavior for live:
     - disable left/right seek.
     - show `LIVE` instead of normal duration/progress semantics.

2. URL routing strategy:
   - Keep existing yt-dlp finite-download path for YouTube and normal URLs.
   - Route direct stream URLs (`.m3u8`, ICY stream URLs, direct audio stream endpoints) to `player.NewStream`.
   - Fallback order:
     - try live stream path first for stream-like URLs.
     - on setup failure, fallback to yt-dlp path when appropriate.

3. Robust ffmpeg settings for unstable live streams:
   - include reconnect flags (`-reconnect 1`, `-reconnect_streamed 1`, etc) where supported.
   - keep startup timeout and surface concise errors.

4. Queue integration:
   - Allow queue tracks that represent live URLs.
   - Skip failed live tracks the same way failed download tracks are skipped.
   - Keep repeat/shuffle semantics consistent.

5. Test plan for live support:
   - unit tests for URL classification and seek-disabled behavior.
   - manual smoke tests with known ICY/HLS live URLs.
   - verify no regressions for current local-file and YouTube finite-download flows.

Notes for next agent:

- Do not market `.m3u8` URL support as complete until live path exists.
- If not implementing live path yet, position current behavior as fail-fast + skip only.
