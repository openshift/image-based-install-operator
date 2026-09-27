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
	// These are required for image parsing to work correctly with digest-based pull specs
	// See: https://github.com/opencontainers/go-digest/blob/v1.0.0/README.md#usage
	_ "crypto/sha256"
	_ "crypto/sha512"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	apicfgv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/openshift/image-based-install-operator/internal/monitor"
	"github.com/sirupsen/logrus"
)

// ImageClusterInstallMonitor reconciles a ImageClusterInstall object
type ImageClusterInstallMonitor struct {
	client.Client
	Log                          logrus.FieldLogger
	Scheme                       *runtime.Scheme
	DefaultInstallTimeout        time.Duration
	GetSpokeClusterInstallStatus monitor.GetInstallStatusFunc
	Options                      *ImageClusterInstallReconcilerOptions
}

//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;
//+kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=imageclusterinstalls,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=imageclusterinstalls/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch

func (r *ImageClusterInstallMonitor) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithFields(logrus.Fields{"name": req.Name, "namespace": req.Namespace})
	log.Info("Monitor running reconcile ...")
	defer log.Info("Monitor reconcile complete")

	ici := &v1alpha1.ImageClusterInstall{}
	if err := r.Get(ctx, req.NamespacedName, ici); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Nothing to do if the installation process hasn't started
	if ici.Status.BootTime.IsZero() {
		return ctrl.Result{}, nil
	}
	// Nothing to do if the installation process has already stopped
	if InstallationCompleted(ici) {
		log.Infof("Cluster %s/%s finished installation process, nothing to do", ici.Namespace, ici.Name)
		return ctrl.Result{}, nil
	}
	return r.monitorInstallationProgress(ctx, log, ici)
}

