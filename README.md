# climp

minimal cli media player.

![playback demo](demo/playback.gif)

# format support
.mp3, .wav, .flac, .ogg

## file browser

Run `climp` with no arguments to browse and select files interactively.

![file browser demo](demo/browser.gif)

## url support

Play audio from URLs using [yt-dlp](https://github.com/yt-dlp/yt-dlp):

```bash
climp https://youtube.com/watch?v=dQw4w9WgXcQ
```

Requires `yt-dlp` to be installed. climp will show install instructions if it's missing. Downloads as WAV for faster processing — press `s` during playback to save as MP3 (requires `ffmpeg`).

![url playback demo](demo/url-fixed.gif)

## playlist support

### local directory playlists

When playing a local file, climp automatically scans the directory for other supported audio files and builds a playlist. All files are sorted alphabetically and playback starts from the file you selected.

```bash
climp song.mp3   # plays all audio files in the same directory
```

### youtube playlists

YouTube playlists and radio URLs are automatically detected. The first track starts playing immediately while the rest of the playlist is extracted in the background (up to 50 tracks). Upcoming tracks are downloaded one at a time ahead of playback.

```bash
climp https://youtube.com/playlist?list=PLxxxxxxxx
climp https://youtube.com/watch?v=xxx&list=RDxxx   # radio/mix
```

Use `n`/`p` to skip between tracks, `j`/`k` to scroll the queue, `enter` to jump to a selected track, and `del` to remove a track. Repeat mode (`r`) cycles through off, repeat song, and repeat playlist. Shuffle mode (`z`) randomizes playback order without reordering the queue — the current track stays put and the rest are shuffled. Works with repeat playlist to re-shuffle at the end of each cycle.

![playlist demo](demo/playlist.gif)

## visualizer

Press `v` to cycle through audio-reactive visualizers: VU meter, spectrum, waveform, braille, dense, matrix, and hatching.

![visualizer demo](demo/visualizer.gif)

## install

### scoop (windows)

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

## usage

```bash
climp song.mp3
climp track.flac
climp https://youtube.com/watch?v=...
climp https://youtube.com/playlist?list=...
```

## keybindings

| key | Action |
|-----|--------|
| space | toggle pause |
| left / h | seek -5s |
| right / l | seek +5s |
| up / k | volume +5% |
| down / j | volume -5% |
| v | cycle visualizer (vu / spectrum / waveform / braille / dense / matrix / hatching / off) |
| r | toggle repeat (off / song / playlist) |
| z | toggle shuffle (playlist) |
| n | next track (playlist) |
| N / p | previous track (playlist) |
| j / k | scroll queue list (playlist) |
| enter | play selected track (playlist) |
| del / backspace | remove selected track (playlist) |
| s | save as mp3 (url playback only) |
| q / esc / ctrl+c | quit |

## license

[Apache-2.0](LICENSE)
