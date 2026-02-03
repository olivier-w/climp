# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

climp is a standalone CLI media player built with Go. Supports MP3, WAV, FLAC, and OGG Vorbis. Uses a Bubbletea TUI with real-time progress, volume control, ID3 tag display, repeat mode, and shuffle mode. Supports URL playback via yt-dlp with a bubbles-based download progress UI. YouTube playlists and radio URLs are automatically detected — the first track plays immediately while remaining tracks (up to 50) are extracted and downloaded in the background. Local directory playlists are built automatically when playing a file that has sibling audio files. No backend or cloud services.

Usage: `climp <file.mp3|.wav|.flac|.ogg>` or `climp <url|playlist-url>`

## Build & Run

```bash
go build -o climp.exe .
go vet ./...
go test ./...
```

If `go` is not on PATH (freshly installed via winget), use `"C:\Program Files\Go\bin\go.exe"` directly.

## Architecture

Three main subsystems connected through `main.go`:

**Audio engine** (`internal/player/`) — Oto v3 handles audio output. Format-specific decoders (MP3, WAV, FLAC, OGG) all implement an `audioDecoder` interface in `decoder.go`, providing `Read()`, `Seek()`, `Length()`, `SampleRate()`, and `ChannelCount()`. All decoders normalize output to 16-bit LE PCM. A `countingReader` wraps the decoder to track byte position as Oto reads from it. Position is converted to time via `pos / (sampleRate * channels * bitDepth)`. All Player methods are mutex-protected since Oto runs audio in its own goroutine. The Oto context is initialized once globally via `sync.Once`.

**Seeking** requires recreating the Oto player (no in-place seek support): pause current player, seek the decoder, reset the byte counter, create a new Oto player from the same countingReader. Byte positions must be aligned to 4-byte frame boundaries (stereo 16-bit). `Restart()` uses the same approach to seek to 0 for repeat mode, also resetting the done channel and monitor goroutine.

