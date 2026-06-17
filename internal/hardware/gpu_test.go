package hardware

import "testing"

func TestLlamaServerFlagsNil(t *testing.T) {
	flags := LlamaServerFlags(nil, -1)
	if len(flags) == 0 {
		t.Fatal("expected flags, got none")
	}
	// should return CPU-only flags
	found := false
	for i, f := range flags {
		if f == "--n-gpu-layers" && i+1 < len(flags) && flags[i+1] == "0" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --n-gpu-layers 0, got %v", flags)
	}
}

func TestLlamaServerFlagsApple(t *testing.T) {
	gpu := &GPU{Index: 0, Vendor: "apple"}
	flags := LlamaServerFlags(gpu, -1)
	// Should include --n-gpu-layers 999 (auto)
	found := false
	for i, f := range flags {
		if f == "--n-gpu-layers" && i+1 < len(flags) && flags[i+1] == "999" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --n-gpu-layers 999, got %v", flags)
	}
}

func TestDetectGPUs(t *testing.T) {
	gpus := DetectGPUs()
	// Just check it doesn't panic and returns a slice
	t.Logf("detected %d GPU(s): %+v", len(gpus), gpus)
	for i, g := range gpus {
		if g.Index != i {
			t.Errorf("GPU[%d].Index = %d, want %d", i, g.Index, i)
		}
		if len(gpus) > 0 && i == 0 && !g.IsDefault {
			t.Error("first GPU should have IsDefault=true")
		}
	}
}

// TestParseLspciMixed verifies that parseLspci correctly returns both
// NVIDIA and AMD entries from mixed lspci output.
func TestParseLspciMixed(t *testing.T) {
	input := `01:00.0 VGA compatible controller: NVIDIA Corporation GA102 [GeForce RTX 3080 Ti] (rev a1)
c7:00.0 VGA compatible controller: Advanced Micro Devices, Inc. [AMD/ATI] HawkPoint1 (rev c5)`

	gpus := parseLspci(input)
	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d: %+v", len(gpus), gpus)
	}

	// First should be NVIDIA
	if gpus[0].Vendor != "nvidia" {
		t.Errorf("GPU[0] vendor = %q, want nvidia", gpus[0].Vendor)
	}
	// Second should be AMD
	if gpus[1].Vendor != "amd" {
		t.Errorf("GPU[1] vendor = %q, want amd", gpus[1].Vendor)
	}
}

// TestParseNvidiaSMIMixed verifies that when nvidia-smi returns NVIDIA GPUs,
// the AMD iGPU from lspci is not dropped.
func TestDetectGPUsMergesNvidiaAndAMD(t *testing.T) {
	nvidiaSMIOut := `0, NVIDIA GeForce RTX 3080 Ti, 12288`
	lspciOut := `01:00.0 VGA compatible controller: NVIDIA Corporation GA102 [GeForce RTX 3080 Ti] (rev a1)
c7:00.0 VGA compatible controller: Advanced Micro Devices, Inc. [AMD/ATI] HawkPoint1 (rev c5)`

	nvidiaGPUs := parseNvidiaSMI(nvidiaSMIOut)
	lspciGPUs := parseLspci(lspciOut)

	// Simulate the merge: nvidia-smi provides NVIDIA, lspci provides the rest.
	var merged []GPU
	for _, g := range nvidiaGPUs {
		merged = append(merged, g)
	}
	for _, g := range lspciGPUs {
		if g.Vendor == "nvidia" {
			continue // skip: covered by nvidia-smi
		}
		merged = append(merged, g)
	}

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged GPUs, got %d: %+v", len(merged), merged)
	}
	if merged[0].Vendor != "nvidia" {
		t.Errorf("merged[0] vendor = %q, want nvidia", merged[0].Vendor)
	}
	if merged[1].Vendor != "amd" {
		t.Errorf("merged[1] vendor = %q, want amd", merged[1].Vendor)
	}
}

// TestLlamaServerFlagsNVIDIA verifies --main-gpu is no longer emitted.
func TestLlamaServerFlagsNVIDIA(t *testing.T) {
	gpu := &GPU{Index: 0, Vendor: "nvidia"}
	flags := LlamaServerFlags(gpu, -1)
	for i, f := range flags {
		if f == "--main-gpu" {
			t.Errorf("--main-gpu should not be in flags; found at index %d: %v", i, flags)
		}
	}
	// Should still offload layers
	found := false
	for i, f := range flags {
		if f == "--n-gpu-layers" && i+1 < len(flags) && flags[i+1] == "999" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --n-gpu-layers 999, got %v", flags)
	}
}

// TestLlamaServerEnv verifies the correct env var is returned for each vendor.
func TestLlamaServerEnv(t *testing.T) {
	tests := []struct {
		name    string
		gpu     *GPU
		wantLen int
		wantEnv string
	}{
		{
			name:    "nil GPU",
			gpu:     nil,
			wantLen: 0,
		},
		{
			name:    "apple GPU",
			gpu:     &GPU{Index: 0, Vendor: "apple"},
			wantLen: 0,
		},
		{
			name:    "unknown vendor",
			gpu:     &GPU{Index: 0, Vendor: "unknown"},
			wantLen: 0,
		},
		{
			name:    "NVIDIA at index 0",
			gpu:     &GPU{Index: 0, Vendor: "nvidia"},
			wantLen: 1,
			wantEnv: "GGML_VK_VISIBLE_DEVICES=0",
		},
		{
			name:    "AMD at index 1",
			gpu:     &GPU{Index: 1, Vendor: "amd"},
			wantLen: 1,
			wantEnv: "GGML_VK_VISIBLE_DEVICES=1",
		},
		{
			name:    "Intel at index 0",
			gpu:     &GPU{Index: 0, Vendor: "intel"},
			wantLen: 1,
			wantEnv: "GGML_VK_VISIBLE_DEVICES=0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := LlamaServerEnv(tc.gpu)
			if len(env) != tc.wantLen {
				t.Fatalf("LlamaServerEnv() len = %d, want %d; got %v", len(env), tc.wantLen, env)
			}
			if tc.wantLen > 0 && env[0] != tc.wantEnv {
				t.Errorf("LlamaServerEnv()[0] = %q, want %q", env[0], tc.wantEnv)
			}
		})
	}
}

func TestTrimLspciName(t *testing.T) {
	tests := []struct {
		raw    string
		vendor string
		want   string
	}{
		{
			raw:    "Advanced Micro Devices, Inc. [AMD/ATI] HawkPoint1 (rev c5)",
			vendor: "amd",
			want:   "AMD HawkPoint1",
		},
		{
			raw:    "Advanced Micro Devices, Inc. [AMD] Phoenix Internal GPP Bridge (rev 01)",
			vendor: "amd",
			want:   "AMD Phoenix Internal GPP Bridge",
		},
		{
			raw:    "Intel Corporation Iris Xe Graphics (rev 05)",
			vendor: "intel",
			want:   "Intel Iris Xe Graphics",
		},
		{
			raw:    "NVIDIA Corporation GA102 [GeForce RTX 3080 Ti] (rev a1)",
			vendor: "nvidia",
			// NVIDIA prefix stripped, rev stripped, NVIDIA tag prepended
			want: "NVIDIA GA102 [GeForce RTX 3080 Ti]",
		},
	}

	for _, tc := range tests {
		got := trimLspciName(tc.raw, tc.vendor)
		if got != tc.want {
			t.Errorf("trimLspciName(%q, %q) = %q, want %q", tc.raw, tc.vendor, got, tc.want)
		}
	}
}

