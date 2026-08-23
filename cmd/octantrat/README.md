# rat

Renders a rotating 3D rat in the terminal using [octant](https://github.com/reynoldsme/octant) block-character graphics.

The mesh and texture are embedded in the binary — no external files required.

## Usage

```
go run ./cmd/octantrat/
```

Override the embedded assets or set the initial rotation speed:

```
octantrat [-obj path/to/model.obj] [-tex path/to/texture.jpg] [-speed 1.0]
```

## Controls

| Key | Action |
|-----|--------|
| `q` / `ESC` / `Ctrl-C` | Quit |
| `w` | Toggle wireframe mode |
| `x` | Toggle X-axis rotation (pitch) |
| `y` | Toggle Y-axis rotation (yaw / spin) — on by default |
| `z` | Toggle Z-axis rotation (roll) |
| `+` / `=` | Speed up rotation |
| `-` | Slow down rotation |
| `r` | Reset rotation speed |
| `[` | Scale model down |
| `]` | Scale model up |
| `h` | Toggle debug overlay (controls, model info, rotation state) |

## Model

Rat model by [EWTube0](https://sketchfab.com/EWTube0),
licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