**Downloader** (`internal/downloader/`) — URL detection and yt-dlp integration. `Download()` runs yt-dlp as a subprocess with `--no-playlist` to extract audio as WAV into a temp file (WAV avoids yt-dlp's conversion overhead). Streams yt-dlp output via callback for progress display. Returns a cleanup func for temp file removal. `ExtractPlaylist()` uses `yt-dlp --flat-playlist` to extract video IDs and titles (up to 50 entries) from playlist/radio URLs without downloading. Returns nil for single-video URLs. `save.go` provides `SaveFile()` which converts downloaded WAV to MP3 via ffmpeg on demand (triggered by `s` key).

**Queue** (`internal/queue/`) — Manages ordered playlist tracks. `Track` struct holds ID, title, URL, file path, state (Pending → Downloading → Ready → Playing → Done/Failed), and a cleanup function for temp files. `Queue` provides `Current()`, `Next()`, `Advance()`, `Previous()`, `Peek(n)`, `Remove()`, and state mutation methods. Shuffle support uses a separate index mapping (`shuffleOrder[]`) so the underlying track array stays in original order — `EnableShuffle()` Fisher-Yates shuffles all indices except current (pinned at position 0), `DisableShuffle()` clears the mapping and resumes linear order. `AdvanceShuffle()`/`PreviousShuffle()`/`NextShuffled()` navigate the shuffle order. `Remove()` maintains the shuffle mapping when tracks are removed. Single-threaded, mutated only from Bubbletea's Update loop.

**TUI** (`internal/ui/`) — Standard Bubbletea Model pattern. A 200ms tick polls `player.Position()` to update the progress bar. A blocking goroutine waits on `player.Done()` channel to detect playback end. On repeat-one, playback end triggers `player.Restart()` and re-watches the new done channel instead of quitting. Repeat mode is defined in `repeat.go` with three modes: off, repeat-one, and repeat-all (loops entire playlist). Shuffle mode is defined in `shuffle.go` with on/off toggle — when active, `[shuffle]` appears in the status line. `skipToNext()` uses `findNextPlayable()` to scan forward in playback order (shuffle-aware), automatically skipping Failed tracks. When a track fails to open in `advanceToTrack()`, it's marked Failed and the next playable track is tried via `trackFailedMsg`. The View renders the "Option A" minimal layout with adaptive colors for light/dark terminals. `download.go` implements a separate Bubbletea model using bubbles (spinner + progress bar) for the yt-dlp download phase, showing phases (fetching/downloading/converting), download speed, size, and ETA.

**Playlist flow**: Two sources build queues: (1) URL playback — `Init()` fires a background `extractPlaylistCmd`, if 2+ entries are found a Queue is built with tracks in Pending state, downloaded one at a time ahead of playback. (2) Local file playback — `main.go` calls `scanAudioFiles()` to find sibling audio files in the same directory, builds a Queue with all tracks in Ready state. In both cases, on track end or `n` keypress, playback advances: the old player is closed, a new player is created from the next track, and for URL playlists the track after that starts downloading. `enter` jumps to any selected track (downloading on demand for URL playlists). `del`/`backspace` removes a track from the queue. Old temp files are cleaned up 2 tracks behind (only for URL downloads; local files have nil cleanup). The "Up Next" queue list wraps around to show all tracks (after current first, then before current). In visualizer mode, the queue is shown as a compact one-liner to reduce render overhead.

**Data flow**: `tea.KeyMsg` → player controls (pause/seek/volume) → next tick updates UI state from player. `tea.WindowSizeMsg` drives responsive progress bar width.

## Dependencies

- `ebitengine/oto/v3` — Cross-platform audio output (WASAPI on Windows, CoreAudio on macOS, ALSA on Linux)
- `hajimehoshi/go-mp3` — MP3 decoding
- `go-audio/wav` — WAV decoding
- `mewkiz/flac` — FLAC decoding
- `jfreymuth/oggvorbis` — OGG Vorbis decoding
- `bogem/id3v2/v2` — ID3 tag reading
- `charmbracelet/bubbletea` + `lipgloss` + `bubbles` — TUI framework, styling, and components (spinner, progress bar)

## Platform Notes

- **Windows**: Oto uses WASAPI, no external deps needed. This is the primary dev platform.
- **Linux**: Oto needs ALSA; building requires `libasound2-dev`.
- **macOS**: Oto uses Core Audio, no external deps.
- NEVER use `/dev/null`, `nul`, or `NUL` in bash commands on this Windows system — these create actual files and break git.

## Keybindings

Defined in `internal/ui/keys.go`. Vim-style alternatives (h/j/k/l) mirror arrow keys. `r` toggles repeat mode (off/song/playlist). `z` toggles shuffle mode (playlist only). `s` saves the current URL download as MP3 (only available during URL playback). When a playlist queue is active: `n` skips to next track (shuffle-aware, skips Failed tracks), `N`/`p` goes to previous track, `j`/`k` scroll the queue list, `enter` plays the selected track, `del`/`backspace` removes a track from the queue.

## External Tools

- **yt-dlp** — Optional runtime dependency for URL playback. Not a Go dependency; invoked as a subprocess. Downloads as WAV to avoid conversion overhead.
- **ffmpeg** — Optional runtime dependency for saving URL downloads as MP3 (triggered by `s` key).

## Known Issues

### Visualizer FPS drop with queue list visible

**Status**: Unsolved. All visualizers except Matrix drop to ~2-3 FPS when a playlist queue is present. Matrix runs smoothly. The issue does NOT occur during single-track playback.

**What was tried** (changes remain in `internal/ui/model.go`):

1. **Cached queue list view** — `rebuildQueueViewCache()` caches `list.Model.View()` output and pagination dots. Only rebuilt on discrete events (track download, queue navigation, window resize), not on every tick.
2. **Removed `syncQueueList()` from tickMsg** — Queue state only changes on discrete events, not every 200ms. Moved to specific handlers: `handleTrackDownloaded`, `handleQueuePlaybackEnd`, `skipToNext`, `skipToPrevious`, `handlePlaylistExtracted`, `advanceToTrack`.
3. **Cached entire View() output** — `View()` now returns `headerCache + vizCache + bottomCache` (three pre-built strings). `headerCache` rebuilds on track change, `bottomCache` rebuilds on tickMsg/discrete events, `vizCache` rebuilds on vizTickMsg. Eliminated all computation from View() itself.
4. **Compact queue in viz mode** — When visualizer is active, shows one-line "Up next: title (n/N)" instead of full queue list.
5. **strings.Builder in cache builders** — Replaced string concatenation with `strings.Builder`.
6. **Tried ASCII-only visualizer characters** — Replaced Unicode block chars (▁▂▃▄▅▆▇█, braille, ░▒▓) with ASCII equivalents. No improvement. Reverted.

**What was ruled out via debug logging**:

- `vizTickMsg` fires at correct 50ms intervals (20 FPS)
- `Update()` + `View()` take 0ms combined (sub-microsecond)
- Total View output is only ~1.5KB
- FFT processing, sample reads, and all cache rebuilds are sub-microsecond
- The bottleneck is NOT in application code

**Where the bottleneck likely is**:

The bottleneck is in Bubbletea's `standardRenderer.flush()` (bubbletea v1.3.10, `standard_renderer.go`). On each flush at 60fps, for every changed line the renderer calls `ansi.Truncate()` and `ansi.StringWidth()` which invoke `uniseg.FirstGraphemeClusterInString()` for every Unicode character. Matrix uses only ASCII (0-9, A-Z, space) which takes the fast `PrintAction` byte path. Other visualizers use Unicode (▁▂▃▄▅▆▇█, braille ⠀-⣿, ░▒▓) triggering expensive grapheme clustering. However, switching to ASCII chars didn't help either, suggesting the issue may be deeper in the Windows terminal write path or Bubbletea's line-diffing with the queue present (even as a single-line summary, the queue's existence changes the total line count and content enough to affect the diff).

**Possible next steps not yet tried**:

- Profile with Go's `pprof` to find the actual hot path
- Bypass Bubbletea's renderer entirely for the visualizer region (ANSI cursor writes from a separate goroutine)
- Use Bubbletea's `setIgnoredLines` (not publicly exposed) to mark visualizer lines
- Test on Linux/macOS to determine if this is Windows-specific terminal I/O
- Try Bubbletea v2 or a different TUI framework
- Reduce visualizer height (fewer changed lines per frame)
