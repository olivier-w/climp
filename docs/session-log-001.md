# Session 001 - Project Planning & Architecture

**Date**: 2026-01-31
**Goal**: Think through implementation details, create specs and plans

---

## User's Initial Prompt

> New project time
>
> lets take our time with this, because we need to make sure we do the right thing. the goal for this session is to think through implementation details, and create a structure of specs and plans that we can keep track off, pass context off to, to stay on track. im expecting a lot of questions from you, ask me anything you're not sure of
>
> GOAL: create a cli media player, with a beautiful tui interface.
>
> Motivation: mpv is a great cli player, but the user experience of installing it and dependencies to use it, the user interface (image attached as an example of what NOT to do), is very poor.
>
> Inspiration: there have been some great tui apps coming out recently:
> - claude code
> - opencode
> - charm crush
> - codex
> - the examples listed on https://ratatui.rs/
> - https://github.com/charmbracelet/bubbletea
>
> those are some goals i would like to get to some day, but for now we should keep it basic
>
> Suggestions: I want to learn, Rust, Go, Zig, so any of those would be great. but using a tui framework like ink, ratatui, or bubbletea will probably save a lot of headaches as a first cli app.
>
> or something else that i havent thought of.
>
> for features, lets list out some mvp ideas, followed by features that we could add later on:
>
> mvp
> - play a local audio file
> - show a progress bar similar to any media player (0:00 --x----- 4:20)
> - keyboard shortcut tips to pause, ff, rw, and exit
> - no need to install dependencies, just install one package, then run `climp song.mp3` and the song plays
>
> v1+
> - parses yt and soundcloud links, downloads and plays media
> - more media player options (repeat, playback speed, etc)
> - ascii visualizer (maybe)
>
> i know this is a lot, so the main goal for now is to write all of theses specs and plans, claude.md, readmes, so in future sessions, we can build out what we want without going over context limits
>
> please also save these prompts and replies somewhere, id like to keep them for a presentation

(User also attached a screenshot of mpv playing a SoundCloud link - showing raw codec text, file tags, ffmpeg demuxer warnings, and a plain text progress line. This is the UX we're improving on.)

---

## Questions Asked & Answers

### Round 1

**Q: Language/framework choice?**
A: **Go + Bubbletea** - Best for zero-dep install goal. Pure Go MP3 decoder, simplest distribution. User needs to install Go first.

**Q: Audio formats for MVP?**
A: **MP3 only** - Simplest MVP, cover the most common format first.

**Q: Distribution method?**
A: **Both** - GitHub releases for MVP, then add package managers (brew, scoop) in v1+.

**Q: Project name confirmed?**
A: **Yes, climp** - Works as: `climp song.mp3`

### Round 2

**Q: TUI layout style?**
A: **Show me mockups first** - User wants to see options before deciding.

**Q: Should climp use the Convex backend?**
A: **No** - `~/projects/climp` is an empty directory, the CLAUDE.md with Convex instructions was just a template. Pure standalone CLI.

**Q: YT/SoundCloud download approach for v1+?**
A: **yt-dlp is fine** - Bundle or require yt-dlp for URL parsing.

---

## Key Research Findings

### System State
- Windows machine
- Rust 1.92.0 installed, Go and Zig are NOT installed
- Node.js, Python, Git available
- `climp/` directory is completely empty

### Framework Comparison Summary

| | Go + Bubbletea | Rust + Ratatui | Zig |
|---|---|---|---|
| Zero-dep binary | Yes (pure Go MP3) | No (needs ALSA on Linux) | No (C lib wrappers) |
| Learning curve | Easiest | Steep (borrow checker) | Steepest |
| TUI maturity | Production-ready | Most mature | Young |
| Distribution | Trivial (GOOS/GOARCH) | Good (cross tool) | Good |

**Winner: Go + Bubbletea** - best match for zero-dependency goal and learning curve.

### Go Audio Stack
- `ebitengine/oto` - cross-platform audio output (WASAPI on Windows, Core Audio on macOS, ALSA on Linux)
- `hajimehoshi/go-mp3` - pure Go MP3 decoder, no C dependencies
- `bogem/id3v2` - ID3 tag reading for metadata

---

## Decisions Made

1. **Language**: Go + Bubbletea (charmbracelet ecosystem)
2. **Audio**: oto v3 + go-mp3 (pure Go, zero external deps)
3. **MVP scope**: MP3 only, local files only, minimal TUI
4. **Distribution**: GitHub releases first, then brew/scoop
5. **No backend**: Pure standalone CLI binary
6. **v1+ URL support**: yt-dlp (acceptable external dependency)
7. **TUI mockups**: 3 options provided, user to choose during implementation

---

## Architecture Summary

- Bubbletea Model/Update/View pattern
- Audio runs in separate goroutine (managed by Oto)
- `countingReader` wraps MP3 decoder for position tracking
- 200ms tick for UI updates (5 fps)
- Seeking requires recreating Oto player
- Mutex-protected player methods for thread safety

## Implementation Plan

5 sessions planned:
1. Project setup (install Go, scaffold)
2. Audio engine (player.go, metadata.go)
3. TUI interface (model.go, keys.go, styles.go, components.go)
4. Integration & polish
5. Build & distribution

See `C:\Users\hi\.claude\plans\effervescent-mixing-crystal.md` for full plan.
