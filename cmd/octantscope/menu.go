package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gordonklaus/portaudio"
)

// audioSource is the unified representation of a selectable capture source.
// Exactly one of paDevice or pwNodeID>0 is set.
type audioSource struct {
	label      string                // display name shown in the menu
	isMonitor  bool                  // true for output-sink monitors
	paDevice   *portaudio.DeviceInfo // non-nil: open this PortAudio device directly
	pwNodeID   int                   // >0: capture via pw-cat --target <id>
	pwIsMonitor bool                 // true: target is the sink's monitor, not the source
}

// menuChoice is the result of showMenu.
type menuChoice struct {
	source audioSource
	siggen bool
	quit   bool
}

// menuItem is one row in the rendered list.
type menuItem struct {
	label  string
	kind   string // "input" | "monitor" | "direct" | "siggen" | "header"
	source audioSource
}

// showMenu renders an interactive source-selection menu and returns the choice.
// portaudio.Initialize must already have been called.
func showMenu(br *bufio.Reader) menuChoice {
	items := buildMenuItems()

	cursor := firstSelectable(items, 0)

	for {
		drawMenu(items, cursor)

		b, err := br.ReadByte()
		if err != nil {
			return menuChoice{quit: true}
		}
		switch b {
		case 'q', 3:
			return menuChoice{quit: true}
		case '\r', '\n':
			it := items[cursor]
			if it.kind == "siggen" {
				return menuChoice{siggen: true}
			}
			return menuChoice{source: it.source}
		case 0x1b:
			b2, _ := br.ReadByte()
			if b2 != '[' {
				break
			}
			b3, _ := br.ReadByte()
			switch b3 {
			case 'A':
				cursor = prevSelectable(items, cursor)
			case 'B':
				cursor = nextSelectable(items, cursor)
			}
		}
	}
}

// buildMenuItems creates the full item list from PipeWire and PortAudio sources.
func buildMenuItems() []menuItem {
	var items []menuItem

	// --- PipeWire sources (preferred; covers monitors) ---
	pwSources, pwSinks := listPipeWireAudioNodes()
	if len(pwSources) > 0 {
		items = append(items, menuItem{label: "Input Devices", kind: "header"})
		for _, n := range pwSources {
			items = append(items, menuItem{
				label: n.Description,
				kind:  "input",
				source: audioSource{
					label:    n.Description,
					pwNodeID: n.ID,
				},
			})
		}
	}
	if len(pwSinks) > 0 {
		items = append(items, menuItem{label: "Output Monitors", kind: "header"})
		for _, n := range pwSinks {
			items = append(items, menuItem{
				label: n.Description,
				kind:  "monitor",
				source: audioSource{
					label:       n.Description,
					isMonitor:   true,
					pwNodeID:    n.ID,
					pwIsMonitor: true,
				},
			})
		}
	}

	// --- Direct ALSA hw: devices (bypass PipeWire) ---
	directItems := directALSAItems(pwSources)
	if len(directItems) > 0 {
		items = append(items, menuItem{label: "Direct ALSA (bypasses PipeWire)", kind: "header"})
		items = append(items, directItems...)
	}

	// --- Signal generator ---
	items = append(items, menuItem{label: "Other", kind: "header"})
	items = append(items, menuItem{label: "Signal Generator", kind: "siggen"})

	return items
}

// directALSAItems returns PortAudio hw: devices that have input channels and
// are not already represented by a PipeWire source.
func directALSAItems(pwSources []pwAudioNode) []menuItem {
	var devices []*portaudio.DeviceInfo
	var err error
	suppressStderr(func() { devices, err = portaudio.Devices() })
	if err != nil {
		return nil
	}
	var items []menuItem
	for _, d := range devices {
		if d.MaxInputChannels <= 0 {
			continue
		}
		// Skip the pulse/pipewire aggregate device; it's used internally.
		nameLower := strings.ToLower(d.Name)
		if nameLower == "pulse" || nameLower == "pipewire" ||
			strings.HasPrefix(nameLower, "default") {
			continue
		}
		items = append(items, menuItem{
			label: d.Name,
			kind:  "direct",
			source: audioSource{
				label:    d.Name,
				paDevice: d,
			},
		})
	}
	return items
}

// drawMenu clears the screen and redraws the menu.
func drawMenu(items []menuItem, cursor int) {
	var sb strings.Builder
	sb.WriteString("\x1b[2J\x1b[H")
	sb.WriteString("  \x1b[1moctantscope\x1b[0m — select audio source\r\n\r\n")

	for i, it := range items {
		switch it.kind {
		case "header":
			fmt.Fprintf(&sb, "  \x1b[2m── %s\x1b[0m\r\n", it.label)
		case "monitor":
			suffix := "  \x1b[2m(monitor)\x1b[0m"
			if i == cursor {
				fmt.Fprintf(&sb, "  \x1b[1;32m▶  %s%s\x1b[0m\r\n", it.label, suffix)
			} else {
				fmt.Fprintf(&sb, "     %s%s\r\n", it.label, suffix)
			}
		default:
			if i == cursor {
				fmt.Fprintf(&sb, "  \x1b[1;32m▶  %s\x1b[0m\r\n", it.label)
			} else {
				fmt.Fprintf(&sb, "     %s\r\n", it.label)
			}
		}
	}

	sb.WriteString("\r\n  \x1b[2m↑/↓  navigate    Enter  select    q  quit\x1b[0m\r\n")
	os.Stdout.WriteString(sb.String())
}

// pwAudioNode holds the identity of a PipeWire audio node.
type pwAudioNode struct {
	ID          int    // PipeWire node serial ID, used as pw-cat --target
	Name        string // e.g. "alsa_input.pci-0000_c1_00.6.analog-stereo"
	Description string // e.g. "Family 17h/19h HD Audio Controller Analog Stereo"
}

// listPipeWireAudioNodes runs pw-dump and returns Audio/Source and Audio/Sink nodes.
func listPipeWireAudioNodes() (sources, sinks []pwAudioNode) {
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		return
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(out, &raw); err != nil {
		return
	}

	for _, obj := range raw {
		if obj["type"] != "PipeWire:Interface:Node" {
			continue
		}
		// Node serial ID is a top-level float64 in the JSON.
		idF, _ := obj["id"].(float64)
		info, _ := obj["info"].(map[string]interface{})
		if info == nil {
			continue
		}
		props, _ := info["props"].(map[string]interface{})
		if props == nil {
			continue
		}
		mediaClass, _ := props["media.class"].(string)
		nodeName, _ := props["node.name"].(string)
		nodeDesc, _ := props["node.description"].(string)
		if nodeDesc == "" {
			nodeDesc = nodeName
		}
		if nodeName == "" {
			continue
		}
		node := pwAudioNode{ID: int(idF), Name: nodeName, Description: nodeDesc}
		switch mediaClass {
		case "Audio/Source":
			sources = append(sources, node)
		case "Audio/Sink":
			sinks = append(sinks, node)
		}
	}
	return
}

func firstSelectable(items []menuItem, from int) int {
	for i := from; i < len(items); i++ {
		if items[i].kind != "header" {
			return i
		}
	}
	return from
}

func nextSelectable(items []menuItem, cur int) int {
	for i := cur + 1; i < len(items); i++ {
		if items[i].kind != "header" {
			return i
		}
	}
	return cur
}

func prevSelectable(items []menuItem, cur int) int {
	for i := cur - 1; i >= 0; i-- {
		if items[i].kind != "header" {
			return i
		}
	}
	return cur
}
