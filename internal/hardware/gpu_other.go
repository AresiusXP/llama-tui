//go:build !darwin && !linux

package hardware

func detectGPUs() []GPU {
	return nil
}

// DetectLinuxGPUBuild is a no-op on non-Linux platforms — always returns "".
func DetectLinuxGPUBuild() string { return "" }
