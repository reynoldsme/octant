# octant

Renders images and GIF animations in the terminal using [octant](https://github.com/reynoldsme/octant) block-character graphics.

## Install

```shell
go install github.com/reynoldsme/octant/cmd/octant@latest
```

Or build from source:

```shell
git clone git@github.com:reynoldsme/octant.git
cd octant
go build -o octant ./cmd/octant/
```

## Usage

```shell
octant [--mono] [--cols N] [--png out.png] <image.jpg|image.png|image.gif>
```

| Flag | Description |
|------|-------------|
| `--cols N` / `-c N` | Maximum output width in terminal columns. Defaults to the current terminal width. |
| `--mono` / `-m` | Monochrome output (1-bit Floyd-Steinberg dithered). |
| `--png out.png` | Write the octant-quantised image to a PNG file instead of the terminal. |

Animated GIFs play in a loop (respecting the GIF's loop-count setting) until the program receives `SIGINT` (Ctrl-C).
