//go:build darwin

package hardware

func detectGPUs() []GPU {
	output := runCommand("system_profiler", "SPDisplaysDataType")
	if output == "" {
		return nil
	}
	return parseMacOSGPUs(output)
}
