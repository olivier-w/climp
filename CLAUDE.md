# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

climp is a standalone CLI media player for MP3 files, built with Go. It uses a Bubbletea TUI with real-time progress, volume control, ID3 tag display, and repeat mode. Supports URL playback via yt-dlp. No backend or cloud services.

Usage: `climp <file.mp3>` or `climp <url>`

## Build & Run

```bash
go build -o climp.exe .
go vet ./...
go test ./...
```

If `go` is not on PATH (freshly installed via winget), use `"C:\Program Files\Go\bin\go.exe"` directly.

## Architecture

Three main subsystems connected through `main.go`:

**Audio engine** (`internal/player/`) — Oto v3 handles audio output, go-mp3 decodes. A `countingReader` wraps the decoder to track byte position as Oto reads from it. Position is converted to time via `pos / (sampleRate * channels * bitDepth)`. All Player methods are mutex-protected since Oto runs audio in its own goroutine. The Oto context is initialized once globally via `sync.Once`.

**Seeking** requires recreating the Oto player (no in-place seek support): pause current player, seek the decoder, reset the byte counter, create a new Oto player from the same countingReader. Byte positions must be aligned to 4-byte frame boundaries (stereo 16-bit). `Restart()` uses the same approach to seek to 0 for repeat mode, also resetting the done channel and monitor goroutine.

**Downloader** (`internal/downloader/`) — URL detection and yt-dlp integration. `Download()` runs yt-dlp as a subprocess to extract audio as MP3 into a temp file. Streams yt-dlp output via callback for progress display. Returns a cleanup func for temp file removal.

**TUI** (`internal/ui/`) — Standard Bubbletea Model pattern. A 200ms tick polls `player.Position()` to update the progress bar. A blocking goroutine waits on `player.Done()` channel to detect playback end. On repeat-one, playback end triggers `player.Restart()` and re-watches the new done channel instead of quitting. Repeat mode is defined in `repeat.go`. The View renders the "Option A" minimal layout with adaptive colors for light/dark terminals.

**Data flow**: `tea.KeyMsg` → player controls (pause/seek/volume) → next tick updates UI state from player. `tea.WindowSizeMsg` drives responsive progress bar width.

## Dependencies

- `ebitengine/oto/v3` — Cross-platform audio output (WASAPI on Windows, CoreAudio on macOS, ALSA on Linux)
- `hajimehoshi/go-mp3` — MP3 decoding
- `bogem/id3v2/v2` — ID3 tag reading
- `charmbracelet/bubbletea` + `lipgloss` — TUI framework and styling

## Platform Notes

- **Windows**: Oto uses WASAPI, no external deps needed. This is the primary dev platform.
- **Linux**: Oto needs ALSA; building requires `libasound2-dev`.
- **macOS**: Oto uses Core Audio, no external deps.
- NEVER use `/dev/null`, `nul`, or `NUL` in bash commands on this Windows system — these create actual files and break git.

## Keybindings

Defined in `internal/ui/keys.go`. Vim-style alternatives (h/j/k/l) mirror arrow keys. `r` toggles repeat mode.

## External Tools

- **yt-dlp** — Optional runtime dependency for URL playback. Not a Go dependency; invoked as a subprocess.
