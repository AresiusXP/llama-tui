//go:build darwin

package hardware

func detectGPUs() []GPU {
	output := runCommand("system_profiler", "SPDisplaysDataType")
	if output == "" {
		return nil
	}
	return parseMacOSGPUs(output)
}

// DetectLinuxGPUBuild is a no-op on Darwin — always returns "".
func DetectLinuxGPUBuild() string { return "" }
