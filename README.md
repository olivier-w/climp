# climp

minimal cli media player.

<img width="674" height="230" alt="image" src="https://github.com/user-attachments/assets/f9e49654-36d9-4518-830d-5cabf4c415fa" />

# file support
.mp3

## url support

Play audio from URLs using [yt-dlp](https://github.com/yt-dlp/yt-dlp):

```bash
climp https://youtube.com/watch?v=dQw4w9WgXcQ
```

Requires `yt-dlp` to be installed. climp will show install instructions if it's missing.

## install

```bash
go install github.com/olivier-w/climp@latest
```

or build from source:

```bash
git clone https://github.com/olivier-w/climp.git
cd climp
go build -o climp .
```

## usage

```bash
climp song.mp3
climp https://youtube.com/watch?v=...
```

## keybindings

| key | Action |
|-----|--------|
| space | toggle pause |
| left / h | seek -5s |
| right / l | seek +5s |
| up / k | volume +5% |
| down / j | volume -5% |
| r | toggle repeat |
| q / esc / ctrl+c | quit |

## license

[Apache-2.0](LICENSE)
