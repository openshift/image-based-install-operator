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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	// These are required for image parsing to work correctly with digest-based pull specs
	// See: https://github.com/opencontainers/go-digest/blob/v1.0.0/README.md#usage
	_ "crypto/sha256"
	_ "crypto/sha512"

	"github.com/sirupsen/logrus"
	"golang.org/x/mod/sumdb/dirhash"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8serrors "k8s.io/apimachinery/pkg/util/errors"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/containers/image/v5/docker/reference"
	"github.com/google/uuid"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	lca_api "github.com/openshift-kni/lifecycle-agent/api/seedreconfig"
	apicfgv1 "github.com/openshift/api/config/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/openshift/image-based-install-operator/internal/certs"
	"github.com/openshift/image-based-install-operator/internal/credentials"
	"github.com/openshift/image-based-install-operator/internal/filelock"
	"github.com/openshift/image-based-install-operator/internal/monitor"
)

type ImageClusterInstallReconcilerOptions struct {
	RouteName      string `envconfig:"ROUTE_NAME"`
	RouteNamespace string `envconfig:"ROUTE_NAMESPACE"`
	RoutePort      string `envconfig:"ROUTE_PORT"`
	RouteScheme    string `envconfig:"ROUTE_SCHEME"`
	DataDir        string `envconfig:"DATA_DIR" default:"/data"`
}

// ImageClusterInstallReconciler reconciles a ImageClusterInstall object
type ImageClusterInstallReconciler struct {
	client.Client
	credentials.Credentials
	Log                          logrus.FieldLogger
	Scheme                       *runtime.Scheme
	Options                      *ImageClusterInstallReconcilerOptions
	BaseURL                      string
	CertManager                  certs.KubeConfigCertManager
	DefaultInstallTimeout        time.Duration
	GetSpokeClusterInstallStatus monitor.GetInstallStatusFunc
}

type imagePullSecret struct {
	Auths map[string]map[string]interface{} `json:"auths"`
}

const (
	detachedAnnotation           = "baremetalhost.metal3.io/detached"
	detachedAnnotationValue      = "imageclusterinstall-controller"
	inspectAnnotation            = "inspect.metal3.io"
	clusterConfigDir             = "cluster-configuration"
	extraManifestsDir            = "extra-manifests"
	manifestsDir                 = "manifests"
	nmstateCMKey                 = "network-config"
	nmstateSecretKey             = "nmstate"
	clusterInstallFinalizerName  = "imageclusterinstall." + v1alpha1.Group + "/deprovision"
	caBundleFileName             = "tls-ca-bundle.pem"
	imageBasedInstallInvoker     = "image-based-install"
	invokerCMFileName            = "invoker-cm.yaml"
	imageDigestMirrorSetFileName = "image-digest-sources.json"
	installTimeoutAnnotation     = "imageclusterinstall." + v1alpha1.Group + "/install-timeout"
	backupLabel                  = "cluster.open-cluster-management.io/backup"
	backupLabelValue             = "true"
)

//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch
//+kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=imageclusterinstalls,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=imageclusterinstalls/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=imageclusterinstalls/finalizers,verbs=update
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=hive.openshift.io,resources=clusterimagesets,verbs=get;list;watch

