# octanttarget

Displays an image in the terminal using [octant](https://github.com/reynoldsme/octant) block-character graphics. The image can be repositioned and scaled interactively via keyboard or a JSON HTTP API.

## Usage

```
go run ./cmd/octanttarget/ [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `-image` | `target.png` | Image file to display (JPEG or PNG) |
| `-port` | `8077` | HTTP API port |
| `-step` | `5` | Keyboard movement step in percentage points |
| `-scale-step` | `0.1` | Keyboard scale step |

## Keyboard controls

| Key | Action |
|-----|--------|
| Arrow keys | Move image |
| `+` / `=` | Scale up |
| `-` | Scale down |
| `q` / `ESC` | Quit |

Position is expressed as a percentage of the canvas (0–100 on each axis). Scale is a multiplier relative to the largest size that fits the terminal.

## HTTP API

All requests and responses use JSON. The server listens on `localhost:<port>`.

### `POST /move`

Reposition the image. Supply absolute coordinates, a relative delta, or both.

```json
{ "x": 50, "y": 50 }
```
```json
{ "dx": 5, "dy": -5 }
```

`x`/`y` are clamped to 0–100. `dx`/`dy` are added to the current position.

### `POST /scale`

Resize the image.

```json
{ "scale": 1.5 }
```
```json
{ "dscale": 0.1 }
```

`scale` sets an absolute multiplier; `dscale` is added to the current scale. Scale is clamped to 0.05–20.

### `POST /image`

Swap the displayed image at runtime.

```json
{ "file": "path/to/image.png" }
```

### `GET /status`

Returns the current display state.

```json
{ "x": 50, "y": 50, "scale": 1, "file": "target.png" }
```
