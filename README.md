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

Play audio from URLs using [yt-dlp](https://github.com/yt-dlp/yt-dlp):

```bash
climp https://youtube.com/watch?v=dQw4w9WgXcQ
```

Requires `yt-dlp` to be installed. climp will show install instructions if it's missing. Downloads as WAV for faster processing — press `s` during playback to save as MP3 (requires `ffmpeg`).
If yt-dlp shows no activity for 15 seconds, climp fails fast instead of hanging. Some live radio URLs may fail with a "live radio stream not supported yet" error.

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

For local playlist files, climp plays valid local media entries and `http(s)` URL entries. Invalid or unsupported entries are skipped. If no playable entries remain, playback fails with an error.

### youtube playlists

YouTube playlists and radio URLs are automatically detected. The first track starts playing immediately while the rest of the playlist is extracted in the background (up to 50 tracks). Upcoming tracks are downloaded one at a time ahead of playback.

```bash
climp https://youtube.com/playlist?list=PLxxxxxxxx
climp https://youtube.com/watch?v=xxx&list=RDxxx   # radio/mix
```

Use `n`/`p` to skip between tracks, `j`/`k` to scroll the queue, `enter` to jump to a selected track, and `del` to remove a track. Repeat mode (`r`) cycles through off, repeat song, and repeat playlist. Shuffle mode (`z`) randomizes playback order without reordering the queue — the current track stays put and the rest are shuffled. Works with repeat playlist to re-shuffle at the end of each cycle. Speed control (`x`) cycles through 1x, 2x, and 0.5x playback speed — pitch shifts proportionally.

![playlist demo](demo/playlist.gif)

## visualizer

Press `v` to cycle through audio-reactive visualizers: VU meter, spectrum, waterfall spectrogram, waveform, lissajous scope, braille, dense, matrix, and hatching.

![visualizer demo](demo/visualizer.gif)

## install

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


## usage

```bash
climp song.mp3
climp track.flac
climp my-playlist.m3u
climp https://youtube.com/watch?v=...
climp https://youtube.com/playlist?list=...
```

## keybindings

| key | Action |
|-----|--------|
| space | toggle pause |
| left / h | seek -5s |
| right / l | seek +5s |
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
| s | save as mp3 (url playback only) |
| q / esc / ctrl+c | quit |

## license

[Apache-2.0](LICENSE)
