package image

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/caoyingjunz/pixiulib/exec"
	"k8s.io/apimachinery/pkg/util/version"
)

const (
	remoteRegistryUrl string = "wenchenhou"
	sourceRegistryUrl string = "registry.cn-hangzhou.aliyuncs.com/google_containers"
)

type kubeReleaseInfo struct {
	// kubernetes version
	// eg: v1.23.0
	kubeVersion string
	// kube-apiserver, kube-controller-manager, kube-scheduler, kube-proxy, etcd, pause, coredns
	subUnitVersions map[string]string
	// 记录 kubeadm 获取组件版本信息时的镜像的前缀
	subUnitPrefixs map[string]string
	// 记录 kubernetes 集群的各个组件的 image 是否在 dockerhub 中存在
	subUnitExist map[string]bool

	// 存放 image 的 dockerhub 地址
	remoteRegistry  string
	remoteImageInfo map[string]string
	// 拉取 image 的地址
	sourceRegistry  string
	sourceImageInfo map[string]string

	// 当环境没有安装 kubeadm 时，从 kubernetes 的 constants 文件中解析版本
	constantsUrl string
	existKubeadm bool
	existDocker  bool

	// 主机上执行命令的接口
	exec exec.Interface
}

type writeCounter struct {
	total       int64
	totalLength int64
}

type kubeadmResp struct {
	Kind       string   `json:"kind"`
	ApiVersion string   `json:"apiVersion"`
	Images     []string `json:"images"`
}

// 初始化kubeReleaseInfo
func NewKubeReleaseInfo(releaseBranch string) *kubeReleaseInfo {
	kr := &kubeReleaseInfo{
		kubeVersion:     releaseBranch,
		subUnitVersions: make(map[string]string),
		subUnitPrefixs:  make(map[string]string),
		subUnitExist:    make(map[string]bool),
		remoteRegistry:  remoteRegistryUrl,
		remoteImageInfo: make(map[string]string),
		sourceRegistry:  sourceRegistryUrl,
		sourceImageInfo: make(map[string]string),
		exec:            exec.New(),
	}

	kr.formatKubeVersion()
	kr.constantsUrl = fmt.Sprintf("https://raw.githubusercontent.com/kubernetes/kubernetes/%s/cmd/kubeadm/app/constants/constants.go", kr.kubeVersion)
	kr.dockerExist()
	kr.kubeadmExist()
	kr.getSubUnitVersions()
	kr.buildAllImageInfo()
	kr.checkDockerHub()

	return kr
}

// run 增加检查 subUnitVersions
func (kr *kubeReleaseInfo) Run() {
	if len(kr.subUnitVersions) == 0 {
		fmt.Println("subUnitVersions don't contains any info, please check")
		return
	}
	kr.imageManageProcess()
}

// kubeVersion 格式检查，标准格式是：v1.23.0
func (kr *kubeReleaseInfo) formatKubeVersion() {
	slices := strings.Split(kr.kubeVersion, ".")
	if len(slices) != 3 {
		panic(errors.New("kubeVersion format error, should be same as v1.23.0"))
	}

	if !strings.HasPrefix(kr.kubeVersion, "v") {
		kr.kubeVersion = "v" + kr.kubeVersion
	}
}

// 检查主机是否安装了 docker, 直接使用 docker search 命令是否成功判断是否安装 docker，顺便测试与 dockerhub 的连通性
func (kr *kubeReleaseInfo) dockerExist() {
	_, err := kr.exec.Command("docker", "search", "busybox").CombinedOutput()
	if err != nil {
		kr.existDocker = false
		fmt.Println("host docker env have some issue, please check")
		panic(err)
	}
	kr.existDocker = true
}

// 检查主机是否安装了 kubeadm
func (kr *kubeReleaseInfo) kubeadmExist() {
	_, err := kr.exec.LookPath("kubeadm")
	if err != nil {
		kr.existKubeadm = false
		return
	}
	kr.existKubeadm = true
}

// 使用不同的方法获取 subUnitVersions
func (kr *kubeReleaseInfo) getSubUnitVersions() {
	if kr.existKubeadm {
		kr.getSubUnitVersionsViaKubeadm()
	} else {
		kr.getSubUnitVersionsViaConstantsUrl()
	}
}

