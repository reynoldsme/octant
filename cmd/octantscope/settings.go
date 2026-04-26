package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"

	"github.com/reynoldsme/octant/oscilloscope"
)

// settingsPath returns the path to the user settings file.
// Follows XDG on Linux: ~/.config/octantscope/settings.json
func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".octantscope.json")
	}
	return filepath.Join(dir, "octantscope", "settings.json")
}

// loadSettings reads the settings file and returns the stored Config.
// Returns DefaultConfig() (silently) if the file is absent or unreadable.
func loadSettings() oscilloscope.Config {
	f, err := os.Open(settingsPath())
	if err != nil {
		return oscilloscope.DefaultConfig()
	}
	defer f.Close()
	var cfg oscilloscope.Config
	if json.NewDecoder(f).Decode(&cfg) != nil {
		return oscilloscope.DefaultConfig()
	}
	cfg.Validate() //nolint — clamps any out-of-range values from old files
	return cfg
}

// saveSettings writes cfg to the settings file.
// Transient state (FreezeImage, SignalGeneratorOn) is stripped before writing.
func saveSettings(cfg oscilloscope.Config) {
	cfg.FreezeImage = false
	cfg.SignalGeneratorOn = false

	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(cfg) //nolint — best-effort write
}

// applyExplicitFlags overrides cfg with any flags that were explicitly
// specified on the command line, leaving all other fields from the settings
// file (or DefaultConfig) intact.
func applyExplicitFlags(cfg *oscilloscope.Config) {
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "gain":
			cfg.MainGain = *flagGain
		case "exposure":
			cfg.ExposureStops = *flagExposure
		case "hue":
			cfg.Hue = *flagHue
		case "persistence":
			cfg.Persistence = *flagPersistence
		case "grid":
			cfg.GridMode = *flagGrid
		case "sweep":
			cfg.SweepOn = *flagSweep
		case "auto-trigger":
			cfg.SweepAutoTrigger = *flagSweepAuto
		case "sweep-trigger":
			cfg.SweepTriggerValue = *flagSweepTrig
		case "sweep-ms-div":
			cfg.SweepMsDiv = *flagSweepMsDiv
		case "x-expr":
			cfg.XExpression = *flagXExpr
		case "y-expr":
			cfg.YExpression = *flagYExpr
		case "a":
			cfg.AValue = *flagA
		case "a-exp":
			cfg.AExponent = *flagAExp
		case "b":
			cfg.BValue = *flagB
		case "b-exp":
			cfg.BExponent = *flagBExp
		}
		// swap-xy, invert-x/y, no-filter, siggen are always taken from CLI
		// (applied unconditionally in main after this call).
	})
}
