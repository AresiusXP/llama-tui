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
