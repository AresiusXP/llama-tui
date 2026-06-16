package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	llamaTUIOwner = "patriciodanos"
	llamaTUIRepo  = "llama-tui"
	llamaCPPOwner = "ggml-org"
	llamaCPPRepo  = "llama.cpp"
	githubAPIBase = "https://api.github.com"
)

// Release holds GitHub release metadata.
type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Body       string  `json:"body"`
	HTMLURL    string  `json:"html_url"`
	Assets     []Asset `json:"assets"`
	PreRelease bool    `json:"prerelease"`
}

// Asset is a release binary attachment.
type Asset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// UpdateInfo reports whether a newer version is available.
type UpdateInfo struct {
	Available      bool
	CurrentVersion string
	LatestVersion  string
	DownloadURL    string
	ReleaseURL     string
}

// CheckAppUpdate checks GitHub for a newer llama-tui release.
// currentVersion is the running version string (e.g. "v0.1.0" or "dev").
// Returns UpdateInfo; Available=false if already up-to-date or on error.
func CheckAppUpdate(ctx context.Context, currentVersion string) UpdateInfo {
	if currentVersion == "dev" {
		return UpdateInfo{Available: false, CurrentVersion: currentVersion}
	}

	release, err := getLatestRelease(ctx, llamaTUIOwner, llamaTUIRepo)
	if err != nil {
		return UpdateInfo{Available: false, CurrentVersion: currentVersion}
	}

	info := UpdateInfo{
		CurrentVersion: currentVersion,
		LatestVersion:  release.TagName,
		ReleaseURL:     release.HTMLURL,
	}

	if release.TagName == currentVersion {
		return info
	}

	assetName := appAssetName(release.TagName)
	for _, a := range release.Assets {
		if a.Name == assetName {
			info.Available = true
			info.DownloadURL = a.BrowserDownloadURL
			break
		}
	}

	return info
}

// CheckLlamaServerUpdate checks ggml-org/llama.cpp for a newer release.
// currentBuildTag is the currently installed build tag (e.g. "b9667"). Empty = not installed.
func CheckLlamaServerUpdate(ctx context.Context, currentBuildTag string) UpdateInfo {
	release, err := getLatestRelease(ctx, llamaCPPOwner, llamaCPPRepo)
	if err != nil {
		return UpdateInfo{Available: false, CurrentVersion: currentBuildTag}
	}

	info := UpdateInfo{
		CurrentVersion: currentBuildTag,
		LatestVersion:  release.TagName,
		ReleaseURL:     release.HTMLURL,
	}

	if currentBuildTag != "" && release.TagName == currentBuildTag {
		return info
	}

	assetName := LlamaServerAssetName(release.TagName)
	for _, a := range release.Assets {
		if a.Name == assetName {
			info.Available = true
			info.DownloadURL = a.BrowserDownloadURL
			break
		}
	}

	return info
}

// LlamaServerAssetName returns the expected asset filename for the current platform.
// e.g. "llama-b9667-bin-macos-arm64.tar.gz"
func LlamaServerAssetName(tag string) string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return fmt.Sprintf("llama-%s-bin-macos-arm64.tar.gz", tag)
	case "darwin/amd64":
		return fmt.Sprintf("llama-%s-bin-macos-x64.tar.gz", tag)
	case "linux/arm64":
		return fmt.Sprintf("llama-%s-bin-ubuntu-arm64.tar.gz", tag)
	default:
		return fmt.Sprintf("llama-%s-bin-ubuntu-x64.tar.gz", tag)
	}
}

// DownloadLlamaServer downloads and extracts llama-server for the current platform
// from a llama.cpp GitHub release, placing the binary at destPath.
// Progress is sent on the returned channel (0.0 to 1.0 float64 values, then -1 for done, -2 for error).
func DownloadLlamaServer(ctx context.Context, release Release, destPath string) <-chan float64 {
	ch := make(chan float64, 16)

	go func() {
		defer close(ch)

		assetName := LlamaServerAssetName(release.TagName)
		var downloadURL string
		var totalSize int64
		for _, a := range release.Assets {
			if a.Name == assetName {
				downloadURL = a.BrowserDownloadURL
				totalSize = a.Size
				break
			}
		}
		if downloadURL == "" {
			ch <- -2.0
			return
		}

		tmpFile, err := os.CreateTemp("", "llama-server-*.tar.gz")
		if err != nil {
			ch <- -2.0
			return
		}
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if err != nil {
			ch <- -2.0
			return
		}

		client := &http.Client{Timeout: 0} // streaming download — no deadline on body
		resp, err := client.Do(req)
		if err != nil {
			ch <- -2.0
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			ch <- -2.0
			return
		}

		if totalSize == 0 && resp.ContentLength > 0 {
			totalSize = resp.ContentLength
		}

		written, err := copyWithProgress(ctx, tmpFile, resp.Body, totalSize, ch)
		_ = written
		if err != nil {
			ch <- -2.0
			return
		}

		if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
			ch <- -2.0
			return
		}

		// Extract all files from the archive into the bin directory.
		// llama-server is dynamically linked against several .dylib / .so files
		// that must live in the same directory as the binary.
		destDir := filepath.Dir(destPath)

		// Remove the existing bin directory first to ensure a clean install —
		// stale dylibs from a previous version can cause abort traps.
		if err := os.RemoveAll(destDir); err != nil {
			ch <- -2.0
			return
		}
		if err := os.MkdirAll(destDir, 0755); err != nil {
			ch <- -2.0
			return
		}

		if err := extractAllToDir(tmpFile, destDir); err != nil {
			ch <- -2.0
			return
		}

		ch <- -1.0
	}()

	return ch
}