func (r *ImageClusterInstallReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithFields(logrus.Fields{"name": req.Name, "namespace": req.Namespace})
	log.Info("Running reconcile ...")
	defer log.Info("Reconcile complete")

	ici := &v1alpha1.ImageClusterInstall{}
	if err := r.Get(ctx, req.NamespacedName, ici); err != nil {
		log.WithError(err).Error("failed to get ImageClusterInstall")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if res, stop, err := r.handleFinalizer(ctx, log, ici); !res.IsZero() || stop || err != nil {
		if err != nil {
			log.Error(err)
		}
		return res, err
	}

	// Nothing to do if the installation process has already stopped
	if installationStopped(ici) {
		log.Infof("Cluster %s/%s finished installation process, nothing to do", ici.Namespace, ici.Name)
		return ctrl.Result{}, nil
	}

	if ici.Spec.ClusterDeploymentRef == nil || ici.Spec.ClusterDeploymentRef.Name == "" {
		log.Error("ClusterDeploymentRef is unset, not reconciling")
		return ctrl.Result{}, nil
	}

	clusterDeployment := &hivev1.ClusterDeployment{}
	cdKey := types.NamespacedName{
		Namespace: ici.Namespace,
		Name:      ici.Spec.ClusterDeploymentRef.Name,
	}
	if err := r.Get(ctx, cdKey, clusterDeployment); err != nil {
		if !errors.IsNotFound(err) {
			log.WithError(err).Errorf(
				"failed to get ClusterDeployment with name '%s' in namespace '%s'",
				cdKey.Name, cdKey.Namespace)
			return ctrl.Result{}, err
		}
		errorMessagge := fmt.Errorf("clusterDeployment with name '%s' in namespace '%s' not found",
			cdKey.Name, cdKey.Namespace)
		log.WithError(err).Error(errorMessagge)
		if updateErr := r.setImageReadyCondition(ctx, ici, errorMessagge, ""); updateErr != nil {
			log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if err := r.initializeConditions(ctx, ici); err != nil {
		log.Errorf("Failed to initialize conditions: %s", err)
		return ctrl.Result{}, err
	}

	var bmh *bmh_v1alpha1.BareMetalHost
	var err error
	if ici.Spec.BareMetalHostRef != nil {
		bmh, err = r.getBMH(ctx, ici.Spec.BareMetalHostRef)
		if err != nil {
			log.WithError(err).Error("failed to get BareMetalHost")
			if updateErr := r.setHostConfiguredCondition(ctx, ici, err); updateErr != nil {
				log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
			}
			return ctrl.Result{}, err
		}
	}

	res, updated, err := r.writeInputData(ctx, log, ici, clusterDeployment, bmh)
	if !res.IsZero() || err != nil {
		if err != nil {
			if updateErr := r.setImageReadyCondition(ctx, ici, err, ""); updateErr != nil {
				log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
			}
			log.Error(err)
		}
		return res, err
	}

	if err := r.setClusterInstallMetadata(ctx, ici, clusterDeployment.Name); err != nil {
		log.WithError(err).Error("failed to set ImageClusterInstall data")
		return ctrl.Result{}, err
	}

	r.labelReferencedObjectsForBackup(ctx, log, ici, clusterDeployment)

	imageUrl, err := url.JoinPath(r.BaseURL, "images", req.Namespace, fmt.Sprintf("%s.iso", req.Name))
	if err != nil {
		log.WithError(err).Error("failed to create image url")
		if updateErr := r.setImageReadyCondition(ctx, ici, err, ""); updateErr != nil {
			log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
		}
		return ctrl.Result{}, err
	}

	// in case there is no bmh we should set requirements met condition to true with image ready message
	// in case bmh was set we will set this condition after host validations
	if ici.Spec.BareMetalHostRef == nil {
		if err := r.setImageReadyCondition(ctx, ici, nil, imageUrl); err != nil {
			log.WithError(err).Error("failed to update ImageClusterInstall status")
			return ctrl.Result{}, err
		}
	}

	if ici.Status.BareMetalHostRef != nil && !v1alpha1.BMHRefsMatch(ici.Spec.BareMetalHostRef, ici.Status.BareMetalHostRef) {
		if _, err := r.removeBMHImage(ctx, ici.Status.BareMetalHostRef); client.IgnoreNotFound(err) != nil {
			log.WithError(err).Errorf("failed to remove image from BareMetalHost %s/%s", ici.Status.BareMetalHostRef.Namespace, ici.Status.BareMetalHostRef.Name)
			return ctrl.Result{}, err
		}
	}

	if bmh != nil {
		// in case image data was changed and bmh has image url configured
		// we should remove image from it in order to invalidate ironic cache
		if bmh.Spec.Image != nil && updated {
			r.Log.Info("Image data was changed, removing image from BareMetalHost")
			removed, err := r.removeBMHImage(ctx, ici.Spec.BareMetalHostRef)
			if err != nil || removed {
				if err != nil {
					log.WithError(err).Error("failed to remove image from BareMetalHost")
				}
				return ctrl.Result{}, err
			}
		}

		res, err := r.validateSeedReconfigurationWithBMH(ctx, ici, bmh)
		if err != nil || !res.IsZero() {
			return res, err
		}

		if err := r.setBMHImage(ctx, bmh, imageUrl); err != nil {
			log.WithError(err).Error("failed to set BareMetalHost image")
			if updateErr := r.setHostConfiguredCondition(ctx, ici, err); updateErr != nil {
				log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
			}
			return ctrl.Result{}, err
		}

		if !v1alpha1.BMHRefsMatch(ici.Spec.BareMetalHostRef, ici.Status.BareMetalHostRef) {
			patch := client.MergeFrom(ici.DeepCopy())
			ici.Status.BareMetalHostRef = ici.Spec.BareMetalHostRef.DeepCopy()
			if ici.Status.BootTime.IsZero() {
				ici.Status.BootTime = metav1.Now()
			}
			r.Log.Info("Setting Status.BareMetalHostRef and installation starting condition")
			if err := r.Status().Patch(ctx, ici, patch); err != nil {
				log.WithError(err).Error("failed to set Status.BareMetalHostRef")
				return ctrl.Result{}, err
			}
		}

		timedout, err := r.checkClusterTimeout(ctx, log, ici, r.DefaultInstallTimeout)
		if err != nil {
			log.WithError(err).Error("failed to check for install timeout")
			return ctrl.Result{}, err
		}
		if timedout {
			log.Info("cluster install timed out")
			return ctrl.Result{}, nil
		}

		res, err = r.checkClusterStatus(ctx, log, ici, clusterDeployment)
		if err != nil {
			log.WithError(err).Error("failed to check cluster status")
			return ctrl.Result{}, err
		}
		return res, nil
	}

	return ctrl.Result{}, nil
}

func (r *ImageClusterInstallReconciler) validateSeedReconfigurationWithBMH(
	ctx context.Context,
	ici *v1alpha1.ImageClusterInstall,
	bmh *bmh_v1alpha1.BareMetalHost) (ctrl.Result, error) {

	// no need to validate if inspect annotation is disabled
	if bmh.ObjectMeta.Annotations != nil && bmh.ObjectMeta.Annotations[inspectAnnotation] == "disabled" {
		msg := fmt.Sprintf("inspection is disabled for BareMetalHost %s/%s, skip hardware validation", bmh.Namespace, bmh.Name)
		r.Log.Info(msg)
		if updateErr := r.setRequirementsMetCondition(ctx, ici, corev1.ConditionTrue, v1alpha1.HostValidationSucceeded, msg); updateErr != nil {
			r.Log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
		}
		return ctrl.Result{}, nil
	}

	if bmh.Status.HardwareDetails == nil {
		msg := fmt.Sprintf("hardware details not found for BareMetalHost %s/%s", bmh.Namespace, bmh.Name)
		if updateErr := r.setRequirementsMetCondition(ctx, ici, corev1.ConditionFalse, v1alpha1.HostValidationPending, msg); updateErr != nil {
			r.Log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
		}
		r.Log.Info(msg)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	var err error
	clusterInfoFilePath := ""
	defer func() {
		reason := v1alpha1.HostValidationSucceeded
		msg := v1alpha1.HostValidationsOKMsg
		if err != nil {
			reason = v1alpha1.HostValidationFailedReason
			msg = fmt.Sprintf("failed to validate host: %s", err.Error())
		}

		if updateErr := r.setRequirementsMetCondition(ctx, ici, corev1.ConditionTrue, reason, msg); updateErr != nil {
			r.Log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
		}
	}()

	clusterInfoFilePath, err = r.clusterInfoFilePath(ici)
	if err != nil {
		r.Log.WithError(err).Error("failed to read cluster info file")
		return ctrl.Result{}, err
	}
	clusterInfo := r.getClusterInfoFromFile(clusterInfoFilePath)

	err = r.validateBMHMachineNetwork(clusterInfo, *bmh.Status.HardwareDetails)
	if err != nil {
		r.Log.WithError(err).Error("failed to validate BMH machine network")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ImageClusterInstallReconciler) validateBMHMachineNetwork(
	clusterInfo *lca_api.SeedReconfiguration,
	hwDetails bmh_v1alpha1.HardwareDetails) error {
	if clusterInfo.MachineNetwork == "" {
		return nil
	}
	for _, nic := range hwDetails.NIC {
		inCIDR, _ := ipInCidr(nic.IP, clusterInfo.MachineNetwork)
		if inCIDR {
			return nil
		}
	}

	return fmt.Errorf("bmh host doesn't have any nic with ip in provided machineNetwork %s", clusterInfo.MachineNetwork)
}

func (r *ImageClusterInstallReconciler) checkClusterTimeout(ctx context.Context, log logrus.FieldLogger, ici *v1alpha1.ImageClusterInstall, defaultTimeout time.Duration) (bool, error) {
	timeout := defaultTimeout

	if timeoutOverride, present := ici.Annotations[installTimeoutAnnotation]; present {
		var err error
		timeout, err = time.ParseDuration(timeoutOverride)
		if err != nil {
			return false, fmt.Errorf("failed to parse install timeout annotation value %s: %w", timeoutOverride, err)
		}
	}

	if ici.Status.BootTime.Add(timeout).Before(time.Now()) {
		log.Error("timed out waiting for cluster to finish installation")
		err := r.setClusterTimeoutConditions(ctx, ici, timeout.String())
		if err != nil {
			log.WithError(err).Error("failed to set cluster timeout conditions")
		}
		return true, err
	}

	return false, nil
}

func (r *ImageClusterInstallReconciler) checkClusterStatus(ctx context.Context, log logrus.FieldLogger, ici *v1alpha1.ImageClusterInstall, clusterDeployment *hivev1.ClusterDeployment) (ctrl.Result, error) {
	spokeClient, err := r.spokeClient(ctx, ici)
	if err != nil {
		log.WithError(err).Error("failed to create spoke client")
		return ctrl.Result{}, err
	}

	if status := r.GetSpokeClusterInstallStatus(ctx, log, spokeClient); !status.Installed {
		log.Infof("cluster install in progress: %s", status.String())
		if err := r.setClusterInstallingConditions(ctx, ici, status.String()); err != nil {
			log.WithError(err).Error("failed to set installing conditions")
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	log.Info("cluster is installed")

	if err := r.setClusterInstalledConditions(ctx, ici); err != nil {
		log.WithError(err).Error("failed to set installed conditions")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ImageClusterInstallReconciler) spokeClient(ctx context.Context, ici *v1alpha1.ImageClusterInstall) (client.Client, error) {
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

func (r *ImageClusterInstallReconciler) mapBMHToICI(ctx context.Context, obj client.Object) []reconcile.Request {
	bmh := &bmh_v1alpha1.BareMetalHost{}
	bmhName := obj.GetName()
	bmhNamespace := obj.GetNamespace()

	if err := r.Get(ctx, types.NamespacedName{Name: bmhName, Namespace: bmhNamespace}, bmh); err != nil {
		return []reconcile.Request{}
	}
	iciList := &v1alpha1.ImageClusterInstallList{}
	if err := r.List(ctx, iciList); err != nil {
		return []reconcile.Request{}
	}
	if len(iciList.Items) == 0 {
		return []reconcile.Request{}
	}

	var requests []reconcile.Request
	for _, ici := range iciList.Items {
		if ici.Spec.BareMetalHostRef == nil {
			continue
		}
		if ici.Spec.BareMetalHostRef.Name == bmhName && ici.Spec.BareMetalHostRef.Namespace == bmhNamespace {
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: ici.Namespace,
					Name:      ici.Name,
				},
			}
			requests = append(requests, req)
		}
	}
	if len(requests) > 1 {
		r.Log.Warnf("found multiple ImageClusterInstalls referencing BaremetalHost %s/%s", bmhNamespace, bmhName)
	}
	return requests
}

func (r *ImageClusterInstallReconciler) mapCDToICI(ctx context.Context, obj client.Object) []reconcile.Request {
	cdName := obj.GetName()
	cdNamespace := obj.GetNamespace()

	cd := &hivev1.ClusterDeployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: cdName, Namespace: cdNamespace}, cd); err != nil {
		return []reconcile.Request{}
	}

	if cd.Spec.ClusterInstallRef != nil &&
		cd.Spec.ClusterInstallRef.Group == v1alpha1.Group &&
		cd.Spec.ClusterInstallRef.Kind == "ImageClusterInstall" {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Namespace: cdNamespace,
				Name:      cd.Spec.ClusterInstallRef.Name,
			},
		}}
	}

	return []reconcile.Request{}
}

func (r *ImageClusterInstallReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ImageClusterInstall{}).
		WatchesRawSource(source.Kind(mgr.GetCache(), &bmh_v1alpha1.BareMetalHost{}), handler.EnqueueRequestsFromMapFunc(r.mapBMHToICI)).
		WatchesRawSource(source.Kind(mgr.GetCache(), &hivev1.ClusterDeployment{}), handler.EnqueueRequestsFromMapFunc(r.mapCDToICI)).
		Complete(r)
}

func (r *ImageClusterInstallReconciler) setBMHImage(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost, url string) error {
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

	if bmh.Spec.AutomatedCleaningMode != bmh_v1alpha1.CleaningModeDisabled {
		bmh.Spec.AutomatedCleaningMode = bmh_v1alpha1.CleaningModeDisabled
		dirty = true
	}

	if bmh.Status.Provisioning.State == bmh_v1alpha1.StateProvisioned {
		if setAnnotaitonIfNotExists(&bmh.ObjectMeta, detachedAnnotation, detachedAnnotationValue) {
			dirty = true
		}
	}

	if dirty {
		r.Log.Infof("Setting image URL to %s for BareMetalHost %s/%s", url, bmh.Namespace, bmh.Name)
		if err := r.Patch(ctx, bmh, patch); err != nil {
			return err
		}
	}

	return nil
}

func (r *ImageClusterInstallReconciler) getBMH(ctx context.Context, bmhRef *v1alpha1.BareMetalHostReference) (*bmh_v1alpha1.BareMetalHost, error) {
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

func (r *ImageClusterInstallReconciler) removeBMHImage(ctx context.Context, bmhRef *v1alpha1.BareMetalHostReference) (bool, error) {
	bmh := &bmh_v1alpha1.BareMetalHost{}
	key := types.NamespacedName{
		Name:      bmhRef.Name,
		Namespace: bmhRef.Namespace,
	}
	if err := r.Get(ctx, key, bmh); err != nil {
		return false, err
	}
	patch := client.MergeFrom(bmh.DeepCopy())

	dirty := false
	if bmh.Spec.Image != nil {
		bmh.Spec.Image = nil
		dirty = true
	}

	if dirty {
		r.Log.Infof("Removing image from BareMetalHost %s/%s", bmh.Namespace, bmh.Name)
		if err := r.Patch(ctx, bmh, patch); err != nil {
			return false, err
		}
	}

	return dirty, nil
}

func setBackupLabel(obj client.Object) bool {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	if labels[backupLabel] == backupLabelValue {
		return false
	}

	labels[backupLabel] = backupLabelValue
	obj.SetLabels(labels)
	return true
}

func (r *ImageClusterInstallReconciler) labelConfigMapForBackup(ctx context.Context, key types.NamespacedName) error {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, key, cm); err != nil {
		return err
	}

	patch := client.MergeFrom(cm.DeepCopy())
	if setBackupLabel(cm) {
		return r.Patch(ctx, cm, patch)
	}
	return nil
}

func (r *ImageClusterInstallReconciler) labelSecretForBackup(ctx context.Context, key types.NamespacedName) error {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, key, secret); err != nil {
		return err
	}

	patch := client.MergeFrom(secret.DeepCopy())
	if setBackupLabel(secret) {
		return r.Patch(ctx, secret, patch)
	}
	return nil
}

