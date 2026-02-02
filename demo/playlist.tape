# YouTube Playlist Demo
# Demonstrates: playlist extraction, queue navigation, track skipping, saving.
# Requires: yt-dlp and ffmpeg installed.

Output demo/playlist.gif

Set Shell "powershell"
Set FontSize 20
Set Width 1200
Set Height 900
Set Margin 20
Set MarginFill "#FF8C00"
Set BorderRadius 10
Set Theme "TokyoNight"
Set Padding 20
Set TypingSpeed 50ms

Sleep 500ms
Type "climp 'https://www.youtube.com/watch?v=wDuoOapZ9Z0&list=RDwDuoOapZ9Z0'"
Enter
Sleep 12s

# Let it play so the queue is visible
Sleep 3s

# Scroll queue list down/up
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms
Type "k"
Sleep 500ms

# Skip to next track
Type "n"
Sleep 5s

# Go back to previous track
Type@0ms "N"
Sleep 5s

# Jump to selected track
Enter
Sleep 5s

# Remove a track from queue
Backspace
Sleep 1s

# Save current track as MP3
Type "s"
Sleep 5s
