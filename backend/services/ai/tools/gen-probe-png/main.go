// gen-probe-png: 生成 256x64 PNG（白底黑字 "MULTIMODAL"）
// 用于多模态检测的探测图。
//
// 运行：go run ./services/ai/tools/gen-probe-png/main.go > probe-sample.b64
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

func main() {
	const (
		width      = 512
		height     = 160
		text       = "MULTIMODAL"
		borderSize = 4
	)
	textColor := color.RGBA{0, 0, 0, 255}         // 黑字
	bgColor := color.RGBA{255, 255, 255, 255}    // 白底
	borderColor := color.RGBA{0, 0, 0, 255}       // 黑边

	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// 背景：白
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bgColor)
		}
	}

	// 边框：黑
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < borderSize || x >= width-borderSize || y < borderSize || y >= height-borderSize {
				img.Set(x, y, borderColor)
			}
		}
	}

	// 文字渲染：使用简单 5x7 点阵字体（仅含所需字符）
	drawText(img, text, width, height, textColor)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		fmt.Printf("PNG encode error: %v\n", err)
		return
	}

	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	fmt.Println(b64)
}

// 简化版 5x7 点阵字体（仅含大写字母 + 空格）
var font5x7 = map[rune][]string{
	'M': {"XXXXX", "X...X", "X...X", "XXXXX", "X...X", "X...X", "X...X"},
	'U': {"X...X", "X...X", "X...X", "X...X", "X...X", "X...X", ".XXX."},
	'L': {"X....", "X....", "X....", "X....", "X....", "X....", "XXXXX"},
	'T': {"XXXXX", "..X..", "..X..", "..X..", "..X..", "..X..", "..X.."},
	'I': {"XXXXX", "..X..", "..X..", "..X..", "..X..", "..X..", "XXXXX"},
	'O': {".XXX.", "X...X", "X...X", "X...X", "X...X", "X...X", ".XXX."},
	'D': {"XXXX.", "X...X", "X...X", "X...X", "X...X", "X...X", "XXXX."},
	'A': {".XXX.", "X...X", "X...X", "XXXXX", "X...X", "X...X", "X...X"},
}

// drawText 在图像上居中绘制字符串
func drawText(img *image.RGBA, text string, width, height int, c color.Color) {
	runes := []rune(text)
	charWidth := 5
	charHeight := 7
	gap := 1
	scale := 12 // 每字符像素缩放

	textWidth := (len(runes) * charWidth + (len(runes)-1)*gap) * scale
	textHeight := charHeight * scale

	startX := (width - textWidth) / 2
	startY := (height - textHeight) / 2

	for i, ch := range runes {
		rows, ok := font5x7[ch]
		if !ok {
			continue
		}
		for ry, row := range rows {
			for rx, r := range row {
				if r != 'X' {
					continue
				}
				// 放大 scale 倍
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						x := startX + (i*(charWidth+gap)+rx)*scale + sx
						y := startY + ry*scale + sy
						if x >= 0 && x < width && y >= 0 && y < height {
							img.Set(x, y, c)
						}
					}
				}
			}
		}
	}
}