package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"

	"github.com/ericpauley/go-quantize/quantize"
)

func main() {
	// Open the JPEG file
	imgFile, err := os.Open("input.jpg")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer imgFile.Close()

	// Decode the image.
	img, err := jpeg.Decode(imgFile)
	if err != nil {
		fmt.Println("Error decoding image:", err)
		return
	}

	// Process the image in 2x4 blocks
	for y := 0; y < img.Bounds().Max.Y; y += 4 {
		for x := 0; x < img.Bounds().Max.X; x += 2 {
			block := getBlock(img, x, y)
			foreColor, backColor, octant := processBlock(block)
			printBlock(foreColor, backColor, octant)
		}
		fmt.Println() // New line at the end of each row of blocks
	}
}

// Get a 2x4 block of pixels from the image and return as an image.Image
func getBlock(img image.Image, x, y int) image.Image {
	block := image.NewRGBA(image.Rect(0, 0, 2, 4))
	for dy := 0; dy < 4; dy++ {
		for dx := 0; dx < 2; dx++ {
			if x+dx < img.Bounds().Max.X && y+dy < img.Bounds().Max.Y {
				block.Set(dx, dy, img.At(x+dx, y+dy))
			}
		}
	}
	return block
}

// Process each block to quantize and map to octants
func processBlock(block image.Image) (color.Color, color.Color, rune) {
	// Quantize the block to two colors
	quantizer := quantize.MedianCutQuantizer{}
	palette := quantizer.Quantize(make([]color.Color, 0, 2), block)

	var backColor color.Color
	// Assume the two colors are the most frequent ones
	foreColor := palette[0]

	if len(palette) == 1 {
		backColor = palette[0]
	} else {
		backColor = palette[1]
	}
	// Placeholder for mapping to octant
	//octant := '▀'
	//octant := '▄'
	//octant := ' '
	octant := getOctant(block)

	return foreColor, backColor, octant
}

// Print each block with colors and octant
func printBlock(foreColor, backColor color.Color, octant rune) {
	// Convert color.Color to RGBA
	foreRGBA := color.RGBAModel.Convert(foreColor).(color.RGBA)
	backRGBA := color.RGBAModel.Convert(backColor).(color.RGBA)

	// Set foreground and background colors using VT100 escape sequences
	fmt.Printf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%c\x1b[0m",
		foreRGBA.R, foreRGBA.G, foreRGBA.B,
		backRGBA.R, backRGBA.G, backRGBA.B,
		octant)
}

// getOctant takes a 2x4 image and returns the matching octant
func getOctant(block image.Image) rune {
	// Placeholder octant array
	octants := []rune{' ', '𜴀', '𜴃', '𜴉', '𜴘', '𜴄', '𜴊', '𜴙', '𜴶', '𜵱', '𜴁', '𜴋', '𜴚', '𜴷', '𜵲', '𜴆', '𜴜', '𜴹', '𜵴', '𜴐', '𜴽', '𜵸', '𜴧', '𜶀', '𜵑', '𜴂', '𜴅', '𜴌', '𜴛', '𜴸', '𜵳', '𜴇', '𜴍', '𜴝', '𜴺', '𜵵', '𜴑', '𜴠', '𜴾', '𜵹', '𜴨', '𜵅', '𜶁', '𜵒', '𜶐', '𜶬', '𜴈', '𜴎', '𜴞', '𜴻', '𜵶', '𜴒', '𜴡', '𜴿', '𜵺', '𜴩', '𜵆', '𜶂', '𜵓', '𜶑', '𜶭', '𜴔', '𜴣', '𜵁', '𜵼', '𜴫', '𜵈', '𜶄', '𜵕', '𜶓', '𜶯', '𜴯', '𜵋', '𜶈', '𜵙', '𜶖', '𜶳', '𜵡', '𜶜', '𜶻', '𜷋', '𜴏', '𜴟', '𜴼', '𜵷', '𜴓', '𜴢', '𜵀', '𜵻', '𜴪', '𜵇', '𜶃', '𜵔', '𜶒', '𜶮', '𜴕', '𜴤', '𜵂', '𜵽', '𜴬', '𜶅', '𜵖', '𜶰', '𜴰', '𜵌', '𜶉', '𜵚', '𜶗', '𜶴', '𜵢', '𜶝', '𜶼', '𜷌', '𜴖', '𜴥', '𜵃', '𜵾', '𜴭', '𜵉', '𜶆', '𜵗', '𜶔', '𜶱', '𜴱', '𜶊', '𜵛', '𜶵', '𜵣', '𜶞', '𜶽', '𜷍', '𜴳', '𜵎', '𜶌', '𜵝', '𜶙', '𜶷', '𜵥', '𜶠', '𜶿', '𜷏', '𜵩', '𜶤', '𜷃', '𜷓', '𜴗', '𜴦', '𜵄', '𜵿', '𜴮', '𜵊', '𜶇', '𜵘', '𜶕', '𜶲', '𜴲', '𜵍', '𜶋', '𜵜', '𜶘', '𜶶', '𜵤', '𜶟', '𜶾', '𜷎', '𜴴', '𜵏', '𜶍', '𜵞', '𜶚', '𜶸', '𜵦', '𜶡', '𜷀', '𜷐', '𜵪', '𜶥', '𜷄', '𜷔', '𜷛', '𜴵', '𜵐', '𜶎', '𜵟', '𜶛', '𜶹', '𜵧', '𜶢', '𜷁', '𜷑', '𜵫', '𜶦', '𜷅', '𜷕', '𜷜', '𜵭', '𜶨', '𜷇', '𜷗', '𜷞', '𜷡', '𜶏', '𜵠', '𜶺', '𜵨', '𜶣', '𜷂', '𜷒', '𜵬', '𜶧', '𜷆', '𜷖', '𜷝', '𜵮', '𜶩', '𜷈', '𜷘', '𜷢', '𜵯', '𜶪', '𜷉', '𜷙', '𜷟', '𜵰', '𜶫', '𜷊', '𜷚', '𜷠', '𜷣', '𜷤', '𜷥', ' '}

	// Determine the two colors used in the block
	var color1, color2 color.Color
	color1 = block.At(0, 0)
	hasSecondColor := false

	// Scan for the second color
	for dy := 0; dy < 4; dy++ {
		for dx := 0; dx < 2; dx++ {
			c := block.At(dx, dy)
			if !colorEqual(c, color1) {
				color2 = c
				hasSecondColor = true
				break
			}
		}
		if hasSecondColor {
			break
		}
	}

	// Convert 2x4 block to index
	var index int
	for dy := 0; dy < 4; dy++ {
		for dx := 0; dx < 2; dx++ {
			c := block.At(dx, dy)
			index <<= 1
			if hasSecondColor && colorEqual(c, color2) {
				index |= 1
			}
		}
	}
	fmt.Println(index)
	return octants[index%len(octants)]
}

// colorEqual compares two colors
func colorEqual(c1, c2 color.Color) bool {
	r1, g1, b1, _ := c1.RGBA()
	r2, g2, b2, _ := c2.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2
}