// 使用 kubeadm 构造 subUnitVersions
func (kr *kubeReleaseInfo) getSubUnitVersionsViaKubeadm() error {
	kubeadmresp := &kubeadmResp{}

	out, err := kr.exec.Command("kubeadm", "config", "images", "list", "--kubernetes-version=v1.23.0", "-o=json").CombinedOutput()
	if err != nil {
		// kubeadm 获取失败时，使用 constantsUrl 解析版本
		fmt.Println("get subUnitVersions via kubeadm failed")
		fmt.Println("get subUnitVersions via ConstantsUrl")

		if err = kr.getSubUnitVersionsViaConstantsUrl(); err != nil {
			fmt.Println("get subUnitVersions via ConstantsUrl failed")
			return err
		}
	}

	err = json.Unmarshal(out, kubeadmresp)
	if err != nil {
		fmt.Println("kubeadmresp unmarshal failed")
		fmt.Println(err)
		return err
	}

	// 对 images 进行处理, 将数据整合进 kubeReleaseInfo 的 subUnitVersions 字段
	// k8s.gcr.io/coredns/coredns:v1.8.6
	for _, image := range kubeadmresp.Images {
		unitInfos := strings.Split(image, "/")
		prefix := strings.Join(unitInfos[:len(unitInfos)-1], "/")
		UnitAndVersion := unitInfos[len(unitInfos)-1]
		unitVersion := strings.Split(UnitAndVersion, ":")
		kr.subUnitPrefixs[unitVersion[0]] = prefix
		kr.subUnitVersions[unitVersion[0]] = unitVersion[1]
	}

	return nil
}

// 使用 constantsUrl 构造 subUnitVersions
func (kr *kubeReleaseInfo) getSubUnitVersionsViaConstantsUrl() error {
	infos := make(map[string]string)
	v, _ := version.ParseGeneric(kr.kubeVersion)
	fmt.Println(v)
	err := kr.getImageVersions(v, infos)
	if err != nil {
		return err
	}

	if version, ok := infos["coredns/coredns"]; ok {
		delete(infos, "coredns/coredns")
		infos["coredns"] = version
	}
	kr.subUnitVersions = infos

	return nil
}

// 解析 constantsUrl 文件，获取 subUnitVersions
// 因为国内访问 raw.githubusercontent.com 有概率失败，增加重试操作
func (kr *kubeReleaseInfo) getImageVersions(ver *version.Version, images map[string]string) error {
	constants, err := kr.getFromURL()
	if err != nil {
		return err
	}
	lines := strings.Split(constants, "\n")

	// map[coredns:v1.8.6 etcd:3.5.1-0 kube-apiserver:v1.23.0 kube-controller-manager:v1.23.0
	// kube-proxy:v1.23.0 kube-scheduler:v1.23.0 pause:3.6]
	k8sVersionV := "v" + ver.String()
	images["kube-apiserver"] = k8sVersionV
	images["kube-controller-manager"] = k8sVersionV
	images["kube-scheduler"] = k8sVersionV
	images["kube-proxy"] = k8sVersionV
	images["etcd"] = ""
	images["pause"] = ""

	// the hyperkube image was removed for version v1.17
	if ver.Major() == 1 && ver.Minor() < 17 {
		images["hyperkube"] = k8sVersionV
	}
	// the cloud-controller-manager image was removed for version v1.16
	if ver.Major() == 1 && ver.Minor() < 16 {
		images["cloud-controller-manager"] = k8sVersionV
	}

	// coredns changed image location after 1.21.0-alpha.1
	coreDNSNewVer := version.MustParseSemantic("v1.21.0-alpha.1")
	coreDNSPath := "coredns"
	if ver.AtLeast(coreDNSNewVer) {
		coreDNSPath = "coredns/coredns"
	}

	// parse the constants file and fetch versions.
	// note: Split(...)[1] is safe here given a line contains the key.
	for _, line := range lines {
		if strings.Contains(line, "CoreDNSVersion = ") {
			line = strings.TrimSpace(line)
			line = strings.Split(line, "CoreDNSVersion = ")[1]
			line = strings.Replace(line, `"`, "", -1)
			images[coreDNSPath] = line
		} else if strings.Contains(line, "DefaultEtcdVersion = ") {
			line = strings.TrimSpace(line)
			line = strings.Split(line, "DefaultEtcdVersion = ")[1]
			line = strings.Replace(line, `"`, "", -1)
			images["etcd"] = line
		} else if strings.Contains(line, "PauseVersion = ") {
			line = strings.TrimSpace(line)
			line = strings.Split(line, "PauseVersion = ")[1]
			line = strings.Replace(line, `"`, "", -1)
			images["pause"] = line
		}
	}
	// hardcode the tag for pause as older k8s branches lack a constant.
	if images["pause"] == "" {
		images["pause"] = "3.1"
	}
	// verify.
	fmt.Printf("* getImageVersions(): [%s] %#v\n", ver.String(), images)
	if images[coreDNSPath] == "" || images["etcd"] == "" {
		return fmt.Errorf("at least one image version could not be set: %#v", images)
	}
	return nil
}