// SelfUpdate downloads the latest llama-tui binary and replaces the running executable.
// It downloads to a temp file first, then does an atomic rename.
// Returns error if anything fails.
func SelfUpdate(ctx context.Context, info UpdateInfo) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable path: %w", err)
	}

	exeDir := filepath.Dir(currentExe)

	tmpArchive, err := os.CreateTemp(exeDir, "llama-tui-update-*.tar.gz")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	defer os.Remove(tmpArchive.Name())
	defer tmpArchive.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.DownloadURL, nil)
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpArchive, resp.Body); err != nil {
		return fmt.Errorf("could not write archive: %w", err)
	}

	if _, err := tmpArchive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("could not seek archive: %w", err)
	}

	newExePath := currentExe + ".new"
	if err := extractFromTarGz(tmpArchive, "llama-tui", newExePath); err != nil {
		return fmt.Errorf("could not extract binary: %w", err)
	}

	if err := os.Rename(newExePath, currentExe); err != nil {
		os.Remove(newExePath)
		return fmt.Errorf("could not replace binary: %w", err)
	}

	return nil
}

// GetLatestLlamaRelease returns the latest llama.cpp GitHub release.
func GetLatestLlamaRelease(ctx context.Context) (Release, error) {
	return getLatestRelease(ctx, llamaCPPOwner, llamaCPPRepo)
}

// GetRelease returns a specific release by tag from the given repo.
func GetRelease(ctx context.Context, owner, repo, tag string) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", githubAPIBase, owner, repo, tag)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, err
	}
	return release, nil
}

// ─── unexported helpers ───────────────────────────────────────────────────────

func getLatestRelease(ctx context.Context, owner, repo string) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIBase, owner, repo)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, err
	}
	return release, nil
}

// appAssetName returns the expected llama-tui asset filename for the current platform.
func appAssetName(tag string) string {
	// Strip leading 'v' for the filename component (e.g. v0.1.0 → 0.1.0)
	ver := strings.TrimPrefix(tag, "v")

	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return fmt.Sprintf("llama-tui_%s_darwin_arm64.tar.gz", ver)
	case "darwin/amd64":
		return fmt.Sprintf("llama-tui_%s_darwin_amd64.tar.gz", ver)
	case "linux/arm64":
		return fmt.Sprintf("llama-tui_%s_linux_arm64.tar.gz", ver)
	default:
		return fmt.Sprintf("llama-tui_%s_linux_amd64.tar.gz", ver)
	}
}

// copyWithProgress copies src → dst, sending progress fractions [0,1] to ch.
// It respects context cancellation, returning ctx.Err() if cancelled.
func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, total int64, ch chan<- float64) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64

	for {
		// Check for cancellation before each read.
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = fmt.Errorf("invalid write result")
				}
			}
			written += int64(nw)
			if ew != nil {
				return written, ew
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
			if total > 0 {
				select {
				case ch <- float64(written) / float64(total):
				default:
				}
			}
		}
		if er != nil {
			if er == io.EOF {
				break
			}
			return written, er
		}
	}
	return written, nil
}

// extractAllToDir extracts every regular file and symlink from a .tar.gz archive
// into destDir, stripping the top-level versioned directory prefix (e.g.
// "llama-b9670/llama-server" → destDir/llama-server).
//
// Symlinks are critical: the llama.cpp release archives ship dylibs/sos under
// fully-versioned names (libllama-common.0.0.9670.dylib) plus short SONAME
// symlinks (libllama-common.0.dylib) that the binary's @rpath/DT_NEEDED entries
// actually reference. Dropping the symlinks breaks dynamic linking and causes
// an abort trap (SIGABRT) at startup.
//
// Regular files get 0755 permissions so binaries and libraries are
// executable/loadable by the OS dynamic linker.
func extractAllToDir(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		// Strip the top-level directory prefix (e.g. "llama-b9670/").
		name := filepath.Base(hdr.Name)
		if name == "" || name == "." {
			continue
		}
		destPath := filepath.Join(destDir, name)

		switch hdr.Typeflag {
		case tar.TypeReg:
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return fmt.Errorf("create %s: %w", name, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("write %s: %w", name, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close %s: %w", name, err)
			}

		case tar.TypeSymlink:
			// These archives use relative, same-directory symlink targets
			// (e.g. libllama-common.0.dylib -> libllama-common.0.0.9670.dylib).
			// Strip any path component defensively to avoid traversal and to
			// keep the link valid inside our flattened destDir.
			linkTarget := filepath.Base(hdr.Linkname)
			if linkTarget == "" || linkTarget == "." {
				continue
			}
			// os.Symlink fails if the path already exists — remove first.
			_ = os.Remove(destPath)
			if err := os.Symlink(linkTarget, destPath); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", name, linkTarget, err)
			}

		default:
			// Skip directories, hard links, devices, etc.
			continue
		}
	}
	return nil
}

// extractFromTarGz reads a .tar.gz archive from r, finds the first entry whose
// base name matches targetName, and writes it to destPath with 0755 permissions.
func extractFromTarGz(r io.Reader, targetName, destPath string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != targetName {
			continue
		}

		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("create dest file: %w", err)
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return fmt.Errorf("write dest file: %w", err)
		}
		return out.Close()
	}

	return fmt.Errorf("binary %q not found in archive", targetName)
}