func (r *ImageClusterInstallReconciler) labelReferencedObjectsForBackup(ctx context.Context, log logrus.FieldLogger, ici *v1alpha1.ImageClusterInstall, cd *hivev1.ClusterDeployment) {
	if ici.Spec.ClusterMetadata != nil {
		kubeconfigKey := types.NamespacedName{Name: ici.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name, Namespace: ici.Namespace}
		if err := r.labelSecretForBackup(ctx, kubeconfigKey); err != nil {
			log.WithError(err).Errorf("failed to label Secret %s for backup", kubeconfigKey)
		}
		if ici.Spec.ClusterMetadata.AdminPasswordSecretRef != nil {
			passwordKey := types.NamespacedName{Name: ici.Spec.ClusterMetadata.AdminPasswordSecretRef.Name, Namespace: ici.Namespace}
			if err := r.labelSecretForBackup(ctx, passwordKey); err != nil {
				log.WithError(err).Errorf("failed to label Secret %s for backup", passwordKey)
			}
		}
	}

	if ici.Spec.NetworkConfigRef != nil {
		networkConfigKey := types.NamespacedName{Name: ici.Spec.NetworkConfigRef.Name, Namespace: ici.Namespace}
		if err := r.labelConfigMapForBackup(ctx, networkConfigKey); err != nil {
			log.WithError(err).Errorf("failed to label ConfigMap %s for backup", networkConfigKey)
		}
	}

	if ici.Spec.CABundleRef != nil {
		caBundleKey := types.NamespacedName{Name: ici.Spec.CABundleRef.Name, Namespace: ici.Namespace}
		if err := r.labelConfigMapForBackup(ctx, caBundleKey); err != nil {
			log.WithError(err).Errorf("failed to label ConfigMap %s for backup", caBundleKey)
		}
	}

	for _, manifestRef := range ici.Spec.ExtraManifestsRefs {
		manifestKey := types.NamespacedName{Name: manifestRef.Name, Namespace: ici.Namespace}
		if err := r.labelConfigMapForBackup(ctx, manifestKey); err != nil {
			log.WithError(err).Errorf("failed to label ConfigMap %s for backup", manifestKey)
		}
	}

	if cd.Spec.PullSecretRef != nil {
		psKey := types.NamespacedName{Name: cd.Spec.PullSecretRef.Name, Namespace: cd.Namespace}
		if err := r.labelSecretForBackup(ctx, psKey); err != nil {
			log.WithError(err).Errorf("failed to label Secret %s for backup", psKey)
		}
	}
}

