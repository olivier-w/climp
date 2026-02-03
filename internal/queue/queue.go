package queue

import "math/rand"

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
	tracks       []Track
	current      int
	shuffleOrder []int // maps shuffle position → original track index
	shufflePos   int   // current position in shuffleOrder
	shuffled     bool
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

// Next returns a pointer to the next track in playback order, or nil if at the end.
// When shuffle is active, returns the next track in shuffle order.
func (q *Queue) Next() *Track {
	if q.shuffled {
		return q.NextShuffled()
	}
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
// Also syncs the shuffle position when shuffle mode is active.
func (q *Queue) SetCurrentIndex(i int) {
	if i >= 0 && i < len(q.tracks) {
		q.current = i
		q.SetShufflePosition(i)
	}
}

// WrapToStart positions the queue so that Next() returns track 0
// and Advance() moves to track 0. Used for RepeatAll wrap-around.
func (q *Queue) WrapToStart() {
	q.current = -1
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

// Remove removes the track at the given index. Cannot remove the currently playing track.
// Adjusts the current index if needed. Returns false if the index is invalid or is current.
func (q *Queue) Remove(i int) bool {
	if i < 0 || i >= len(q.tracks) || i == q.current {
		return false
	}
	// Run cleanup if present
	if q.tracks[i].Cleanup != nil {
		q.tracks[i].Cleanup()
	}
	q.tracks = append(q.tracks[:i], q.tracks[i+1:]...)
	if i < q.current {
		q.current--
	}
	if q.shuffled {
		q.rebuildShuffleAfterRemove(i)
	}
	return true
}

// rebuildShuffleAfterRemove rebuilds the shuffle mapping after a track at
// removedIdx has been spliced out. Filters the removed index, decrements
// indices above it, and derives shufflePos from where q.current lands.
func (q *Queue) rebuildShuffleAfterRemove(removedIdx int) {
	newOrder := make([]int, 0, len(q.shuffleOrder))
	newPos := 0
	for _, idx := range q.shuffleOrder {
		if idx == removedIdx {
			continue
		}
		adjusted := idx
		if idx > removedIdx {
			adjusted--
		}
		if adjusted == q.current {
			newPos = len(newOrder)
		}
		newOrder = append(newOrder, adjusted)
	}
	q.shuffleOrder = newOrder
	q.shufflePos = newPos
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

// IsShuffled returns whether shuffle mode is active.
func (q *Queue) IsShuffled() bool {
	return q.shuffled
}

// EnableShuffle activates shuffle mode. The current track stays at position 0
// in the shuffle order; all other indices are randomized via Fisher-Yates.
func (q *Queue) EnableShuffle() {
	n := len(q.tracks)
	if n <= 1 {
		return
	}
	q.shuffled = true
	q.shuffleOrder = make([]int, 0, n-1)
	for i := 0; i < n; i++ {
		if i != q.current {
			q.shuffleOrder = append(q.shuffleOrder, i)
		}
	}
	// Fisher-Yates shuffle
	for i := len(q.shuffleOrder) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		q.shuffleOrder[i], q.shuffleOrder[j] = q.shuffleOrder[j], q.shuffleOrder[i]
	}
	// Prepend current track at position 0
	q.shuffleOrder = append([]int{q.current}, q.shuffleOrder...)
	q.shufflePos = 0
}

// DisableShuffle deactivates shuffle mode, keeping the current track.
func (q *Queue) DisableShuffle() {
	q.shuffled = false
	q.shuffleOrder = nil
	q.shufflePos = 0
}

// NextShuffled returns the next track in shuffle order, or nil if at the end.
func (q *Queue) NextShuffled() *Track {
	if !q.shuffled || q.shufflePos+1 >= len(q.shuffleOrder) {
		return nil
	}
	idx := q.shuffleOrder[q.shufflePos+1]
	return q.Track(idx)
}

// AdvanceShuffle moves forward in shuffle order and updates current. Returns false if at end.
func (q *Queue) AdvanceShuffle() bool {
	if !q.shuffled || q.shufflePos+1 >= len(q.shuffleOrder) {
		return false
	}
	q.shufflePos++
	q.current = q.shuffleOrder[q.shufflePos]
	return true
}

// PreviousShuffle moves backward in shuffle order and updates current. Returns false if at start.
func (q *Queue) PreviousShuffle() bool {
	if !q.shuffled || q.shufflePos <= 0 {
		return false
	}
	q.shufflePos--
	q.current = q.shuffleOrder[q.shufflePos]
	return true
}

// NextDownloadIndex returns the original track index that should be downloaded next
// (the track after the current one in playback order). Returns -1 if none.
func (q *Queue) NextDownloadIndex() int {
	if q.shuffled {
		if q.shufflePos+1 < len(q.shuffleOrder) {
			return q.shuffleOrder[q.shufflePos+1]
		}
		return -1
	}
	next := q.current + 1
	if next >= len(q.tracks) {
		return -1
	}
	return next
}

// SetShufflePosition syncs shufflePos when the user jumps to a specific original track index.
func (q *Queue) SetShufflePosition(originalIdx int) {
	if !q.shuffled {
		return
	}
	for i, idx := range q.shuffleOrder {
		if idx == originalIdx {
			q.shufflePos = i
			return
		}
	}
}
