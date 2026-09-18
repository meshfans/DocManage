// verify-probe-png: 验证 base64 PNG 合法性
package main

import (
	"encoding/base64"
	"fmt"
	"image/png"
	"os"
	"strings"
)

func main() {
	b64 := strings.TrimSpace(os.Args[1])
	bytes, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		fmt.Printf("base64 decode error: %v\n", err)
		return
	}

	img, err := png.Decode(strings.NewReader(string(bytes)))
	if err != nil {
		fmt.Printf("PNG decode error: %v\n", err)
		return
	}

	fmt.Printf("PNG OK: %d x %d, size=%d bytes\n", img.Bounds().Dx(), img.Bounds().Dy(), len(bytes))
}