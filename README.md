# climp

Minimal CLI media player for local files, URLs, and playlists.

![playback demo](demo/playback.gif)

## Table of contents

- [Install](#install)
- [Usage](#usage)
- [Keybindings](#keybindings)
- [Format support](#format-support)
- [File browser](#file-browser)
- [URL support](#url-support)
- [Playlist support](#playlist-support)
- [Visualizer](#visualizer)
- [Install troubleshooting](#install-troubleshooting)
- [License](#license)

## Install

### With [`go`](https://go.dev/dl/)

```bash
go install github.com/olivier-w/climp@latest
```

### Download [prebuilt binaries](https://github.com/olivier-w/climp/releases) for:
- Linux [`amd64`](https://github.com/olivier-w/climp/releases/download/v0.2.3/climp_v0.2.3_linux_amd64.tar.gz), [`arm64`](https://github.com/olivier-w/climp/releases/download/v0.2.3/climp_v0.2.3_linux_arm64.tar.gz)
- macOS [`amd64` (intel)](https://github.com/olivier-w/climp/releases/download/v0.2.3/climp_v0.2.3_darwin_amd64.tar.gz), [`arm64` (m1,m2,m3,m4,m5)](https://github.com/olivier-w/climp/releases/download/v0.2.3/climp_v0.2.3_darwin_arm64.tar.gz)
- Windows [`amd64`](https://github.com/olivier-w/climp/releases/download/v0.2.3/climp_0.2.3_windows_amd64.zip)

### Windows [scoop](https://scoop.sh/)

```bash
scoop bucket add climp https://github.com/olivier-w/scoop-bucket
scoop install climp
```

### Build from source

```bash
git clone https://github.com/olivier-w/climp.git
cd climp
go build -o climp .
```

## Usage

```bash
climp
climp -h
climp --help
climp -v
climp --version
climp song.mp3
climp track.flac
climp my-playlist.m3u
climp https://youtube.com/watch?v=...
climp https://youtube.com/playlist?list=...
climp https://example.com/station.m3u8
```

`climp` with no arguments opens the file browser. `-h` / `--help` print startup usage, and `-v` / `--version` print the binary version and exit. Release binaries print the release tag, while dev builds print a tag-derived `-dev` version when run from a git checkout and fall back to `dev` otherwise.

## Keybindings

| key | action |
|-----|--------|
| `space` | toggle pause |
| `left / h` | seek -5s (disabled for live streams) |
| `right / l` | seek +5s (disabled for live streams) |
| `+ / =` | volume +5% |
| `-` | volume -5% |
| `v` | cycle visualizer (vu / spectrum / waterfall / waveform / lissajous / braille / dense / matrix / hatching / off) |
| `r` | cycle repeat mode (off / song / playlist) |
| `x` | cycle speed (1x / 2x / 0.5x) |
| `z` | toggle shuffle (playlist) |
| `n` | next track (playlist) |
| `N / p` | previous track (playlist) |
| `up / down / j / k` | move queue selection (playlist) |
| `enter` | play selected track (playlist) |
| `del / backspace` | remove selected track (playlist) |
| `s` | save as MP3 (downloaded URL tracks only; disabled for live streams) |
| `?` | toggle expanded help |
| `q / esc / ctrl+c` | quit |

## Format support

- audio: `.mp3`, `.wav`, `.flac`, `.ogg`
- playlists: `.m3u`, `.m3u8`, `.pls`

## File browser

Run `climp` with no arguments to browse and select files interactively.

![file browser demo](demo/browser.gif)

## URL support

Play audio from URLs with probe-based routing:

- finite media downloads use [yt-dlp](https://github.com/yt-dlp/yt-dlp)
- live streams use `ffmpeg` (`ffmpeg -i <url> -> s16le PCM`)
- remote playlist wrappers (`.pls`, `.m3u`, `.m3u8`) are expanded into queue entries

```bash
climp https://youtube.com/watch?v=dQw4w9WgXcQ
```
![url playback demo](demo/url-fixed.gif)

Requirements:

- `yt-dlp` is required for finite URL playback and YouTube sources
- `ffmpeg` is required for live URL playback
- `ffmpeg` is also required for `s` (save as MP3) on downloaded URL tracks

Behavior notes:

- finite URL downloads use WAV temp files for fast processing
- if `yt-dlp` reports no progress for 15 seconds, climp exits instead of hanging
- live streams are non-seekable

Live URL examples:

```bash
climp https://example.com/station.m3u8
climp https://example.com/station.m3u
climp https://example.com/stream.aac
climp https://example.com/stream.mp3
climp https://example.com/stream.ogg
```

## Playlist support

### Local directory playlists

When you open a local audio file, climp scans the same directory for supported audio files, sorts them alphabetically, and starts playback at the selected file.

```bash
climp song.mp3
```

### Local playlist files

climp opens local playlist files directly:

```bash
climp my-playlist.m3u
climp my-playlist.m3u8
climp my-playlist.pls
```

For local playlist files, climp plays valid local media entries and `http(s)` URL entries. URL entries are probe-routed the same way as direct URL playback. Remote playlist URL entries (`.pls`, `.m3u`, `.m3u8`) are expanded inline in file order. Invalid or unsupported entries are skipped. If no playable entries remain, playback fails with an error.

### YouTube playlists

YouTube playlist and radio URLs are auto-detected. The first track starts immediately while the rest of the playlist is extracted in the background (up to 50 tracks). Upcoming tracks are downloaded one at a time ahead of playback.

```bash
climp https://youtube.com/playlist?list=PLxxxxxxxx
climp https://youtube.com/watch?v=xxx&list=RDxxx
```

![playlist demo](demo/playlist.gif)

## Visualizer

Press `v` to cycle visualizers: VU meter, spectrum, waterfall spectrogram, waveform, lissajous scope, braille, dense, matrix, hatching, and off.

![visualizer demo](demo/visualizer.gif)

## Install Troubleshooting

### macOS

If `yt-dlp` is installed with `pip`, Python certificate verification can fail for some setups. Install certificates (replace `x` with your Python version):

```bash
/Applications/Python\ 3.xx.xx/Install\ Certificates.command
```

### Linux troubleshooting (headless/vm)

If playback fails with ALSA errors such as `Unknown PCM default` or `cannot find card '0'`, the machine has no usable default audio output device. This is common on headless VMs and containers.

Check detected devices:

```bash
aplay -l
aplay -L
```

Install or enable an audio stack (ALSA, PipeWire, or PulseAudio), or run in a session with audio output available.

## license

[Apache-2.0](LICENSE)
