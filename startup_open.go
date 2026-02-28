package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/media"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/queue"
	"github.com/olivier-w/climp/internal/ui"
)

func buildPlaybackModel(arg string, downloadURL urlDownloadFunc) (ui.Model, error) {
	var playlistEntries []media.PlaylistEntry
	playlistStartIdx := -1
	var playlistStartCleanup func()
	var playlistSourcePath string
	playlistName := ""
	metaSet := false

	var path string
	var sourcePath string
	var originalURL string
	var meta player.Metadata
	var p *player.Player
	var cleanup func()

	if downloader.IsURL(arg) {
		route, err := downloader.ResolveURLRoute(arg)
		if err != nil {
			route = downloader.URLRouteResult{
				Kind:     downloader.RouteFiniteDownload,
				FinalURL: arg,
			}
		}
		if route.FinalURL == "" {
			route.FinalURL = arg
		}
		if route.Kind == downloader.RouteRemotePlaylist {
			playlistName = playlistNameFromURL(arg)
			playlistEntries = expandRemotePlaylistEntries(route.Playlist, maxRemotePlaylistDepth)
			if len(playlistEntries) == 0 {
				return ui.Model{}, fmt.Errorf("playlist contains no playable entries")
			}
		} else {
			openedLive := false
			if route.Kind == downloader.RouteLiveStream {
				var err error
				p, err = player.NewStream(route.FinalURL)
				if err == nil {
					openedLive = true
					meta = player.Metadata{Title: route.FinalURL}
					metaSet = true
				}
			}
			if !openedLive {
				result, err := downloadURL(route.FinalURL)
				if err != nil {
					return ui.Model{}, err
				}
				if result.Err != nil {
					if result.Cleanup != nil {
						result.Cleanup()
					}
					return ui.Model{}, result.Err
				}
				path = result.Path
				sourcePath = result.Path
				originalURL = arg
				cleanup = result.Cleanup

				if result.Title != "" {
					meta = player.Metadata{Title: result.Title}
				} else {
					meta = player.ReadMetadata(path)
				}
				metaSet = true
			}
		}
	} else {
		path = arg

		info, err := os.Stat(path)
		if err != nil {
			return ui.Model{}, err
		}
		if info.IsDir() {
			return ui.Model{}, fmt.Errorf("%s is a directory", path)
		}

		ext := strings.ToLower(filepath.Ext(path))
		if media.IsPlaylistExt(ext) {
			var err error
			playlistName = playlistNameFromFile(path)
			entries, err := media.ParseLocalPlaylist(path)
			if err != nil {
				return ui.Model{}, err
			}
			playlistEntries, _ = media.FilterPlayablePlaylistEntries(entries)
			playlistEntries = expandRemotePlaylistEntries(playlistEntries, maxRemotePlaylistDepth)
			if len(playlistEntries) == 0 {
				return ui.Model{}, fmt.Errorf("playlist contains no playable entries")
			}
		} else if !media.IsSupportedExt(ext) {
			return ui.Model{}, fmt.Errorf("unsupported format %s (supported: %s)", ext, media.SupportedExtsList())
		}
	}

	if len(playlistEntries) > 0 {
		var err error
		var start playlistStart
		playlistEntries, start, err = openFirstPlayablePlaylistEntry(playlistEntries, downloadURL)
		if err != nil {
			return ui.Model{}, err
		}
		playlistStartIdx = start.startIdx
		playlistStartCleanup = start.cleanup
		playlistSourcePath = start.sourcePath
		if start.path != "" {
			path = start.path
		}
		if start.player != nil {
			p = start.player
			path = ""
		}
		if start.metaSet {
			meta = start.meta
			metaSet = true
		}
	}

	if !metaSet {
		meta = player.ReadMetadata(path)
	}

	if p == nil {
		var err error
		p, err = player.New(path)
		if err != nil {
			if cleanup != nil {
				cleanup()
			}
			if playlistStartCleanup != nil {
				playlistStartCleanup()
			}
			return ui.Model{}, fmt.Errorf("error creating player: %w", err)
		}
	}

	if len(playlistEntries) > 0 {
		tracks := make([]queue.Track, len(playlistEntries))
		for i, e := range playlistEntries {
			title := e.Title
			if title == "" && e.Path != "" {
				title = strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path))
			}
			if title == "" && e.URL != "" {
				title = e.URL
			}

			tracks[i] = queue.Track{
				Title: title,
				URL:   e.URL,
				Path:  e.Path,
			}
			if e.URL != "" && e.Path == "" && !downloader.IsLiveURL(e.URL) {
				tracks[i].State = queue.Pending
			} else {
				tracks[i].State = queue.Ready
			}
		}
		tracks[playlistStartIdx].State = queue.Playing
		if playlistStartCleanup != nil {
			tracks[playlistStartIdx].Cleanup = playlistStartCleanup
		}
		q := queue.New(tracks)
		q.SetCurrentIndex(playlistStartIdx)
		return ui.NewWithQueue(p, meta, playlistSourcePath, q, playlistName), nil
	}

	if downloader.IsURL(arg) {
		return ui.New(p, meta, sourcePath, originalURL, cleanup), nil
	}

	if siblings := scanAudioFiles(path); siblings != nil {
		playlistName = playlistNameFromDirectoryOfFile(path)
		tracks := make([]queue.Track, len(siblings))
		var startIdx int
		absPath, _ := filepath.Abs(path)
		for i, f := range siblings {
			tracks[i] = queue.Track{
				Title: strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)),
				Path:  f,
				State: queue.Ready,
			}
			if f == absPath {
				startIdx = i
			}
		}
		tracks[startIdx].State = queue.Playing
		q := queue.New(tracks)
		q.SetCurrentIndex(startIdx)
		return ui.NewWithQueue(p, meta, "", q, playlistName), nil
	}

	return ui.New(p, meta, "", "", nil), nil
}
