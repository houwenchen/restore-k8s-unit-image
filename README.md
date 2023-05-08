功能：


    实现kubernetes集群的镜像转存。


运行环境：


    docker--必须，作用：镜像pull，retag，push
    kubeadm--可选，作用：获取某一个固定版本的kubernetes的组件版本。
    当kubeadm不存在时，会采用解析https://raw.githubusercontent.com/kubernetes/kubernetes/%s/cmd/kubeadm/app/constants/constants.go，文件的方式获取对应组件版本。


使用：


    修改image.go文件中的remoteRegistryUrl和sourceRegistryUrl，修改main.go中的ReleaseInfo。
