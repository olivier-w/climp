# AGENTS.md

This file gives coding agents fast context for working in this repository.

## Project

`climp` is a standalone CLI media player written in Go with a Bubble Tea TUI.

- Local playback: MP3, WAV, FLAC, OGG, MP4, MKV, WEBM, MOV
- URL playback via `yt-dlp`
- Queue support for local directories and YouTube playlists/radio
- Real-time visualizers, progress, repeat, shuffle, speed control
- Terminal video playback via ANSI half-block rendering (requires `ffmpeg`/`ffprobe`)

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
  - `ffmpeg_decoder.go`: ffmpeg subprocess fallback for video/container formats
- `internal/ui/`: Bubble Tea model, key handling, queue UI, download UI
- `internal/queue/`: playlist ordering, shuffle mapping, navigation
- `internal/downloader/`: `yt-dlp` integration and playlist extraction
- `internal/visualizer/`: all visualization modes + FFT analysis
- `internal/video/`: terminal video rendering pipeline
  - `probe.go`: ffprobe metadata extraction (resolution, fps, duration)
  - `session.go`: ffmpeg frame decode subprocess + clock-synced frame delivery
  - `renderer.go`: RGB24 -> ANSI half-block or ASCII brightness conversion
  - `palette.go`: color mode detection, ANSI escape generation
- `internal/media/`: shared media type detection and extension helpers

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
- `ffmpeg`: save current URL stream to MP3 (`s` key), video frame decoding, container audio extraction
- `ffprobe`: video metadata probing (resolution, fps, duration)

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