func (r *ImageClusterInstallReconciler) configDirs(ici *v1alpha1.ImageClusterInstall) (string, string, error) {
	lockDir := filepath.Join(r.Options.DataDir, "namespaces", ici.Namespace, ici.Name)
	filesDir := filepath.Join(lockDir, "files")
	if err := os.MkdirAll(filesDir, 0700); err != nil {
		return "", "", err
	}

	return lockDir, filesDir, nil
}

func (r *ImageClusterInstallReconciler) clusterInfoFilePath(ici *v1alpha1.ImageClusterInstall) (string, error) {
	_, filesDir, err := r.configDirs(ici)
	if err != nil {
		return "", err
	}

	return filepath.Join(filesDir, clusterConfigDir, "manifest.json"), nil
}

// writeInputData writes the required info based on the ImageClusterInstall to the config cache dir
func (r *ImageClusterInstallReconciler) writeInputData(
	ctx context.Context, log logrus.FieldLogger,
	ici *v1alpha1.ImageClusterInstall,
	cd *hivev1.ClusterDeployment,
	bmh *bmh_v1alpha1.BareMetalHost) (ctrl.Result, bool, error) {

	lockDir, filesDir, err := r.configDirs(ici)
	if err != nil {
		return ctrl.Result{}, false, err
	}
	clusterConfigPath := filepath.Join(filesDir, clusterConfigDir)
	if err := os.MkdirAll(clusterConfigPath, 0700); err != nil {
		return ctrl.Result{}, false, err
	}
	hashBeforeChanges, err := dirhash.HashDir(clusterConfigPath, "", dirhash.DefaultHash)
	if err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to hash cluster config dir: %w", err)
	}

	locked, lockErr, funcErr := filelock.WithWriteLock(lockDir, func() error {

		if err := r.writeCABundle(ctx, ici.Spec.CABundleRef, ici.Namespace, filepath.Join(clusterConfigPath, caBundleFileName)); err != nil {
			return fmt.Errorf("failed to write ca bundle: %w", err)
		}

		manifestsPath := filepath.Join(clusterConfigPath, manifestsDir)
		if err := os.MkdirAll(manifestsPath, 0700); err != nil {
			return err
		}

		psData, err := r.getValidPullSecret(ctx, cd.Spec.PullSecretRef, cd.Namespace)
		if err != nil {
			return fmt.Errorf("failed to get valid pull secret: %w", err)
		}

		if err := r.writeImageDigestSourceToFile(ici.Spec.ImageDigestSources, filepath.Join(manifestsPath, imageDigestMirrorSetFileName)); err != nil {
			return fmt.Errorf("failed to write ImageDigestSources: %w", err)
		}
		if err := r.writeInvokerCM(filepath.Join(manifestsPath, invokerCMFileName)); err != nil {
			return fmt.Errorf("failed to write invoker config map: %w", err)
		}

		if ici.Spec.ExtraManifestsRefs != nil {
			extraManifestsPath := filepath.Join(filesDir, extraManifestsDir)
			if err := os.MkdirAll(extraManifestsPath, 0700); err != nil {
				return err
			}

			for _, cmRef := range ici.Spec.ExtraManifestsRefs {
				cm := &corev1.ConfigMap{}
				key := types.NamespacedName{Name: cmRef.Name, Namespace: ici.Namespace}
				if err := r.Get(ctx, key, cm); err != nil {
					return fmt.Errorf("failed to get extraManifests config map %w", err)
				}

				for name, content := range cm.Data {
					var y interface{}
					if err := yaml.Unmarshal([]byte(content), &y); err != nil {
						return fmt.Errorf("failed to validate manifest file %s: %w", name, err)
					}
					if err := os.WriteFile(filepath.Join(extraManifestsPath, name), []byte(content), 0644); err != nil {
						return fmt.Errorf("failed to write extra manifest file: %w", err)
					}
				}
			}
		}

		clusterInfoFilePath, err := r.clusterInfoFilePath(ici)
		if err != nil {
			return err
		}
		clusterInfo := r.getClusterInfoFromFile(clusterInfoFilePath)
		if clusterInfo == nil {
			clusterInfo = &lca_api.SeedReconfiguration{}
		}

		crypto, err := r.Credentials.EnsureKubeconfigSecret(ctx, cd, clusterInfo)
		if err != nil {
			return fmt.Errorf("failed to ensure kubeconifg secret: %w", err)
		}

		kubeadminPasswordHash, err := r.Credentials.EnsureAdminPasswordSecret(ctx, cd, clusterInfo.KubeadminPasswordHash)
		if err != nil {
			return fmt.Errorf("failed to ensure admin password secret: %w", err)
		}
		if err := r.writeClusterInfo(ctx, log, ici, cd, crypto, psData, kubeadminPasswordHash, clusterInfoFilePath, clusterInfo, bmh); err != nil {
			return fmt.Errorf("failed to write cluster info: %w", err)
		}
		return nil
	})
	if lockErr != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to acquire file lock: %w", lockErr)
	}
	if funcErr != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to write input data: %w", funcErr)
	}
	if !locked {
		log.Info("requeueing due to lock contention")
		if updateErr := r.setImageReadyCondition(ctx, ici, fmt.Errorf("could not acquire lock for image data"), ""); updateErr != nil {
			log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
		}
		return ctrl.Result{RequeueAfter: time.Second * 5}, false, nil
	}

	hashAfter, err := dirhash.HashDir(clusterConfigPath, "", dirhash.DefaultHash)
	if err != nil {
		return ctrl.Result{}, false, fmt.Errorf("failed to hash cluster config dir: %w", err)
	}

	return ctrl.Result{}, hashBeforeChanges != hashAfter, nil
}

