# octantscope

A terminal oscilloscope heavily modeled on [dood.al/oscilloscope](https://dood.al/oscilloscope/). Renders a real-time XY phosphor display — complete with Gaussian beam glow, phosphor persistence, and a CRT graticule — using [octant](https://github.com/reynoldsme/octant) block-character graphics. A `--sixel` flag for terminals that support it.

## System requirement

PortAudio is required for live audio capture. Install the development package (which includes the pkg-config file needed to build):

```shell
# Debian / Ubuntu
sudo apt install portaudio19-dev
```

## Install

```shell
go install github.com/reynoldsme/octant/cmd/octantscope@latest
```

Or build from source:

```shell
git clone git@github.com:reynoldsme/octant.git
cd octant
go build -o octantscope ./cmd/octantscope/
```

## Usage

```
octantscope [flags]
```

On startup an interactive menu lists available audio input devices and PipeWire/PulseAudio output monitors (for capturing playback audio). Pass `--siggen` to skip the menu and use the built-in signal generator.

### Display flags

| Flag | Default | Description |
|------|---------|-------------|
| `--gain float` | `0` | Main gain exponent; actual scale = 2^gain |
| `--exposure float` | `0` | Brightness in stops (each stop doubles/halves) |
| `--hue float` | `120` | Phosphor hue 0–360 (120 = green, 0 = red, 240 = blue) |
| `--persistence float` | `0` | Phosphor afterglow −1 (short) to +1 (long) |
| `--grid int` | `0` | Graticule mode: 0 = dark, 1 = colored, 2 = off |
| `--swap-xy` | off | Swap X and Y axes |
| `--invert-x` | off | Invert X axis |
| `--invert-y` | off | Invert Y axis |
| `--no-filter` | off | Disable Lanczos upsampling (jagged but faster) |
| `--mono` | off | Monochrome octant output |
| `--sixel` | off | DEC sixel output mode |
| `--cols N` | auto | Output width in terminal columns |

### Sweep / time-base flags

| Flag | Default | Description |
|------|---------|-------------|
| `--sweep` | on | Enable time-base sweep mode |
| `--auto-trigger` | off | Re-trigger automatically when no edge is found |
| `--sweep-trigger float` | `0` | Rising-edge trigger threshold −1 to 1 |
| `--sweep-ms-div float` | `1` | Time scale in ms/division (0.25–32) |

### Signal generator flags

| Flag | Default | Description |
|------|---------|-------------|
| `--siggen` | off | Use signal generator instead of live audio |
| `--x-expr string` | `sin(2*PI*a*t)*cos(2*PI*b*t)` | X axis expression |
| `--y-expr string` | `cos(2*PI*a*t)*cos(2*PI*b*t)` | Y axis expression |
| `--a float` | `3` | `a` parameter value (0.5–5) |
| `--a-exp int` | `2` | `a` parameter scale exponent (×10ⁿ, 0–3) |
| `--b float` | `2` | `b` parameter value (0.5–5) |
| `--b-exp int` | `2` | `b` parameter scale exponent (×10ⁿ, 0–3) |

Expressions may use `t` (time in seconds), `a`, `b`, `PI`, `sin(x)`, `cos(x)`, and the four arithmetic operators. With defaults this produces a 300 Hz / 200 Hz Lissajous figure.

## Keyboard controls

Settings changed interactively are saved to `~/.config/octantscope/settings.json` and restored on next launch. CLI flags take precedence over saved settings.

| Key | Action |
|-----|--------|
| `q` / Ctrl-C | Quit |
| `r` | Reset all display settings to defaults |
| `g` | Cycle grid: dark → colored → off |
| `s` | Toggle sweep |
| `a` | Toggle auto-trigger |
| `f` | Freeze display |
| `h` / `H` | Hue −10 / +10 |
| `e` / `E` | Exposure −0.25 / +0.25 stops |
| `p` / `P` | Persistence −0.1 / +0.1 |
| `t` / `T` | Trigger level −0.05 / +0.05 |
| `m` / `M` | ms/div step down / up |
| `+` / `-` | Gain +0.25 / −0.25 |
