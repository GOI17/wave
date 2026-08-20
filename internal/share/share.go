package share

import (
	"fmt"
	"os/exec"
	"strings"

	"wave/internal/bundle"
)

const script = `import AppKit

final class ShareDelegate: NSObject, NSSharingServicePickerDelegate {
    func sharingServicePicker(_ sharingServicePicker: NSSharingServicePicker, didChoose service: NSSharingService?) {
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
            NSApp.stop(nil)
        }
    }
}

guard CommandLine.arguments.count > 1 else { exit(2) }
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let window = NSWindow(
    contentRect: NSRect(x: 0, y: 0, width: 1, height: 1),
    styleMask: [.titled],
    backing: .buffered,
    defer: false
)
let delegate = ShareDelegate()
window.center()
window.makeKeyAndOrderFront(nil)
app.activate(ignoringOtherApps: true)
let picker = NSSharingServicePicker(items: [URL(fileURLWithPath: CommandLine.arguments[1])])
picker.delegate = delegate
picker.show(relativeTo: window.contentView!.bounds, of: window.contentView!, preferredEdge: .maxY)
app.run()`

var run = runOSAScript

// Archive validates path and opens the native macOS Share Sheet.
func Archive(path string) error {
	opened, err := bundle.Open(path)
	if err != nil {
		return fmt.Errorf("share requires a valid .wave archive: %w", err)
	}
	_ = opened.Close()
	return run(path)
}

func runOSAScript(path string) error {
	command := exec.Command("/usr/bin/swift", "-", path)
	command.Stdin = strings.NewReader(script)
	return command.Run()
}