func (r *ImageClusterInstallReconciler) getClusterInfoFromFile(clusterInfoFilePath string) *lca_api.SeedReconfiguration {
	data, err := os.ReadFile(clusterInfoFilePath)
	if err != nil {
		// In case it's the first time the ICI gets reconciled the file doesn't exist
		return nil
	}
	clusterInfo := lca_api.SeedReconfiguration{}
	err = json.Unmarshal(data, &clusterInfo)
	if err != nil {
		r.Log.Warnf("failed to marshal cluster info: %w", err)
		return nil
	}
	return &clusterInfo
}

func (r *ImageClusterInstallReconciler) imageSetRegistry(ctx context.Context, ici *v1alpha1.ImageClusterInstall) (string, error) {
	cis := hivev1.ClusterImageSet{}
	key := types.NamespacedName{Name: ici.Spec.ImageSetRef.Name, Namespace: ici.Namespace}
	if err := r.Get(ctx, key, &cis); err != nil {
		return "", err
	}

	ref, err := reference.Parse(cis.Spec.ReleaseImage)
	if err != nil {
		return "", fmt.Errorf("failed to parse ReleaseImage from ClusterImageSet %s: %w", key, err)
	}

	namedRef, ok := ref.(reference.Named)
	if !ok {
		return "", fmt.Errorf("failed to parse registry name from image %s", ref)
	}

	return strings.Split(namedRef.Name(), "/")[0], nil
}

