# climp

A minimal CLI media player for MP3 files.

```
  climp

  Weightless - Marconi Union
  Ambient Works

  0:42 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 4:20

  ▶  playing                                vol 80%

  space pause  ←/→ seek  ↑/↓ volume  q quit
```

## Install

```bash
go install github.com/olivier-w/climp@latest
```

Or build from source:

```bash
git clone https://github.com/olivier-w/climp.git
cd climp
go build -o climp .
```

## Usage

```bash
climp song.mp3
```

## Keybindings

| Key | Action |
|-----|--------|
| Space | Toggle pause |
| Left / h | Seek -5s |
| Right / l | Seek +5s |
| Up / k | Volume +5% |
| Down / j | Volume -5% |
| q / Esc / Ctrl+C | Quit |

## License

[Apache-2.0](LICENSE)
