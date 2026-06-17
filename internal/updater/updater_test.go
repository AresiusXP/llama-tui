package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ─── LlamaServerAssetName ────────────────────────────────────────────────────

func TestLlamaServerAssetName_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only")
	}
	tag := "b9667"
	name := LlamaServerAssetName(tag, "")
	if !strings.Contains(name, "macos") {
		t.Errorf("expected macos in asset name, got %q", name)
	}
	if !strings.Contains(name, tag) {
		t.Errorf("expected tag %q in asset name, got %q", tag, name)
	}
}

func TestLlamaServerAssetName_LinuxVulkan(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	name := LlamaServerAssetName("b1234", "vulkan")
	if !strings.Contains(name, "vulkan") {
		t.Errorf("expected 'vulkan' in asset name, got %q", name)
	}
}

func TestLlamaServerAssetName_LinuxCPU(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	name := LlamaServerAssetName("b1234", "")
	if strings.Contains(name, "vulkan") {
		t.Errorf("did not expect 'vulkan' in CPU asset name, got %q", name)
	}
}

// ─── appAssetName ────────────────────────────────────────────────────────────

func TestAppAssetName_StripsLeadingV(t *testing.T) {
	name := appAssetName("v1.2.3")
	if strings.Contains(name, "v1.2.3") {
		t.Errorf("expected leading 'v' to be stripped, got %q", name)
	}
	if !strings.Contains(name, "1.2.3") {
		t.Errorf("expected version '1.2.3' in asset name, got %q", name)
	}
}

func TestAppAssetName_ContainsPlatform(t *testing.T) {
	name := appAssetName("v0.5.0")
	if !strings.Contains(name, runtime.GOOS) {
		t.Errorf("expected OS %q in asset name, got %q", runtime.GOOS, name)
	}
}

// ─── copyWithProgress ────────────────────────────────────────────────────────

func TestCopyWithProgress_CopiesData(t *testing.T) {
	data := []byte("hello world test data")
	src := bytes.NewReader(data)
	var dst bytes.Buffer
	ch := make(chan float64, 32)

	n, err := copyWithProgress(context.Background(), &dst, src, int64(len(data)), ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("expected %d bytes written, got %d", len(data), n)
	}
	if dst.String() != string(data) {
		t.Fatalf("data mismatch: got %q", dst.String())
	}
}

func TestCopyWithProgress_SendsProgressValues(t *testing.T) {
	data := make([]byte, 100*1024) // 100KB to ensure multiple progress updates
	for i := range data {
		data[i] = byte(i % 256)
	}
	src := bytes.NewReader(data)
	var dst bytes.Buffer
	ch := make(chan float64, 128)

	_, err := copyWithProgress(context.Background(), &dst, src, int64(len(data)), ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain channel and verify progress values are in [0, 1].
	for {
		select {
		case v := <-ch:
			if v < 0 || v > 1 {
				t.Errorf("progress value out of range: %f", v)
			}
		default:
			goto done
		}
	}
done:
}

func TestCopyWithProgress_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	data := []byte("some data")
	src := bytes.NewReader(data)
	var dst bytes.Buffer
	ch := make(chan float64, 8)

	_, err := copyWithProgress(ctx, &dst, src, int64(len(data)), ch)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

// ─── extractFromTarGz ────────────────────────────────────────────────────────

func makeTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		b := []byte(content)
		tw.WriteHeader(&tar.Header{
			Name:     name,
			Size:     int64(len(b)),
			Typeflag: tar.TypeReg,
			Mode:     0755,
		})
		tw.Write(b)
	}
	tw.Close()
	gw.Close()
	return &buf
}

func TestExtractFromTarGz_FindsTarget(t *testing.T) {
	archive := makeTarGz(t, map[string]string{
		"prefix/other-file": "other content",
		"prefix/llama-tui":  "binary content",
	})

	destPath := filepath.Join(t.TempDir(), "llama-tui")
	if err := extractFromTarGz(bytes.NewReader(archive.Bytes()), "llama-tui", destPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("could not read extracted file: %v", err)
	}
	if string(got) != "binary content" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestExtractFromTarGz_MissingTarget(t *testing.T) {
	archive := makeTarGz(t, map[string]string{
		"other-file": "data",
	})

	destPath := filepath.Join(t.TempDir(), "llama-tui")
	err := extractFromTarGz(bytes.NewReader(archive.Bytes()), "llama-tui", destPath)
	if err == nil {
		t.Fatal("expected error for missing target, got nil")
	}
}

// ─── extractAllToDir ─────────────────────────────────────────────────────────

func makeTarGzWithSymlink(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Regular file
	content := []byte("real binary")
	tw.WriteHeader(&tar.Header{
		Name:     "prefix/real-binary",
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
		Mode:     0755,
	})
	tw.Write(content)

	// Symlink pointing to real-binary
	tw.WriteHeader(&tar.Header{
		Name:     "prefix/link-binary",
		Typeflag: tar.TypeSymlink,
		Linkname: "real-binary",
	})

	tw.Close()
	gw.Close()
	return &buf
}

func TestExtractAllToDir_RegularFileAndSymlink(t *testing.T) {
	archive := makeTarGzWithSymlink(t)
	destDir := t.TempDir()

	if err := extractAllToDir(bytes.NewReader(archive.Bytes()), destDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Regular file
	content, err := os.ReadFile(filepath.Join(destDir, "real-binary"))
	if err != nil {
		t.Fatalf("real-binary not extracted: %v", err)
	}
	if string(content) != "real binary" {
		t.Fatalf("unexpected content: %q", content)
	}

	// Symlink
	target, err := os.Readlink(filepath.Join(destDir, "link-binary"))
	if err != nil {
		t.Fatalf("link-binary symlink not created: %v", err)
	}
	if target != "real-binary" {
		t.Fatalf("unexpected symlink target: %q", target)
	}
}