func (r *ImageClusterInstallReconciler) nmstateConfigFromBMH(ctx context.Context, bmh *bmh_v1alpha1.BareMetalHost) (string, error) {
	if bmh == nil || bmh.Spec.PreprovisioningNetworkDataName == "" {
		return "", nil
	}

	nmstateConfigSecret := &corev1.Secret{}
	key := types.NamespacedName{Name: bmh.Spec.PreprovisioningNetworkDataName, Namespace: bmh.Namespace}
	if err := r.Get(ctx, key, nmstateConfigSecret); err != nil {
		return "", fmt.Errorf("failed to get network config secret %s: %w", key, err)
	}

	nmstate, present := nmstateConfigSecret.Data[nmstateSecretKey]
	if !present {
		return "", fmt.Errorf("referenced networking ConfigMap %s does not contain the required key %s", key, nmstateCMKey)
	}

	var nmstateData map[string]any
	if err := yaml.Unmarshal(nmstate, &nmstateData); err != nil {
		return "", fmt.Errorf("failed to unmarshal nmstate data: %w", err)
	}

	return string(nmstate), nil
}

// in case bmh was configured with static networking we want to use it's configuration
// and in case it was not configured we will use configmap
func (r *ImageClusterInstallReconciler) nmstateConfig(
	ctx context.Context,
	ici *v1alpha1.ImageClusterInstall,
	bmh *bmh_v1alpha1.BareMetalHost) (string, error) {
	// in case there is configured networking on BMH we should use it
	nmstate, err := r.nmstateConfigFromBMH(ctx, bmh)
	if err != nil || nmstate != "" {
		return nmstate, err
	}

	if ici.Spec.NetworkConfigRef == nil {
		return "", nil
	}

	nmstateCM := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: ici.Spec.NetworkConfigRef.Name, Namespace: ici.Namespace}
	if err := r.Get(ctx, key, nmstateCM); err != nil {
		return "", fmt.Errorf("failed to get network config ConfigMap %s: %w", key, err)
	}

	nmstate, present := nmstateCM.Data[nmstateCMKey]
	if !present {
		return "", fmt.Errorf("referenced networking ConfigMap %s does not contain the required key %s", key, nmstateCMKey)
	}

	var nmstateData map[string]any
	if err := yaml.Unmarshal([]byte(nmstate), &nmstateData); err != nil {
		return "", fmt.Errorf("failed to unmarshal nmstate data: %w", err)
	}

	return nmstate, nil
}

func (r *ImageClusterInstallReconciler) writeClusterInfo(ctx context.Context, log logrus.FieldLogger,
	ici *v1alpha1.ImageClusterInstall, cd *hivev1.ClusterDeployment,
	KubeconfigCryptoRetention lca_api.KubeConfigCryptoRetention,
	psData, kubeadminPasswordHash, file string,
	existingInfo *lca_api.SeedReconfiguration,
	bmh *bmh_v1alpha1.BareMetalHost) error {

	nmstate, err := r.nmstateConfig(ctx, ici, bmh)
	if err != nil {
		return err
	}
	releaseRegistry, err := r.imageSetRegistry(ctx, ici)
	if err != nil {
		return err
	}
	var clusterID string
	if existingInfo != nil && existingInfo.ClusterID != "" {
		clusterID = existingInfo.ClusterID
	} else if ici.Spec.ClusterMetadata != nil {
		clusterID = ici.Spec.ClusterMetadata.ClusterID
	} else {
		clusterID = uuid.New().String()
		log.Infof("created new cluster ID %s", clusterID)
	}

	var infraID string
	if existingInfo != nil && existingInfo.InfraID != "" {
		infraID = existingInfo.InfraID
	} else if ici.Spec.ClusterMetadata != nil {
		infraID = ici.Spec.ClusterMetadata.InfraID
	} else {
		infraID = generateInfraID(cd.Spec.ClusterName)
		log.Infof("created new infra ID %s", infraID)
	}

	info := lca_api.SeedReconfiguration{
		APIVersion:                lca_api.SeedReconfigurationVersion,
		BaseDomain:                cd.Spec.BaseDomain,
		ClusterName:               cd.Spec.ClusterName,
		ClusterID:                 clusterID,
		InfraID:                   infraID,
		MachineNetwork:            ici.Spec.MachineNetwork,
		SSHKey:                    ici.Spec.SSHKey,
		ReleaseRegistry:           releaseRegistry,
		Hostname:                  ici.Spec.Hostname,
		KubeconfigCryptoRetention: KubeconfigCryptoRetention,
		PullSecret:                psData,
		RawNMStateConfig:          nmstate,
		KubeadminPasswordHash:     kubeadminPasswordHash,
		Proxy:                     r.proxy(ici.Spec.Proxy),
	}
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster info: %w", err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		return fmt.Errorf("failed to write cluster info: %w", err)
	}

	return nil
}

