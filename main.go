package main

import (
	"fmt"

	"github.com/houwenchen/restore-k8s-unit-image/image"
)

func main() {
	kr := image.NewKubeReleaseInfo("1.25.1")
	fmt.Println(kr)
	kr.Run()
}
