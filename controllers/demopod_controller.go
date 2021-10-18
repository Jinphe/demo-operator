/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	k8sv1alpha1 "github.com/jinphe/demo-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// DemoPodReconciler reconciles a DemoPod object
type DemoPodReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=batch.jinphe.github.io,resources=demopods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch.jinphe.github.io,resources=demopods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=batch.jinphe.github.io,resources=demopods/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DemoPod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *DemoPodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("Request.Namespace", req.Namespace, "Request.Name", req.Name)
	logger.Info("start reconcile")

	// Fetch the ImoocPod instance
	instance := &k8sv1alpha1.DemoPod{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}
	lbls := labels.Set{
		"app": instance.Name,
	}
	existingPods := &corev1.PodList{}
	//1. 获取name对应的所有的pod列表
	if err := r.List(ctx, existingPods, &client.ListOptions{
		Namespace:     req.Namespace,
		LabelSelector: labels.SelectorFromSet(lbls),
	}); err != nil {
		logger.Error(err, "取已经存在的pod失败")
		return ctrl.Result{}, err
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
		instance.Status = status
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.Error(err, "更新pod的状态失败")
			return ctrl.Result{}, err
		}
	}

	//4. len(pod) < 运行中的len(pod.replace)，期望值大，需要 scale up create
	if len(existingPodNames) < instance.Spec.Replicas {
		//create
		logger.Info(fmt.Sprintf("正在创建pod, 当前的podnames: %s 期望的Relicas: %d", existingPodNames, instance.Spec.Replicas))

		// Define a new Pod object
		pod := newPodForCR(instance)

		// Set DemoPod instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, pod, r.Scheme); err != nil {
			logger.Error(err, "创建pod失败: SetControllerReference")
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, pod); err != nil {
			logger.Error(err, "创建pod失败: create pod")
			return ctrl.Result{}, err
		}
	}

	//5. len(pod) > 运行中的len(pod.replace),期望值小，需要 scale down delete
	if len(existingPodNames) > instance.Spec.Replicas {
		//delete
		logger.Info(fmt.Sprintf("正在删除pod，当前的podnames: %s 期望的Relicas: %d", existingPodNames, instance.Spec.Replicas))
		pod := existingPods.Items[0]
		existingPods.Items = existingPods.Items[1:]
		if err := r.Delete(ctx, &pod); err != nil {
			logger.Error(err, "删除pod失败")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{Requeue: true}, nil
}

func newPodForCR(cr *k8sv1alpha1.DemoPod) *corev1.Pod {
	labels := map[string]string{
		"app": cr.Name,
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cr.Name + "-pod",
			Namespace:    cr.Namespace,
			Labels:       labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "busybox",
					Command: []string{"sleep", "36000000"},
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
