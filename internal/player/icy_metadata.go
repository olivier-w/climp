package player

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const icyMetadataHeaderTimeout = 4 * time.Second

var icyMetadataHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DisableCompression:    true,
		ResponseHeaderTimeout: icyMetadataHeaderTimeout,
	},
}

type icyTitleWatcher struct {
	ctx       context.Context
	cancel    context.CancelFunc
	body      io.ReadCloser
	updates   chan string
	done      chan struct{}
	closeOnce sync.Once
}

func newICYTitleWatcher(rawURL string) (*icyTitleWatcher, error) {
	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", "climp")

	resp, err := icyMetadataHTTPClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}

	metaInt, err := parseICYMetaInt(resp.Header.Get("icy-metaint"))
	if err != nil {
		resp.Body.Close()
		cancel()
		return nil, err
	}

	w := &icyTitleWatcher{
		ctx:     ctx,
		cancel:  cancel,
		body:    resp.Body,
		updates: make(chan string, 1),
		done:    make(chan struct{}),
	}
	go w.run(metaInt)
	return w, nil
}

func (w *icyTitleWatcher) Updates() <-chan string {
	if w == nil {
		return nil
	}
	return w.updates
}

func (w *icyTitleWatcher) Close() error {
	var err error
	if w == nil {
		return nil
	}
	w.closeOnce.Do(func() {
		w.cancel()
		if w.body != nil {
			err = w.body.Close()
		}
		<-w.done
	})
	return err
}

func (w *icyTitleWatcher) run(metaInt int) {
	defer close(w.done)
	defer close(w.updates)
	defer w.body.Close()

	var lastTitle string
	for {
		if err := discardICYAudio(w.body, metaInt); err != nil {
			return
		}

		var metaLen [1]byte
		if _, err := io.ReadFull(w.body, metaLen[:]); err != nil {
			return
		}

		size := int(metaLen[0]) * 16
		if size == 0 {
			continue
		}

		block := make([]byte, size)
		if _, err := io.ReadFull(w.body, block); err != nil {
			return
		}

		title := extractICYStreamTitle(block)
		if title == "" || title == lastTitle {
			continue
		}
		lastTitle = title

		select {
		case w.updates <- title:
		case <-w.ctx.Done():
			return
		}
	}
}

func parseICYMetaInt(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("icy metadata not available")
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid icy-metaint")
	}
	return n, nil
}

func discardICYAudio(r io.Reader, n int) error {
	_, err := io.CopyN(io.Discard, r, int64(n))
	return err
}

func extractICYStreamTitle(block []byte) string {
	raw := strings.TrimRight(string(block), "\x00")
	if raw == "" {
		return ""
	}

	lower := strings.ToLower(raw)
	const marker = "streamtitle='"
	start := strings.Index(lower, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)

	end := strings.Index(raw[start:], "'")
	if end < 0 {
		return ""
	}

	title := strings.TrimSpace(raw[start : start+end])
	if title == "" {
		return ""
	}
	return title
}
