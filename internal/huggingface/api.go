package huggingface

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AresiusXP/llama-tui/assets"
)

const baseURL = "https://huggingface.co"
const apiURL = "https://huggingface.co/api"

// Client is the HuggingFace API client.
type Client struct {
	token      string
	httpClient *http.Client
}

// ModelInfo is the brief info returned by search.
type ModelInfo struct {
	ID           string    `json:"id"`
	Author       string    `json:"author"`
	LastModified time.Time `json:"lastModified"`
	Downloads    int       `json:"downloads"`
	Likes        int       `json:"likes"`
	Tags         []string  `json:"tags"`
	Private      bool      `json:"private"`
}

// RepoFile represents a file inside a HF repo.
type RepoFile struct {
	RFilename string   `json:"rfilename"` // relative filename
	Size      int64    `json:"size"`
	LFS       *LFSInfo `json:"lfs,omitempty"`
}

// LFSInfo holds LFS metadata for a file.
type LFSInfo struct {
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	PointerSize int64  `json:"pointerSize"`
}

// PopularModel is an entry from the embedded popular_models.json.
type PopularModel struct {
	RepoID           string   `json:"repo_id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	RecommendedQuant string   `json:"recommended_quant"`
	ApproxSizeGB     float64  `json:"approx_size_gb"`
	Tags             []string `json:"tags"`
}

// NewClient creates a new HuggingFace client. token may be empty for public models.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// newRequest creates an HTTP request with optional auth header.
func (c *Client) newRequest(ctx context.Context, method, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

// SearchModels searches HuggingFace for GGUF models matching query.
// Returns up to 20 results.
func (c *Client) SearchModels(ctx context.Context, query string) ([]ModelInfo, error) {
	endpoint := fmt.Sprintf(
		"%s/models?search=%s&filter=gguf&limit=20&full=false",
		apiURL,
		url.QueryEscape(query),
	)

	req, err := c.newRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return nil, fmt.Errorf("huggingface: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("huggingface: search models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("huggingface: search models: unexpected status %d", resp.StatusCode)
	}

	var models []ModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("huggingface: search models: decode response: %w", err)
	}

	return models, nil
}

// EffectiveSize returns the real file size, preferring LFS metadata over the
// siblings "size" field (which is the LFS pointer size for large files).
func (f RepoFile) EffectiveSize() int64 {
	if f.LFS != nil && f.LFS.Size > 0 {
		return f.LFS.Size
	}
	return f.Size
}

// modelResponse is the full model detail response from the API.
type modelResponse struct {
	Siblings []RepoFile `json:"siblings"`
}

// ListGGUFFiles lists downloadable .gguf model files in the given repo.
// It requests blob metadata (?blobs=true) so real LFS file sizes are returned.
// Auxiliary files (multimodal projectors, MTP heads, subdirectory files) are
// excluded — only root-level standalone model weights are returned.
func (c *Client) ListGGUFFiles(ctx context.Context, repoID string) ([]RepoFile, error) {
	// ?blobs=true tells the API to include real LFS sizes in the siblings list.
	endpoint := fmt.Sprintf("%s/models/%s?blobs=true", apiURL, repoID)

	req, err := c.newRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return nil, fmt.Errorf("huggingface: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("huggingface: list files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("huggingface: list files: unexpected status %d", resp.StatusCode)
	}

	var model modelResponse
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, fmt.Errorf("huggingface: list files: decode response: %w", err)
	}

	var ggufFiles []RepoFile
	for _, f := range model.Siblings {
		if !strings.HasSuffix(strings.ToLower(f.RFilename), ".gguf") {
			continue
		}
		if isAuxiliaryGGUF(f.RFilename) {
			continue
		}
		ggufFiles = append(ggufFiles, f)
	}

	return ggufFiles, nil
}

// shardPattern matches split GGUF filenames like "model-00001-of-00003.gguf".
// Capture groups: (1) base name, (2) shard index, (3) total shards.
var shardPattern = regexp.MustCompile(`(?i)^(.*)-(\d{5})-of-(\d{5})\.gguf$`)

// isAuxiliaryGGUF returns true for GGUF files that are not standalone model
// weights — specifically multimodal projectors (mmproj-*), multi-token
// prediction heads (mtp-*), subdirectory files, and non-first shards of
// split models (e.g. -00002-of-00003.gguf).
func isAuxiliaryGGUF(rfilename string) bool {
	// Files in subdirectories (e.g. MTP/gemma-...gguf) are never root models.
	if strings.Contains(rfilename, "/") {
		return true
	}
	base := strings.ToLower(rfilename)
	// Multimodal projector files.
	if strings.HasPrefix(base, "mmproj-") {
		return true
	}
	// Multi-token prediction head files.
	if strings.HasPrefix(base, "mtp-") {
		return true
	}
	// Non-first shards of split GGUFs (-00002-of-N, -00003-of-N, …).
	// Only shard 1 (-00001-of-N) is shown; the rest are downloaded automatically.
	if m := shardPattern.FindStringSubmatch(rfilename); m != nil {
		idx, _ := strconv.Atoi(m[2])
		if idx > 1 {
			return true
		}
	}
	return false
}

// ShardSiblings returns the filenames of all shards that come after filename
// in a split GGUF set. For shard 1 of N this is shards 2…N; for shard K of N
// this is shards K+1…N (useful when resuming a partially downloaded set).
// Returns nil if filename is not a split shard or is already the last shard.
func ShardSiblings(filename string) []string {
	m := shardPattern.FindStringSubmatch(filename)
	if m == nil {
		return nil
	}
	idx, _ := strconv.Atoi(m[2])
	total, _ := strconv.Atoi(m[3])
	if total <= 1 || idx >= total {
		return nil
	}
	prefix := m[1]
	totalStr := m[3]
	ext := ".gguf"
	if strings.HasSuffix(filename, ".GGUF") {
		ext = ".GGUF"
	}
	siblings := make([]string, 0, total-idx)
	for i := idx + 1; i <= total; i++ {
		siblings = append(siblings, fmt.Sprintf("%s-%05d-of-%s%s", prefix, i, totalStr, ext))
	}
	return siblings
}

// DownloadURL returns the direct download URL for a file in a repo.
func DownloadURL(repoID, filename string) string {
	return fmt.Sprintf("%s/%s/resolve/main/%s", baseURL, repoID, filename)
}

// LoadPopularModels loads the embedded popular_models.json.
func LoadPopularModels() ([]PopularModel, error) {
	var models []PopularModel
	if err := json.Unmarshal(assets.PopularModelsJSON, &models); err != nil {
		return nil, fmt.Errorf("huggingface: load popular models: %w", err)
	}
	return models, nil
}
