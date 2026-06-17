package huggingface

import (
	"reflect"
	"testing"
)

func TestIsAuxiliaryGGUF(t *testing.T) {
	cases := []struct {
		filename string
		want     bool
	}{
		// Standalone — should be kept.
		{"model.gguf", false},
		{"Qwen2.5-7B-Instruct-Q4_K_M.gguf", false},
		{"llama-3-8b-q8_0.gguf", false},
		// Shard 1 — should be kept (entry point for multi-shard download).
		{"model-00001-of-00002.gguf", false},
		{"Qwen2.5-Coder-14B-Instruct-Q4_K_M-00001-of-00002.gguf", false},
		{"model-00001-of-00009.gguf", false},
		// Shard 2+ — should be filtered out (auto-downloaded alongside shard 1).
		{"model-00002-of-00002.gguf", true},
		{"model-00002-of-00009.gguf", true},
		{"model-00009-of-00009.gguf", true},
		{"Qwen2.5-Coder-14B-Instruct-Q4_K_M-00002-of-00002.gguf", true},
		// Auxiliary types.
		{"mmproj-model.gguf", true},
		{"mtp-head.gguf", true},
		{"subdir/model.gguf", true},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			got := isAuxiliaryGGUF(tc.filename)
			if got != tc.want {
				t.Errorf("isAuxiliaryGGUF(%q) = %v, want %v", tc.filename, got, tc.want)
			}
		})
	}
}

func TestShardSiblings(t *testing.T) {
	cases := []struct {
		filename string
		want     []string
	}{
		// Non-shard file — no siblings.
		{"model.gguf", nil},
		// Single-file shard (total=1) — no siblings.
		{"model-00001-of-00001.gguf", nil},
		// Last shard — no siblings.
		{"model-00002-of-00002.gguf", nil},
		{"model-00003-of-00003.gguf", nil},
		// Two-shard model — shard 1 returns shard 2.
		{
			"Qwen2.5-Coder-14B-Instruct-Q4_K_M-00001-of-00002.gguf",
			[]string{"Qwen2.5-Coder-14B-Instruct-Q4_K_M-00002-of-00002.gguf"},
		},
		// Three-shard model — shard 1 returns shards 2 and 3.
		{
			"model-00001-of-00003.gguf",
			[]string{"model-00002-of-00003.gguf", "model-00003-of-00003.gguf"},
		},
		// Three-shard model — shard 2 returns only shard 3 (resume case).
		{
			"model-00002-of-00003.gguf",
			[]string{"model-00003-of-00003.gguf"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			got := ShardSiblings(tc.filename)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ShardSiblings(%q) = %v, want %v", tc.filename, got, tc.want)
			}
		})
	}
}
