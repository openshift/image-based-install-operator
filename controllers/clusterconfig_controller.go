/*
Copyright 2023.

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
	"net/url"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	relocationv1alpha1 "github.com/carbonin/cluster-relocation-service/api/v1alpha1"
	"github.com/sirupsen/logrus"
)

type ClusterConfigReconcilerOptions struct {
	BaseURL string `envconfig:"BASE_URL"`
}

// ClusterConfigReconciler reconciles a ClusterConfig object
type ClusterConfigReconciler struct {
	client.Client
	Log     logrus.FieldLogger
	Scheme  *runtime.Scheme
	Options *ClusterConfigReconcilerOptions
}

//+kubebuilder:rbac:groups=relocation.openshift.io,resources=clusterconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=relocation.openshift.io,resources=clusterconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=relocation.openshift.io,resources=clusterconfigs/finalizers,verbs=update

func (r *ClusterConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithFields(logrus.Fields{"name": req.Name, "namespace": req.Namespace})

	config := &relocationv1alpha1.ClusterConfig{}
	if err := r.Get(ctx, req.NamespacedName, config); err != nil {
		log.WithError(err).Error("failed to get referenced cluster config")
		return ctrl.Result{}, err
	}

	imageURL, err := url.JoinPath(r.Options.BaseURL, req.Namespace, req.Name, ".iso")
	if err != nil {
		log.WithError(err).Error("failed to create image url")
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(config.DeepCopy())
	config.Status.ImageURL = imageURL
	if err := r.Patch(ctx, config, patch); err != nil {
		log.WithError(err).Error("failed to patch cluster config")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ClusterConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&relocationv1alpha1.ClusterConfig{}).
		Complete(r)
}
