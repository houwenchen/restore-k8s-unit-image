package main

import (
	"fmt"

	"github.com/houwenchen/restore-k8s-unit-image/image"
)

func main() {
	kr := image.NewKubeReleaseInfo("v1.23.1")
	fmt.Println(kr)
	kr.Run()
}