func (wc *writeCounter) PrintProgress() {
	if wc.totalLength == 0 {
		fmt.Printf("\r* progress...%d bytes", wc.total)
		return
	}
	fmt.Printf("\r* progress...%d %% ", int64((float64(wc.total)/float64(wc.totalLength))*100.0))
}

func (wc *writeCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.total += int64(n)
	return n, nil
}

// 使用 http 客户端的方式拉取 constantsUrl 的信息
func (kr *kubeReleaseInfo) getFromURL() (string, error) {
	url := kr.constantsUrl
	client := http.Client{}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("responded with status: %d", resp.StatusCode)
	}

	var src io.Reader
	var dst bytes.Buffer

	counter := &writeCounter{totalLength: int64(0)}
	src = io.TeeReader(resp.Body, counter)

	_, err = io.Copy(&dst, src)
	if err != nil {
		panic(err)
	}

	return dst.String(), nil
}

// 维护 remoteImageInfo 和 sourceImageInfo 字段
func (kr *kubeReleaseInfo) buildAllImageInfo() {
	// 以组件 coredns 为例
	// remoteImageInfo 中：wenchenhou/coredns:v1.8.6
	// sourceImageInfo 中: registry.cn-hangzhou.aliyuncs.com/google_containers/coredns:v1.8.6
	for unitName, unitVersion := range kr.subUnitVersions {
		kr.remoteImageInfo[unitName] = kr.remoteRegistry + "/" + unitName + ":" + unitVersion
		kr.sourceImageInfo[unitName] = kr.sourceRegistry + "/" + unitName + ":" + unitVersion
	}
}

// 在做镜像转存前，先检查 dockerhub 中是否已经存在镜像
// 将检查的结果维护在 subUnitExist 字段中
// 因为 docker search 没有办法获取 image 的 tag 信息
// 所以使用 docker pull 的返回来判断 image 是否存在
// 维护 subUnitExist 字段
func (kr *kubeReleaseInfo) checkDockerHub() {
	for unitName, unitInfo := range kr.remoteImageInfo {
		// docker image pull wenchenhou/coredns:v1.8.6
		// TODO: 本地存在没有 push 上去的情况需要考虑下
		_, err := kr.exec.Command("docker", "image", "pull", unitInfo).CombinedOutput()
		if err != nil {
			kr.subUnitExist[unitName] = false
			continue
		}
		kr.subUnitExist[unitName] = true
	}
}

// 实现镜像下载，修改 tag ，转存到dockerhub
// TODO: 需要增加 handleErr 的逻辑
// 这个逻辑需要考虑的比较多，介入的时间点，以及重做的位置的定位
// 思路：开启一个死循环，以是否所有的操作均完成为判断标准，每个操作 err 的时候就会有一个信号产生
func (kr *kubeReleaseInfo) imageManageProcess() {
	for unitName, exist := range kr.subUnitExist {
		if !exist {
			pullErr := kr.pullFromSourceRegistry(unitName)
			if pullErr != nil {
				// TODO:
				fmt.Println()
			}
			retagErr := kr.retagImage(unitName)
			if retagErr != nil {
				// TODO:
				fmt.Println()
			}
			pushErr := kr.pushToRemoteRegistry(unitName)
			if pushErr != nil {
				// TODO:
				fmt.Println()
			}
		}
	}
}

func (kr *kubeReleaseInfo) pullFromSourceRegistry(unitName string) error {
	// docker image pull registry.cn-hangzhou.aliyuncs.com/google_containers/ingress-nginx/controller:v1.1.1
	out, err := kr.exec.Command("docker", "image", "pull", kr.sourceImageInfo[unitName]).CombinedOutput()
	if err != nil {
		fmt.Println("docker image pull failed, err: ", err)
		return err
	}
	fmt.Println(string(out))
	return nil
}

func (kr *kubeReleaseInfo) retagImage(unitName string) error {
	// docker image tag registry.cn-hangzhou.aliyuncs.com/google_containers/coredns:v1.8.6 wenchenhou/coredns:v1.8.6
	_, err := kr.exec.Command("docker", "image", "tag", kr.sourceImageInfo[unitName], kr.remoteImageInfo[unitName]).CombinedOutput()
	if err != nil {
		fmt.Println("docker image tag failed, err: ", err)
		return err
	}
	return nil
}

func (kr *kubeReleaseInfo) pushToRemoteRegistry(unitName string) error {
	// docker image push wenchenhou/coredns:v1.8.6
	out, err := kr.exec.Command("docker", "image", "push", kr.remoteImageInfo[unitName]).CombinedOutput()
	if err != nil {
		fmt.Println("docker image push failed, err: ", err)
		return err
	}
	fmt.Println(string(out))
	return nil
}
