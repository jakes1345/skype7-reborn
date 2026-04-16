package main

import (
	"fmt"
	"github.com/pion/mediadevices"
	_ "github.com/pion/mediadevices/pkg/driver/camera"
)

func main() {
	fmt.Println("Scanning for Video Devices...")
	devices := mediadevices.EnumerateDevices()
	
	if len(devices) == 0 {
		fmt.Println("CRITICAL: No video devices found. Check if your user is in the 'video' group.")
		return
	}

	for _, device := range devices {
		fmt.Printf("Found Device: %s (Type: %v)\n", device.Name, device.Kind)
		for _, prop := range device.Capabilities {
			fmt.Printf("  - Resolution: %dx%d @ %f fps\n", prop.Width, prop.Height, prop.FrameRate)
		}
	}
}
