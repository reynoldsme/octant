# octantgore

Runs DOOM in the terminal using [octant](https://github.com/reynoldsme/octant) block-character graphics. Requires a DOOM WAD file (e.g. `doom.wad` from a retail or [freedoom release](https://github.com/freedoom/freedoom/releases)).

## System requirements

ALSA development headers are required for audio (used by the `ebitengine/oto` audio library):

```shell
# Debian / Ubuntu
sudo apt install libasound2-dev
```

## Install

```shell
go install github.com/reynoldsme/octant/cmd/octantgore@latest
```

Or build from source:

```shell
git clone git@github.com:reynoldsme/octant.git
cd octant
go build -o octantgore ./cmd/octantgore/
```

## Usage

```
octantgore -iwad doom.wad
```

## Keyboard controls

| Key | Action |
|-----|--------|
| Arrow keys | Move / turn |
| `,` | Fire |
| Space | Use / open |
| Enter | Confirm |
| Escape | Menu / back |
| Tab | Automap |
| `0`–`9` | Cheats / menu selection |

---

DOOM is a registered trademark of id Software LLC, a ZeniMax Media company.
This project is not affiliated with or endorsed by id Software or ZeniMax Media.
