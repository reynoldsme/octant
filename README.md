# Octant

An attempt to use [Unicode 16.0 octant characters](https://www.unicode.org/charts/PDF/Unicode-16.0/U160-1CC00.pdf) for higher resolution monochrome and color raster image rendering in the terminal.

Note: requires a font with octant charcters, see below.

"Octants" are grids of characters which are two pixels wide and four pixels tall. With conventional terminal capabilities you can define two colors per cell, the "background" and "foreground" (font color) enabling monochrome graphics at "full" 2x4 resolution or 2x4 resolution with something conceptually similar to chroma subsampling (but even worse). Even so, octants may be able to provide better perceptual resolution that the conventional "half-block" often used for low resolution color images in the terminal.

Unicode 16.0 is not yet finalized, but octant characters have already been added to at least one font, [Cascadia code](https://devblogs.microsoft.com/commandline/cascadia-code-2404-23/) and is available as a [nerd font](https://github.com/microsoft/cascadia-code/releases) which is what most people probably want. NOTE: the "official" nerd fonts do not appear to be updated as of this writing, but the direct download from the github link above does include these characters.

## TODO

* I think this should work if I got the mapping right. There is probably an obvious way to map groups of 2x4 pixels to the matching octant with simple math that is currently eluding me.
