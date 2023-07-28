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
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	cro "github.com/RHsyseng/cluster-relocation-operator/api/v1beta1"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	relocationv1alpha1 "github.com/openshift/cluster-relocation-service/api/v1alpha1"
	"github.com/openshift/cluster-relocation-service/internal/filelock"
	"github.com/sirupsen/logrus"
)

type ClusterConfigReconcilerOptions struct {
	ServiceName      string `envconfig:"SERVICE_NAME"`
	ServiceNamespace string `envconfig:"SERVICE_NAMESPACE"`
	ServicePort      string `envconfig:"SERVICE_PORT"`
	ServiceScheme    string `envconfig:"SERVICE_SCHEME"`
	DataDir          string `envconfig:"DATA_DIR" default:"/data"`
}

// ClusterConfigReconciler reconciles a ClusterConfig object
type ClusterConfigReconciler struct {
	client.Client
	Log     logrus.FieldLogger
	Scheme  *runtime.Scheme
	Options *ClusterConfigReconcilerOptions
	BaseURL string
}

//+kubebuilder:rbac:groups=relocation.openshift.io,resources=clusterconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=relocation.openshift.io,resources=clusterconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=relocation.openshift.io,resources=clusterconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch

func (r *ClusterConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithFields(logrus.Fields{"name": req.Name, "namespace": req.Namespace})
	log.Info("Running reconcile ...")
	defer log.Info("Reconcile complete")

	config := &relocationv1alpha1.ClusterConfig{}
	if err := r.Get(ctx, req.NamespacedName, config); err != nil {
		log.WithError(err).Error("failed to get referenced cluster config")
		return ctrl.Result{}, err
	}

	requeue, err := r.writeInputData(ctx, config)
	if err != nil {
		log.WithError(err).Error("failed to write input data")
		return ctrl.Result{}, err
	}
	if requeue {
		log.Info("requeueing due to lock contention")
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	u, err := url.JoinPath(r.BaseURL, "images", req.Namespace, fmt.Sprintf("%s.iso", req.Name))
	if err != nil {
		log.WithError(err).Error("failed to create image url")
		return ctrl.Result{}, err
	}

	if config.Spec.BareMetalHostRef != nil {
		if err := r.setBMHImage(ctx, config.Spec.BareMetalHostRef, u); err != nil {
			log.WithError(err).Error("failed to set BareMetalHost image")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ClusterConfigReconciler) mapBMHToCC(ctx context.Context, obj client.Object) []reconcile.Request {
	bmh := &bmh_v1alpha1.BareMetalHost{}
	bmhName := obj.GetName()
	bmhNamespace := obj.GetNamespace()

	if err := r.Get(ctx, types.NamespacedName{Name: bmhName, Namespace: bmhNamespace}, bmh); err != nil {
		return []reconcile.Request{}
	}
	ccList := &relocationv1alpha1.ClusterConfigList{}
	if err := r.List(ctx, ccList); err != nil {
		return []reconcile.Request{}
	}
	if len(ccList.Items) == 0 {
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}
	for _, cc := range ccList.Items {
		if cc.Spec.BareMetalHostRef == nil {
			continue
		}
		if cc.Spec.BareMetalHostRef.Name == bmhName && cc.Spec.BareMetalHostRef.Namespace == bmhNamespace {
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: cc.Namespace,
					Name:      cc.Name,
				},
			}
			requests = append(requests, req)
		}
	}
	if len(requests) > 1 {
		r.Log.Warn("found multiple ClusterConfigs referencing BaremetalHost %s/%s", bmhNamespace, bmhName)
	}
	return requests
}

func serviceURL(opts *ClusterConfigReconcilerOptions) string {
	host := fmt.Sprintf("%s.%s", opts.ServiceName, opts.ServiceNamespace)
	if opts.ServicePort != "" {
		host = fmt.Sprintf("%s:%s", host, opts.ServicePort)
	}
	u := url.URL{
		Scheme: opts.ServiceScheme,
		Host:   host,
	}
	return u.String()
}

func (r *ClusterConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Options.ServiceName == "" || r.Options.ServiceNamespace == "" || r.Options.ServiceScheme == "" {
		return fmt.Errorf("SERVICE_NAME, SERVICE_NAMESPACE, and SERVICE_SCHEME must be set")
	}
	r.BaseURL = serviceURL(r.Options)

	return ctrl.NewControllerManagedBy(mgr).
		For(&relocationv1alpha1.ClusterConfig{}).
		WatchesRawSource(source.Kind(mgr.GetCache(), &bmh_v1alpha1.BareMetalHost{}), handler.EnqueueRequestsFromMapFunc(r.mapBMHToCC)).
		Complete(r)
}

func (r *ClusterConfigReconciler) setBMHImage(ctx context.Context, bmhRef *relocationv1alpha1.BareMetalHostReference, url string) error {
	bmh := &bmh_v1alpha1.BareMetalHost{}
	key := types.NamespacedName{
		Name:      bmhRef.Name,
		Namespace: bmhRef.Namespace,
	}
	if err := r.Get(ctx, key, bmh); err != nil {
		return err
	}
	patch := client.MergeFrom(bmh.DeepCopy())

	dirty := false
	if !bmh.Spec.Online {
		bmh.Spec.Online = true
		dirty = true
	}
	if bmh.Spec.Image == nil {
		bmh.Spec.Image = &bmh_v1alpha1.Image{}
		dirty = true
	}
	if bmh.Spec.Image.URL != url {
		bmh.Spec.Image.URL = url
		dirty = true
	}
	liveIso := "live-iso"
	if bmh.Spec.Image.DiskFormat == nil || *bmh.Spec.Image.DiskFormat != liveIso {
		bmh.Spec.Image.DiskFormat = &liveIso
		dirty = true
	}

	if dirty {
		if err := r.Patch(ctx, bmh, patch); err != nil {
			return err
		}
	}

	return nil
}

// writeInputData writes the required info based on the cluster config to the config cache dir
func (r *ClusterConfigReconciler) writeInputData(ctx context.Context, config *relocationv1alpha1.ClusterConfig) (bool, error) {
	configDir := filepath.Join(r.Options.DataDir, "namespaces", config.Namespace, config.Name)
	filesDir := filepath.Join(configDir, "files")
	if err := os.MkdirAll(filesDir, 0700); err != nil {
		return false, err
	}

	locked, err := filelock.WithWriteLock(configDir, func() error {
		if err := r.writeClusterRelocation(config, filepath.Join(filesDir, "cluster-relocation.json")); err != nil {
			return err
		}

		if err := r.writeSecretToFile(ctx, config.Spec.APICertRef, filepath.Join(filesDir, "api-cert-secret.json")); err != nil {
			return fmt.Errorf("failed to write api cert secret: %w", err)
		}

		if err := r.writeSecretToFile(ctx, config.Spec.IngressCertRef, filepath.Join(filesDir, "ingress-cert-secret.json")); err != nil {
			return fmt.Errorf("failed to write ingress cert secret: %w", err)
		}

		if err := r.writeSecretToFile(ctx, config.Spec.PullSecretRef, filepath.Join(filesDir, "pull-secret-secret.json")); err != nil {
			return fmt.Errorf("failed to write pull secret: %w", err)
		}

		// TODO: create network config when we know what this looks like
		// no sense in spending time working on a CM if it's not going to be one in the end
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("failed to acquire file lock: %w", err)
	}
	if !locked {
		return true, nil
	}

	return false, nil
}

func (r *ClusterConfigReconciler) writeClusterRelocation(config *relocationv1alpha1.ClusterConfig, file string) error {
	cr := &cro.ClusterRelocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
		},
		Spec: config.Spec.ClusterRelocationSpec,
	}

	gvks, unversioned, err := r.Scheme.ObjectKinds(cr)
	if err != nil {
		return err
	}
	if unversioned || len(gvks) == 0 {
		return fmt.Errorf("unable to find API version for ClusterRelocation")
	}
	// if there are multiple assume the last is the most recent
	gvk := gvks[len(gvks)-1]
	cr.TypeMeta = metav1.TypeMeta{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
	}

	data, err := json.Marshal(cr)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster relocation: %w", err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		return fmt.Errorf("failed to write cluster relocation: %w", err)
	}

	return nil
}

func (r *ClusterConfigReconciler) writeSecretToFile(ctx context.Context, ref *corev1.SecretReference, file string) error {
	if ref == nil {
		return nil
	}

	s := &corev1.Secret{}
	key := types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}
	if err := r.Get(ctx, key, s); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		return err
	}

	return nil
}
