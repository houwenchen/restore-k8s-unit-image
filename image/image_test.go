package image

import (
	"fmt"
	"testing"
)

// TODO: 增加测试用例，增加对子方法的测试
func TestNewKubeReleaseInfo(t *testing.T) {
	kr := NewKubeReleaseInfo("v1.23.0")
	fmt.Printf("%+v\n", kr)
}

func TestMainloop(t *testing.T) {
	kr := NewKubeReleaseInfo("v1.23.0")
	kr.Run()
}