func (r *ImageClusterInstallMonitor) monitorInstallationProgress(
	ctx context.Context,
	log logrus.FieldLogger,
	ici *v1alpha1.ImageClusterInstall) (ctrl.Result, error) {

	bmh, err := r.getBMH(ctx, ici.Status.BareMetalHostRef)
	if err != nil {
		log.WithError(err).Error("failed to get BareMetalHost")
		return ctrl.Result{}, err
	}
	if !bmh.Status.PoweredOn {
		log.Infof("BareMetalHost %s/%s is not powered on yet", bmh.Name, bmh.Namespace)
		timedout, err := r.handleClusterTimeout(ctx, log, ici, r.DefaultInstallTimeout)
		if err != nil {
			return ctrl.Result{}, err
		}
		if timedout {
			log.Infof("BareMetalHost %s/%s failed to power on within the cluster installation timeout", bmh.Name, bmh.Namespace)
			// in case of timeout we want to requeue after 1 hour
			return ctrl.Result{RequeueAfter: time.Hour}, nil
		}
		if err := r.setClusterInstallingConditions(ctx, ici, "Waiting for BMH to power on"); err != nil {
			log.WithError(err).Error("failed to set installing conditions")
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	res, err := r.checkClusterStatus(ctx, log, ici, bmh)
	if err != nil {
		log.WithError(err).Error("failed to check cluster status")
		return ctrl.Result{}, err
	}
	return res, nil
}

func (r *ImageClusterInstallMonitor) handleClusterTimeout(ctx context.Context, log logrus.FieldLogger, ici *v1alpha1.ImageClusterInstall, defaultTimeout time.Duration) (bool, error) {
	timeout := defaultTimeout

	if installationTimedout(ici) {
		return true, nil
	}

	if timeoutOverride, present := ici.Annotations[installTimeoutAnnotation]; present {
		var err error
		timeout, err = time.ParseDuration(timeoutOverride)
		if err != nil {
			return false, fmt.Errorf("failed to parse install timeout annotation value %s: %w", timeoutOverride, err)
		}
	}

	if ici.Status.BootTime.Add(timeout).Before(time.Now()) {
		err := r.setClusterTimeoutConditions(ctx, ici, timeout.String())
		if err != nil {
			log.WithError(err).Error("failed to set cluster timeout conditions")
		}
		return true, nil
	}
	return false, nil
}

func (r *ImageClusterInstallMonitor) checkClusterStatus(ctx context.Context,
	log logrus.FieldLogger,
	ici *v1alpha1.ImageClusterInstall,
	bmh *bmh_v1alpha1.BareMetalHost) (ctrl.Result, error) {
	spokeClient, err := r.spokeClient(ctx, ici)
	if err != nil {
		log.WithError(err).Error("failed to create spoke client")
		return ctrl.Result{}, err
	}

	status := r.GetSpokeClusterInstallStatus(ctx, log, spokeClient)
	if !status.Installed {
		timedout, err := r.handleClusterTimeout(ctx, log, ici, r.DefaultInstallTimeout)
		if err != nil {
			return ctrl.Result{}, err
		}
		if timedout {
			// in case of timeout we want to requeue after 1 hour
			return ctrl.Result{RequeueAfter: time.Hour}, nil
		}
		log.Infof("cluster install in progress: %s", status.String())
		if err := r.setClusterInstallingConditions(ctx, ici, status.String()); err != nil {
			log.WithError(err).Error("failed to set installing conditions")
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	log.Info("cluster is installed")

	// After installation ended we don't want that ironic will do any changes in the node
	patch := client.MergeFrom(bmh.DeepCopy())
	if setAnnotationIfNotExists(&bmh.ObjectMeta, detachedAnnotation, detachedAnnotationValue) {
		log.Infof("Adding detached annotations to BareMetalHost (%s/%s)", bmh.Name, bmh.Namespace)
		if err := r.Patch(ctx, bmh, patch); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.setClusterInstalledConditions(ctx, ici); err != nil {
		log.WithError(err).Error("failed to set installed conditions")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ImageClusterInstallMonitor) spokeClient(ctx context.Context, ici *v1alpha1.ImageClusterInstall) (client.Client, error) {
	if ici.Spec.ClusterMetadata == nil || ici.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name == "" {
		return nil, fmt.Errorf("kubeconfig secret must be set to get spoke client")
	}
	key := types.NamespacedName{
		Namespace: ici.Namespace,
		Name:      ici.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name,
	}

	secret := corev1.Secret{}
	if err := r.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("failed to get admin kubeconfig secret %s: %w", key, err)
	}

	if secret.Data == nil {
		return nil, fmt.Errorf("Secret %s/%s does not contain any data", secret.Namespace, secret.Name)
	}

	kubeconfig, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfig) == 0 {
		return nil, fmt.Errorf("Secret data for %s/%s does not contain kubeconfig", secret.Namespace, secret.Name)
	}

	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get clientconfig from kubeconfig data: %w", err)
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get restconfig for kube client: %w", err)
	}
	restConfig.Timeout = 10 * time.Second

	var schemes = runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(schemes))
	utilruntime.Must(apicfgv1.AddToScheme(schemes))

	spokeClient, err := client.New(restConfig, client.Options{Scheme: schemes})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize spoke client: %s", err)
	}

	return spokeClient, nil
}

func (r *ImageClusterInstallMonitor) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate that check if BootTime is initialized
	bootTimeInitialized := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			obj := e.ObjectNew.(*v1alpha1.ImageClusterInstall)
			return !obj.Status.BootTime.IsZero()
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return false // Do not trigger on create events
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false // Do not trigger on delete events
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false // Ignore generic events
		},
	}
	r.Log.Infof("Setting up controller ImageClusterInstallMonitor with %d concurrent reconciles", r.Options.MaxConcurrentReconciles)

	return ctrl.NewControllerManagedBy(mgr).
		Named("ImageClusterInstallMonitor").
		WithOptions(controller.Options{MaxConcurrentReconciles: r.Options.MaxConcurrentReconciles}).
		For(&v1alpha1.ImageClusterInstall{}).
		WithEventFilter(bootTimeInitialized).
		Complete(r)
}

func (r *ImageClusterInstallMonitor) getBMH(ctx context.Context, bmhRef *v1alpha1.BareMetalHostReference) (*bmh_v1alpha1.BareMetalHost, error) {
	bmh := &bmh_v1alpha1.BareMetalHost{}
	key := types.NamespacedName{
		Name:      bmhRef.Name,
		Namespace: bmhRef.Namespace,
	}
	if err := r.Get(ctx, key, bmh); err != nil {
		return nil, err
	}

	return bmh, nil
}