// all the logic of creating right noProxy is part of LCA, here we just pass it as is
func (r *ImageClusterInstallReconciler) proxy(iciProxy *v1alpha1.Proxy) *lca_api.Proxy {
	if iciProxy == nil || (iciProxy.HTTPSProxy == "" && iciProxy.HTTPProxy == "") {
		return nil
	}
	return &lca_api.Proxy{
		HTTPProxy:  iciProxy.HTTPProxy,
		HTTPSProxy: iciProxy.HTTPSProxy,
		NoProxy:    iciProxy.NoProxy,
	}
}

func (r *ImageClusterInstallReconciler) writeCABundle(ctx context.Context, ref *corev1.LocalObjectReference, ns string, file string) error {
	if ref == nil {
		return nil
	}

	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: ref.Name, Namespace: ns}
	if err := r.Get(ctx, key, cm); err != nil {
		return fmt.Errorf("failed to get CABundle config map: %w", err)
	}

	data, ok := cm.Data[caBundleFileName]
	if !ok {
		return fmt.Errorf("%s key missing from CABundle config map", caBundleFileName)
	}

	return os.WriteFile(file, []byte(data), 0644)
}

func (r *ImageClusterInstallReconciler) writeImageDigestSourceToFile(imageDigestMirrors []apicfgv1.ImageDigestMirrors, file string) error {
	if imageDigestMirrors == nil {
		return nil
	}

	imageDigestMirrorSet := &apicfgv1.ImageDigestMirrorSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apicfgv1.GroupVersion.String(),
			Kind:       "ImageDigestMirrorSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "image-digest-mirror",
			// not namespaced
		},
		Spec: apicfgv1.ImageDigestMirrorSetSpec{
			ImageDigestMirrors: imageDigestMirrors,
		},
	}

	data, err := json.Marshal(imageDigestMirrorSet)
	if err != nil {
		return fmt.Errorf("failed to marshal ImageDigestMirrorSet: %w", err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		return err
	}

	return nil
}

func (r *ImageClusterInstallReconciler) setClusterInstallMetadata(ctx context.Context, ici *v1alpha1.ImageClusterInstall, clusterDeploymentName string) error {
	clusterInfoFilePath, err := r.clusterInfoFilePath(ici)
	if err != nil {
		return err
	}
	clusterInfo := r.getClusterInfoFromFile(clusterInfoFilePath)
	if clusterInfo == nil {
		return fmt.Errorf("No cluster info found for ImageClusterInstall %s/%s", ici.Namespace, ici.Name)
	}

	kubeconfigSecret := credentials.KubeconfigSecretName(clusterDeploymentName)
	kubeadminPasswordSecret := credentials.KubeadminPasswordSecretName(clusterDeploymentName)
	if ici.Spec.ClusterMetadata != nil &&
		ici.Spec.ClusterMetadata.ClusterID == clusterInfo.ClusterID &&
		ici.Spec.ClusterMetadata.InfraID == clusterInfo.InfraID &&
		ici.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name == kubeconfigSecret &&
		ici.Spec.ClusterMetadata.AdminPasswordSecretRef.Name == kubeadminPasswordSecret {
		return nil
	}

	patch := client.MergeFrom(ici.DeepCopy())
	ici.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
		ClusterID: clusterInfo.ClusterID,
		InfraID:   clusterInfo.InfraID,
		AdminKubeconfigSecretRef: corev1.LocalObjectReference{
			Name: kubeconfigSecret,
		},
		AdminPasswordSecretRef: &corev1.LocalObjectReference{
			Name: kubeadminPasswordSecret,
		},
	}

	return r.Patch(ctx, ici, patch)
}

// Implementation from openshift-installer here: https://github.com/openshift/installer/blob/67c114a4b82ed509dc292fa81d63030c8b4118ee/pkg/asset/installconfig/clusterid.go#L60-L79
func generateInfraID(base string) string {
	// replace all characters that are not `alphanum` or `-` with `-`
	re := regexp.MustCompile("[^A-Za-z0-9-]")
	base = re.ReplaceAllString(base, "-")

	// replace all multiple dashes in a sequence with single one.
	re = regexp.MustCompile(`-{2,}`)
	base = re.ReplaceAllString(base, "-")

	maxBaseLen := 21
	// truncate to maxBaseLen
	if len(base) > maxBaseLen {
		base = base[:maxBaseLen]
	}
	base = strings.TrimRight(base, "-")

	// add random chars to the end to randomize
	return fmt.Sprintf("%s-%s", base, utilrand.String(5))
}

