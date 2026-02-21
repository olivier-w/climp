# climp

minimal cli media player.

![playback demo](demo/playback.gif)

# format support
.mp3, .wav, .flac, .ogg

### playlists
.m3u, .m3u8, .pls
## file browser

Run `climp` with no arguments to browse and select files interactively.

![file browser demo](demo/browser.gif)

## url support

Play audio from URLs using [yt-dlp](https://github.com/yt-dlp/yt-dlp) (finite downloads) and `ffmpeg` (live streams):

```bash
climp https://youtube.com/watch?v=dQw4w9WgXcQ
```

Requires `yt-dlp` to be installed. climp will show install instructions if it's missing. Downloads as WAV for faster processing — press `s` during playback to save as MP3 (requires `ffmpeg`).
climp probes URL responses before routing playback. It detects remote playlist wrappers (`.pls`, `.m3u`, `.m3u8`), live streams (HLS and ICY/Icecast-style audio, including many `.mp3`, `.ogg`, and no-extension stream endpoints), and finite media downloads. Live URLs use `ffmpeg` (`ffmpeg -i <url> -> PCM`), while finite URLs use `yt-dlp`.
Live playback requires `ffmpeg` to be installed.
If yt-dlp shows no activity for 15 seconds, climp fails fast instead of hanging.

Live URL examples:

```bash
climp https://example.com/station.m3u8
climp https://example.com/station.m3u
climp https://example.com/stream.aac
climp https://example.com/stream.mp3
climp https://example.com/stream.ogg
```

![url playback demo](demo/url-fixed.gif)

## playlist support

### local directory playlists

When playing a local file, climp automatically scans the directory for other supported audio files and builds a playlist. All files are sorted alphabetically and playback starts from the file you selected.

```bash
climp song.mp3   # plays all audio files in the same directory
```

### local playlist files

climp can open local playlist files directly:

```bash
climp my-playlist.m3u
climp my-playlist.m3u8
climp my-playlist.pls
```

For local playlist files, climp plays valid local media entries and `http(s)` URL entries. URL entries are probe-routed the same way as direct URL playback. Remote playlist URL entries (`.pls`/`.m3u`/`.m3u8` wrappers) are expanded inline into queue entries in file order. Invalid or unsupported entries are skipped. If no playable entries remain, playback fails with an error.

### youtube playlists

YouTube playlists and radio URLs are automatically detected. The first track starts playing immediately while the rest of the playlist is extracted in the background (up to 50 tracks). Upcoming tracks are downloaded one at a time ahead of playback.

```bash
climp https://youtube.com/playlist?list=PLxxxxxxxx
climp https://youtube.com/watch?v=xxx&list=RDxxx   # radio/mix
```

Use `n`/`p` to skip between tracks, `j`/`k` to scroll the queue, `enter` to jump to a selected track, and `del` to remove a track. Repeat mode (`r`) cycles through off, repeat song, and repeat playlist. Shuffle mode (`z`) randomizes playback order without reordering the queue — the current track stays put and the rest are shuffled. Works with repeat playlist to re-shuffle at the end of each cycle. Speed control (`x`) cycles through 1x, 2x, and 0.5x playback speed — pitch shifts proportionally.

When playlist mode is active, the header shows a playlist label (`Playlist: ...`) so you can quickly tell what queue you are in.

![playlist demo](demo/playlist.gif)

## visualizer

Press `v` to cycle through audio-reactive visualizers: VU meter, spectrum, waterfall spectrogram, waveform, lissajous scope, braille, dense, matrix, and hatching.

![visualizer demo](demo/visualizer.gif)

## install

Download prebuilt binaries from [GitHub Releases](https://github.com/olivier-w/climp/releases): linux (amd64/arm64), macos (amd64/arm64), windows (amd64).

### windows

```powershell
scoop bucket add climp https://github.com/olivier-w/scoop-bucket
scoop install climp
```

### go install

```bash
go install github.com/olivier-w/climp@latest
```

### build from source

```bash
git clone https://github.com/olivier-w/climp.git
cd climp
go build -o climp .
```

### macos
if you want `climp` to play youtube tracks, while installing `yt-dlp` with `pip`, `ytp-dlp` will fail due to python not being able to verify SSL connections. To fix it, you can install the `certifi` package (where x is your version of Python):

```bash
/Applications/Python\ 3.xx.xx/Install\ Certificates.command
```

### linux troubleshooting (headless/VM)

If playback fails with ALSA errors like `Unknown PCM default` or `cannot find card '0'`, the machine has no usable default audio output device. This is common on headless VMs/containers.

Check detected devices:

```bash
aplay -l
aplay -L
```

Install/enable an audio stack (ALSA/PipeWire/PulseAudio) or run on a machine/session with audio output available.


## usage

```bash
climp song.mp3
climp track.flac
climp my-playlist.m3u
climp https://youtube.com/watch?v=...
climp https://youtube.com/playlist?list=...
climp https://example.com/station.m3u8
```

## keybindings

| key | Action |
|-----|--------|
| space | toggle pause |
| left / h | seek -5s (disabled for live streams) |
| right / l | seek +5s (disabled for live streams) |
| + | volume +5% |
| - | volume -5% |
| v | cycle visualizer (vu / spectrum / waterfall / waveform / lissajous / braille / dense / matrix / hatching / off) |
| r | toggle repeat (off / song / playlist) |
| x | cycle speed (1x / 2x / 0.5x) |
| z | toggle shuffle (playlist) |
| n | next track (playlist) |
| N / p | previous track (playlist) |
| up / down | scroll queue list (playlist) |
| enter | play selected track (playlist) |
| del / backspace | remove selected track (playlist) |
| s | save as mp3 (downloaded URL playback only; disabled for live streams) |
| q / esc / ctrl+c | quit |

## license

[Apache-2.0](LICENSE)
