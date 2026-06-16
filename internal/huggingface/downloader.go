package huggingface

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DownloadProgress reports progress of a download.
type DownloadProgress struct {
	RepoID     string
	Filename   string
	BytesDone  int64
	BytesTotal int64
	Done       bool
	Err        error
}

// Percent returns download percentage 0-100.
func (p DownloadProgress) Percent() float64 {
	if p.BytesTotal <= 0 {
		return 0
	}
	pct := float64(p.BytesDone) / float64(p.BytesTotal) * 100
	if pct > 100 {
		return 100
	}
	return pct
}

// DownloadFile downloads a GGUF file from HuggingFace to destDir.
// It sends progress updates on the returned channel.
// The channel is closed when the download is complete or errored.
// Supports resumable downloads via Content-Range (HTTP 206).
// token may be empty for public models.
func DownloadFile(ctx context.Context, repoID, filename, destDir, token string) <-chan DownloadProgress {
	ch := make(chan DownloadProgress, 16)

	go func() {
		defer close(ch)

		send := func(p DownloadProgress) {
			p.RepoID = repoID
			p.Filename = filename
			select {
			case ch <- p:
			case <-ctx.Done():
			}
		}

		destPath := filepath.Join(destDir, filepath.Base(filename))

		// Check existing file size for resume support.
		var existingSize int64
		if info, err := os.Stat(destPath); err == nil {
			existingSize = info.Size()
		}

		rawURL := DownloadURL(repoID, filename)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			send(DownloadProgress{Err: fmt.Errorf("huggingface: download: create request: %w", err)})
			return
		}

		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		if existingSize > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
		}

		client := &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: 30 * time.Second,
				}).DialContext,
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			send(DownloadProgress{Err: fmt.Errorf("huggingface: download: request failed: %w", err)})
			return
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusRequestedRangeNotSatisfiable:
			// 416: file already complete.
			send(DownloadProgress{BytesDone: existingSize, BytesTotal: existingSize, Done: true})
			return

		case http.StatusPartialContent:
			// 206: resume from existingSize.
			var totalSize int64
			cr := resp.Header.Get("Content-Range")
			if cr != "" {
				// Format: "bytes start-end/total"
				if idx := strings.LastIndex(cr, "/"); idx >= 0 {
					if t, err := strconv.ParseInt(cr[idx+1:], 10, 64); err == nil {
						totalSize = t
					}
				}
			}
			if totalSize == 0 {
				// Fallback: existingSize + Content-Length
				if cl, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil {
					totalSize = existingSize + cl
				}
			}

			f, err := os.OpenFile(destPath, os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				send(DownloadProgress{Err: fmt.Errorf("huggingface: download: open file for append: %w", err)})
				return
			}
			defer f.Close()

			copyChunked(ctx, ch, send, f, resp.Body, repoID, filename, existingSize, totalSize)

		case http.StatusOK:
			// 200: full download, overwrite.
			var totalSize int64
			if cl, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil {
				totalSize = cl
			}

			f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				send(DownloadProgress{Err: fmt.Errorf("huggingface: download: create file: %w", err)})
				return
			}
			defer f.Close()

			copyChunked(ctx, ch, send, f, resp.Body, repoID, filename, 0, totalSize)

		default:
			send(DownloadProgress{Err: fmt.Errorf("huggingface: download: unexpected status %d", resp.StatusCode)})
		}
	}()

	return ch
}

const chunkSize = 32 * 1024 // 32KB

// copyChunked reads body in chunkSize chunks, writes to f, and sends progress on ch.
func copyChunked(
	ctx context.Context,
	ch chan<- DownloadProgress,
	send func(DownloadProgress),
	f *os.File,
	body io.Reader,
	repoID, filename string,
	initialDone, totalSize int64,
) {
	buf := make([]byte, chunkSize)
	done := initialDone

	for {
		select {
		case <-ctx.Done():
			send(DownloadProgress{Err: ctx.Err()})
			return
		default:
		}

		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				send(DownloadProgress{Err: fmt.Errorf("huggingface: download: write: %w", writeErr)})
				return
			}
			done += int64(n)
			send(DownloadProgress{
				BytesDone:  done,
				BytesTotal: totalSize,
			})
		}

		if readErr == io.EOF {
			send(DownloadProgress{BytesDone: done, BytesTotal: totalSize, Done: true})
			return
		}
		if readErr != nil {
			send(DownloadProgress{Err: fmt.Errorf("huggingface: download: read: %w", readErr)})
			return
		}
	}
}
