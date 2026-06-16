//go:build !darwin && !linux

package hardware

func detectGPUs() []GPU {
	return nil
}
