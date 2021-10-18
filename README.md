# demo-operator

参考 [视频教程：k8s二次开发operator](https://www.bilibili.com/video/BV1zE411j7ky) 学习

## 环境

- kubebuilder: v3.0.0

```bash
# 安装 kubebuilder
https://github.com/kubernetes-sigs/kubebuilder/releases

# 下载 mac 版本
kubebuilder_darwin_amd64

# 移动
sudo mv ~/kubebuilder_darwin_amd64 /usr/local/kubebuilder

# 增加权限
 chmod u+x kubebuilder && sudo mv kubebuilder bin
 
# 查验
 kubebuilder version
```
---

## 项目初始化

```bash
# 初始化项目骨架
kubebuilder init \
--domain=jinphe.github.io \
--repo=github.com/jinphe/demo-operator \
--skip-go-version-check

# 创建自定义资源定义（CRD）API 和控制器
kubebuilder create api --group batch --version v1alpha1 --kind DemoPod --resource --controller
```

## 实现业务逻辑

1. 修改 api/v1alpha1/memcached_types.go 中的 Go 类型定义，使其具有以下 spec 和 status

```go
// DemoPodSpec defines the desired state of DemoPod
type DemoPodSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Replicas int `json:"replicas"`
}

// DemoPodStatus defines the observed state of DemoPod
type DemoPodStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Replicas int      `json:"replicas"`
	PodNames []string `json:"podNames"`
}
```

2. 更新生成的代码

```bash
# 更新依赖
go mod tidy

# 为资源类型更新生成的代码
make generate

# 生成和更新 CRD 清单
make manifests
```

3. 实现控制器

```go
package controllers

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	k8sv1alpha1 "github.com/jinphe/demo-operator/api/v1alpha1"
)

// DemoPodReconciler reconciles a DemoPod object
type DemoPodReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=batch.jinphe.github.io,resources=demopods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch.jinphe.github.io,resources=demopods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch.jinphe.github.io,resources=demopods/finalizers,verbs=update
//+kubebuilder:rbac:groups="batch.jinphe.github.io",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="batch.jinphe.github.io",resources=pods/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DemoPod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *DemoPodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	log.Info("Reconciling DemoPod")

	// Fetch the ImoocPod instance
	instance := &k8sv1alpha1.DemoPod{}
	err := r.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	lbls := labels.Set{
		"app": instance.Name,
	}
	existingPods := &corev1.PodList{}
	//1. 获取name对应的所有的pod列表
	err = r.List(context.TODO(), existingPods, &client.ListOptions{
		Namespace:     req.Namespace,
		LabelSelector: labels.SelectorFromSet(lbls),
	})
	if err != nil {
		log.Error(err, "取已经存在的pod失败")
		return reconcile.Result{}, err
	}

	//2. 取pod中的pod name
	var existingPodNames []string
	for _, pod := range existingPods.Items {
		if pod.GetObjectMeta().GetDeletionTimestamp() != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			existingPodNames = append(existingPodNames, pod.GetObjectMeta().GetName())
		}
	}
	//3. update pod.status != 运行中的status
	//比较 DeepEqual
	status := k8sv1alpha1.DemoPodStatus{ // 期望的status
		PodNames: existingPodNames,
		Replicas: len(existingPodNames),
	}
	if !reflect.DeepEqual(instance.Status, status) {
		err := r.Status().Update(context.TODO(), instance)
		if err != nil {
			log.Error(err, "更新pod的状态失败")
			return reconcile.Result{}, err
		}
	}

	//4. len(pod) > 运行中的len(pod.replace),期望值小，需要scale down
	if len(existingPodNames) > instance.Spec.Replicas {
		//delete
		log.Info("正在删除pod，当前的podnames和期望的Relicas:", existingPodNames, instance.Spec.Replicas)
		pod := existingPods.Items[0]
		err := r.Delete(context.TODO(), &pod)
		if err != nil {
			log.Error(err, "删除pod失败")
			return reconcile.Result{}, err
		}
	}

	//5. len(pod) < 运行中的len(pod.replace)，期望值大，需要scale up create
	if len(existingPodNames) < instance.Spec.Replicas {
		//create
		log.Info("正在创建pod,当前的podnames和期望的Replicas:", existingPodNames, instance.Spec.Replicas)
		// Define a new Pod object
		pod := newPodForCR(instance)

		// Set DemoPod instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, pod, r.Scheme); err != nil {
			return reconcile.Result{}, err
		}

		r.Create(context.TODO(), pod)
		if err != nil {
			log.Error(err, "创建pod失败")
			return reconcile.Result{}, err
		}
	}
	// your logic here

	return ctrl.Result{Requeue: true}, nil
}

func newPodForCR(cr *k8sv1alpha1.DemoPod) *corev1.Pod {
	labels := map[string]string{
		"app": cr.Name,
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-pod",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DemoPodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sv1alpha1.DemoPod{}).
		Complete(r)
}
```

## 运行 Operator

1. 准备工作 - 拉取外部镜像
[katacoda网站](https://katacoda.com/)

```bash
# 点开网站，随便打开一个课程
# 登陆自己dockerhub账户
docker login

# 由于kacacoda是国外网站，所以可以直接在它的终端上拉取gcr镜像
docker pull gcr.io/distroless/static:nonroot

# 给镜像改名，一定要是: 你的dockerhub用户名/镜像名:版本，否则无法推送到自己的镜像仓库
docker tag gcr.io/distroless/static:nonroot  jinphe/distroless:nonroot

# 推送到自己的dockerhub镜像仓库
docker push jinphe/distroless:nonroot

# 本地拉取dockerhub镜像仓库，并打tag
docker pull jinphe/distroless:nonroot
docker tag  jinphe/distroless:nonroot  gcr.io/distroless/static:nonroot

# tar: Error opening archive: Unrecognized archive format
goarch="$(go env GOARCH)" 改为 goarch=amd64

# 本地连接远端的kubenetes, make docker-build时test过不去，没有etcd的bin文件，可以把test关了
# 修改Makefile（可选）:

# docker-build: test
docker-build: 
```

2. 部署CRD和CR

```bash
# 将 config/crd 中 CRD 部署到 k8s 集群
make install

# 本地测试运行 controller
make run

# 安装自定义资源(CR):
kubectl apply -f config/samples/
```

3. 构建及部署operator

```bash
# 把 gcr.io/distroless/static:nonroot 改成 jinphe/distroless:nonroot
# go官方镜像里没有设置proxy，会导致 go mod dowanload 失败

# Dockerfile里设置代理
RUN go env -w GO111MODULE=on
RUN go env -w GOPROXY=https://goproxy.cn,direct

# 构建镜像 - 将镜像推送到镜像仓库
docker login
make docker-build docker-push IMG=jinphe/demopod-operator:v1

# 使用 docker 镜像, 部署 controller 到 k8s 集群
make deploy IMG=<some-registry>/<project-name>:tag

```

4. 卸载 
```bash
# 卸载CRD
make uninstall

# 卸载CR
kubectl delete -f config/samples/batch_v1alpha1_demopod.yaml

# 卸载 controller
make undeploy IMG=jinphe/demopod-operator:v1

```
