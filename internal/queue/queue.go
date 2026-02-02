package queue

// TrackState represents the download/playback state of a track.
type TrackState int

const (
	Pending     TrackState = iota
	Downloading
	Ready
	Playing
	Done
	Failed
)

// Track represents a single item in the playlist queue.
type Track struct {
	ID      string
	Title   string
	URL     string
	Path    string
	State   TrackState
	Cleanup func()
}

// Queue manages an ordered list of tracks for playlist playback.
// It is only mutated from Bubbletea's single-threaded Update loop.
type Queue struct {
	tracks  []Track
	current int
}

// New creates a Queue from the given tracks.
func New(tracks []Track) *Queue {
	return &Queue{tracks: tracks}
}

// Current returns a pointer to the currently playing track, or nil if empty.
func (q *Queue) Current() *Track {
	if q.current < 0 || q.current >= len(q.tracks) {
		return nil
	}
	return &q.tracks[q.current]
}

// Next returns a pointer to the next track, or nil if at the end.
func (q *Queue) Next() *Track {
	i := q.current + 1
	if i >= len(q.tracks) {
		return nil
	}
	return &q.tracks[i]
}

// Advance moves the current index forward by one. Returns false if already at end.
func (q *Queue) Advance() bool {
	if q.current+1 >= len(q.tracks) {
		return false
	}
	q.current++
	return true
}

// Previous moves the current index back by one. Returns false if already at start.
func (q *Queue) Previous() bool {
	if q.current <= 0 {
		return false
	}
	q.current--
	return true
}

// Peek returns up to n tracks after the current one.
func (q *Queue) Peek(n int) []Track {
	start := q.current + 1
	if start >= len(q.tracks) {
		return nil
	}
	end := start + n
	if end > len(q.tracks) {
		end = len(q.tracks)
	}
	result := make([]Track, end-start)
	copy(result, q.tracks[start:end])
	return result
}

// Len returns the total number of tracks.
func (q *Queue) Len() int {
	return len(q.tracks)
}

// CurrentIndex returns the zero-based index of the current track.
func (q *Queue) CurrentIndex() int {
	return q.current
}

// SetCurrentIndex sets the current track index directly.
func (q *Queue) SetCurrentIndex(i int) {
	if i >= 0 && i < len(q.tracks) {
		q.current = i
	}
}

// SetTrackState sets the state of the track at the given index.
func (q *Queue) SetTrackState(i int, state TrackState) {
	if i >= 0 && i < len(q.tracks) {
		q.tracks[i].State = state
	}
}

// SetTrackPath sets the file path of the track at the given index.
func (q *Queue) SetTrackPath(i int, path string) {
	if i >= 0 && i < len(q.tracks) {
		q.tracks[i].Path = path
	}
}

// SetTrackTitle sets the title of the track at the given index.
func (q *Queue) SetTrackTitle(i int, title string) {
	if i >= 0 && i < len(q.tracks) {
		q.tracks[i].Title = title
	}
}

// SetTrackCleanup sets the cleanup function for the track at the given index.
func (q *Queue) SetTrackCleanup(i int, cleanup func()) {
	if i >= 0 && i < len(q.tracks) {
		q.tracks[i].Cleanup = cleanup
	}
}

// Track returns a pointer to the track at the given index, or nil if out of range.
func (q *Queue) Track(i int) *Track {
	if i < 0 || i >= len(q.tracks) {
		return nil
	}
	return &q.tracks[i]
}

// CleanupAll calls the cleanup function on every track that has one.
func (q *Queue) CleanupAll() {
	for i := range q.tracks {
		if q.tracks[i].Cleanup != nil {
			q.tracks[i].Cleanup()
			q.tracks[i].Cleanup = nil
		}
	}
}