// getValidPullSecret validates the pull secret reference and format, return the pull secret data
func (r *ImageClusterInstallReconciler) getValidPullSecret(ctx context.Context, psRef *corev1.LocalObjectReference, namespace string) (string, error) {
	if psRef == nil || psRef.Name == "" || namespace == "" {
		return "", fmt.Errorf("missing reference to pull secret")
	}
	key := types.NamespacedName{Name: psRef.Name, Namespace: namespace}
	s := &corev1.Secret{}
	if err := r.Get(ctx, key, s); err != nil {
		return "", fmt.Errorf("failed to find secret %s: %v", key.Name, err)
	}
	psData, ok := s.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return "", fmt.Errorf("secret %s did not contain key %s", key.Name, corev1.DockerConfigJsonKey)
	}

	err := r.validatePullSecret(string(psData))
	if err != nil {
		return "", fmt.Errorf("invalid pull secret data in secret %w", err)
	}
	return string(psData), nil
}

// validatePullSecret checks if the given string is a valid image pull secret and returns an error if not.
func (r *ImageClusterInstallReconciler) validatePullSecret(pullSecret string) error {
	var s imagePullSecret

	err := json.Unmarshal([]byte(strings.TrimSpace(pullSecret)), &s)
	if err != nil {
		return fmt.Errorf("pull secret must be a well-formed JSON: %w", err)
	}

	if len(s.Auths) == 0 {
		return fmt.Errorf("pull secret must contain 'auths' JSON-object field")
	}
	errs := []error{}

	for d, a := range s.Auths {
		auth, authPresent := a["auth"]
		if !authPresent {
			errs = append(errs, fmt.Errorf("invalid pull secret: %q JSON-object requires 'auth' field", d))
			continue
		}
		data, err := base64.StdEncoding.DecodeString(auth.(string))
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid pull secret: 'auth' fields of %q are not base64-encoded", d))
			continue
		}
		res := bytes.Split(data, []byte(":"))
		if len(res) != 2 {
			errs = append(errs, fmt.Errorf("invalid pull secret: 'auth' for %s is not in 'user:password' format", d))
		}
	}
	return k8serrors.NewAggregate(errs)
}

func (r *ImageClusterInstallReconciler) handleFinalizer(ctx context.Context, log logrus.FieldLogger, ici *v1alpha1.ImageClusterInstall) (ctrl.Result, bool, error) {
	if ici.DeletionTimestamp.IsZero() {
		patch := client.MergeFrom(ici.DeepCopy())
		if controllerutil.AddFinalizer(ici, clusterInstallFinalizerName) {
			// update and requeue if the finalizer was added
			return ctrl.Result{Requeue: true}, true, r.Patch(ctx, ici, patch)
		}
		return ctrl.Result{}, false, nil
	}

	removeFinalizer := func() error {
		log.Info("removing image cluster install finalizer")
		patch := client.MergeFrom(ici.DeepCopy())
		if controllerutil.RemoveFinalizer(ici, clusterInstallFinalizerName) {
			return r.Patch(ctx, ici, patch)
		}
		return nil
	}

	lockDir, _, err := r.configDirs(ici)
	if err != nil {
		return ctrl.Result{}, true, err
	}

	if _, err := os.Stat(lockDir); err == nil {
		locked, lockErr, funcErr := filelock.WithWriteLock(lockDir, func() error {
			log.Info("removing files for image cluster install")
			return os.RemoveAll(lockDir)
		})
		if lockErr != nil {
			return ctrl.Result{}, true, fmt.Errorf("failed to acquire file lock: %w", lockErr)
		}
		if funcErr != nil {
			return ctrl.Result{}, true, fmt.Errorf("failed to write input data: %w", funcErr)
		}
		if !locked {
			log.Info("requeueing due to lock contention")
			return ctrl.Result{RequeueAfter: time.Second * 5}, true, nil
		}
	} else if !os.IsNotExist(err) {
		return ctrl.Result{}, true, fmt.Errorf("failed to stat config directory %s: %w", lockDir, err)
	}

	if bmhRef := ici.Spec.BareMetalHostRef; bmhRef != nil {
		bmh := &bmh_v1alpha1.BareMetalHost{}
		key := types.NamespacedName{
			Name:      bmhRef.Name,
			Namespace: bmhRef.Namespace,
		}
		if err := r.Get(ctx, key, bmh); err != nil {
			if !errors.IsNotFound(err) {
				return ctrl.Result{}, true, fmt.Errorf("failed to get BareMetalHost %s: %w", key, err)
			}
			log.Warnf("Referenced BareMetalHost %s does not exist", key)
			return ctrl.Result{}, true, removeFinalizer()
		}
		patch := client.MergeFrom(bmh.DeepCopy())
		if bmh.Spec.Image != nil {
			log.Infof("removing image from BareMetalHost %s", key)
			bmh.Spec.Image = nil
			if err := r.Patch(ctx, bmh, patch); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("failed to patch BareMetalHost %s: %w", key, err)
			}
		}
	}

	return ctrl.Result{}, true, removeFinalizer()
}

func (r *ImageClusterInstallReconciler) writeInvokerCM(filePath string) error {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "openshift-config",
			Name:      "openshift-install-manifests",
		},
		Data: map[string]string{
			"invoker": imageBasedInstallInvoker,
		},
	}
	data, err := json.Marshal(cm)
	if err != nil {
		return fmt.Errorf("failed to marshal openshift-install-manifests: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

func setAnnotaitonIfNotExists(meta *metav1.ObjectMeta, key string, value string) bool {
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	if _, ok := meta.Annotations[key]; !ok {
		meta.Annotations[key] = value
		return true
	}
	return false
}

func ipInCidr(ipAddr, cidr string) (bool, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, err
	}
	ip := net.ParseIP(ipAddr)
	if ip == nil {
		return false, fmt.Errorf("ip is nil")
	}
	return ipNet.Contains(ip), nil
}
