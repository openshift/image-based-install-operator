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
	"strings"
	"time"

	// These are required for image parsing to work correctly with digest-based pull specs
	// See: https://github.com/opencontainers/go-digest/blob/v1.0.0/README.md#usage
	_ "crypto/sha256"
	_ "crypto/sha512"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/containers/image/v5/docker/reference"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	lca_api "github.com/openshift-kni/lifecycle-agent/api/seedreconfig"
	"github.com/openshift/cluster-relocation-service/api/v1alpha1"
	"github.com/openshift/cluster-relocation-service/internal/certs"
	"github.com/openshift/cluster-relocation-service/internal/filelock"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/sirupsen/logrus"
)

type ImageClusterInstallReconcilerOptions struct {
	ServiceName      string `envconfig:"SERVICE_NAME"`
	ServiceNamespace string `envconfig:"SERVICE_NAMESPACE"`
	ServicePort      string `envconfig:"SERVICE_PORT"`
	ServiceScheme    string `envconfig:"SERVICE_SCHEME"`
	DataDir          string `envconfig:"DATA_DIR" default:"/data"`
}

// ImageClusterInstallReconciler reconciles a ImageClusterInstall object
type ImageClusterInstallReconciler struct {
	client.Client
	Log         logrus.FieldLogger
	Scheme      *runtime.Scheme
	Options     *ImageClusterInstallReconcilerOptions
	BaseURL     string
	CertManager certs.KubeConfigCertManager
}

const (
	detachedAnnotation          = "baremetalhost.metal3.io/detached"
	clusterConfigDir            = "cluster-configuration"
	extraManifestsDir           = "extra-manifests"
	manifestsDir                = "manifests"
	networkConfigDir            = "network-configuration"
	clusterInstallFinalizerName = "imageclusterinstall." + v1alpha1.Group + "/deprovision"
	caBundleFileName            = "tls-ca-bundle.pem"
)

//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=imageclusterinstalls,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=imageclusterinstalls/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=extensions.hive.openshift.io,resources=imageclusterinstalls/finalizers,verbs=update
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=hive.openshift.io,resources=clusterdeployments,verbs=get;list;watch;update;patch
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
		log.WithError(err).Errorf("failed to get ClusterDeployment %s", cdKey)
		return ctrl.Result{}, err
	}

	if res, err := r.writeInputData(ctx, log, ici, clusterDeployment); !res.IsZero() || err != nil {
		if err != nil {
			if updateErr := r.setImageReadyCondition(ctx, ici, err); updateErr != nil {
				log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
			}
			log.Error(err)
		}
		return res, err
	}

	imageUrl, err := url.JoinPath(r.BaseURL, "images", req.Namespace, fmt.Sprintf("%s.iso", req.Name))
	if err != nil {
		log.WithError(err).Error("failed to create image url")
		if updateErr := r.setImageReadyCondition(ctx, ici, err); updateErr != nil {
			log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
		}
		return ctrl.Result{}, err
	}

	if err := r.setImageReadyCondition(ctx, ici, nil); err != nil {
		log.WithError(err).Error("failed to update ImageClusterInstall status")
		return ctrl.Result{}, err
	}

	if ici.Status.BareMetalHostRef != nil && !v1alpha1.BMHRefsMatch(ici.Spec.BareMetalHostRef, ici.Status.BareMetalHostRef) {
		if err := r.removeBMHImage(ctx, ici.Status.BareMetalHostRef); client.IgnoreNotFound(err) != nil {
			log.WithError(err).Errorf("failed to remove image from BareMetalHost %s/%s", ici.Status.BareMetalHostRef.Namespace, ici.Status.BareMetalHostRef.Name)
			return ctrl.Result{}, err
		}
	}

	if ici.Spec.BareMetalHostRef != nil {
		if err := r.setBMHImage(ctx, ici.Spec.BareMetalHostRef, imageUrl); err != nil {
			log.WithError(err).Error("failed to set BareMetalHost image")
			if updateErr := r.setHostConfiguredCondition(ctx, ici, err); updateErr != nil {
				log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
			}
			return ctrl.Result{}, err
		}
		if err := r.setHostConfiguredCondition(ctx, ici, nil); err != nil {
			log.WithError(err).Error("failed to update ImageClusterInstall status")
			return ctrl.Result{}, err
		}

		patch := client.MergeFrom(ici.DeepCopy())
		ici.Status.BareMetalHostRef = ici.Spec.BareMetalHostRef.DeepCopy()
		if err := r.Status().Patch(ctx, ici, patch); err != nil {
			log.WithError(err).Error("failed to set Status.BareMetalHostRef")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ImageClusterInstallReconciler) setImageReadyCondition(ctx context.Context, ici *v1alpha1.ImageClusterInstall, err error) error {
	cond := metav1.Condition{
		Type:    v1alpha1.ImageReadyCondition,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.ImageReadyReason,
		Message: v1alpha1.ImageReadyMessage,
	}

	if err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = v1alpha1.ImageNotReadyReason
		cond.Message = err.Error()
	}

	patch := client.MergeFrom(ici.DeepCopy())
	meta.SetStatusCondition(&ici.Status.ConfigConditions, cond)
	return r.Status().Patch(ctx, ici, patch)
}

func (r *ImageClusterInstallReconciler) setHostConfiguredCondition(ctx context.Context, ici *v1alpha1.ImageClusterInstall, err error) error {
	cond := metav1.Condition{
		Type:    v1alpha1.HostConfiguredCondition,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.HostConfiguraionSucceededReason,
		Message: v1alpha1.HostConfigurationSucceededMessage,
	}

	if err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = v1alpha1.HostConfiguraionFailedReason
		cond.Message = err.Error()
	}

	patch := client.MergeFrom(ici.DeepCopy())
	meta.SetStatusCondition(&ici.Status.ConfigConditions, cond)
	return r.Status().Patch(ctx, ici, patch)
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

	requests := []reconcile.Request{}
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
		r.Log.Warn("found multiple ImageClusterInstalls referencing BaremetalHost %s/%s", bmhNamespace, bmhName)
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

func serviceURL(opts *ImageClusterInstallReconcilerOptions) string {
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

func (r *ImageClusterInstallReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Options.ServiceName == "" || r.Options.ServiceNamespace == "" || r.Options.ServiceScheme == "" {
		return fmt.Errorf("SERVICE_NAME, SERVICE_NAMESPACE, and SERVICE_SCHEME must be set")
	}
	r.BaseURL = serviceURL(r.Options)

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ImageClusterInstall{}).
		WatchesRawSource(source.Kind(mgr.GetCache(), &bmh_v1alpha1.BareMetalHost{}), handler.EnqueueRequestsFromMapFunc(r.mapBMHToICI)).
		WatchesRawSource(source.Kind(mgr.GetCache(), &hivev1.ClusterDeployment{}), handler.EnqueueRequestsFromMapFunc(r.mapCDToICI)).
		Complete(r)
}

func (r *ImageClusterInstallReconciler) setBMHImage(ctx context.Context, bmhRef *v1alpha1.BareMetalHostReference, url string) error {
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

	if bmh.Status.Provisioning.State == bmh_v1alpha1.StateProvisioned {
		if bmh.ObjectMeta.Annotations == nil {
			bmh.ObjectMeta.Annotations = make(map[string]string)
		}
		bmh.ObjectMeta.Annotations[detachedAnnotation] = "imageclusterinstall-controller"
		dirty = true
	}

	if dirty {
		if err := r.Patch(ctx, bmh, patch); err != nil {
			return err
		}
	}

	return nil
}

func (r *ImageClusterInstallReconciler) removeBMHImage(ctx context.Context, bmhRef *v1alpha1.BareMetalHostReference) error {
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
	if bmh.Spec.Image != nil {
		bmh.Spec.Image = nil
		dirty = true
	}

	if dirty {
		if err := r.Patch(ctx, bmh, patch); err != nil {
			return err
		}
	}

	return nil
}

func (r *ImageClusterInstallReconciler) configDirs(ici *v1alpha1.ImageClusterInstall) (string, string, error) {
	lockDir := filepath.Join(r.Options.DataDir, "namespaces", ici.Namespace, ici.Name)
	filesDir := filepath.Join(lockDir, "files")
	if err := os.MkdirAll(filesDir, 0700); err != nil {
		return "", "", err
	}

	return lockDir, filesDir, nil
}

// writeInputData writes the required info based on the ImageClusterInstall to the config cache dir
func (r *ImageClusterInstallReconciler) writeInputData(ctx context.Context, log logrus.FieldLogger, ici *v1alpha1.ImageClusterInstall, cd *hivev1.ClusterDeployment) (ctrl.Result, error) {
	lockDir, filesDir, err := r.configDirs(ici)
	if err != nil {
		return ctrl.Result{}, err
	}
	clusterConfigPath := filepath.Join(filesDir, clusterConfigDir)
	if err := os.MkdirAll(clusterConfigPath, 0700); err != nil {
		return ctrl.Result{}, err
	}

	locked, lockErr, funcErr := filelock.WithWriteLock(lockDir, func() error {

		if err := r.writeCABundle(ctx, ici.Spec.CABundleRef, ici.Namespace, filepath.Join(clusterConfigPath, caBundleFileName)); err != nil {
			return fmt.Errorf("failed to write ca bundle: %w", err)
		}

		manifestsPath := filepath.Join(clusterConfigPath, manifestsDir)
		if err := os.MkdirAll(manifestsPath, 0700); err != nil {
			return err
		}

		if err := r.writePullSecretToFile(ctx, cd.Spec.PullSecretRef, ici.Namespace, filepath.Join(manifestsPath, "pull-secret-secret.json")); err != nil {
			return fmt.Errorf("failed to write pull secret: %w", err)
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

		if ici.Spec.NetworkConfigRef != nil {
			networkConfigPath := filepath.Join(filesDir, networkConfigDir)
			if err := os.MkdirAll(networkConfigPath, 0700); err != nil {
				return err
			}

			cm := &corev1.ConfigMap{}
			key := types.NamespacedName{Name: ici.Spec.NetworkConfigRef.Name, Namespace: ici.Namespace}
			if err := r.Get(ctx, key, cm); err != nil {
				return err
			}

			for name, content := range cm.Data {
				if !strings.HasSuffix(name, ".nmconnection") {
					log.Warnf("Ignoring file name %s without .nmconnection suffix", name)
					continue
				}
				if err := os.WriteFile(filepath.Join(networkConfigPath, name), []byte(content), 0644); err != nil {
					return fmt.Errorf("failed to write network connection file: %w", err)
				}
			}
		}

		crypto, err := r.generateClusterCrypto(cd, ctx)
		if err != nil {
			return err
		}
		if err := r.writeClusterInfo(ctx, ici, cd, crypto, filepath.Join(clusterConfigPath, "manifest.json")); err != nil {
			return fmt.Errorf("failed to write cluster info: %w", err)
		}
		return nil
	})
	if lockErr != nil {
		return ctrl.Result{}, fmt.Errorf("failed to acquire file lock: %w", lockErr)
	}
	if funcErr != nil {
		return ctrl.Result{}, fmt.Errorf("failed to write input data: %w", funcErr)
	}
	if !locked {
		log.Info("requeueing due to lock contention")
		if updateErr := r.setImageReadyCondition(ctx, ici, fmt.Errorf("could not acquire lock for image data")); updateErr != nil {
			log.WithError(updateErr).Error("failed to update ImageClusterInstall status")
		}
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ImageClusterInstallReconciler) generateClusterCrypto(cd *hivev1.ClusterDeployment, ctx context.Context) (lca_api.KubeConfigCryptoRetention, error) {
	// TODO: handle user provided API and ingress certs
	// TODO: consider optimizing this code by skipping the cert generation if the cluster URL is the same
	if err := r.CertManager.GenerateAllCertificates(); err != nil {
		return lca_api.KubeConfigCryptoRetention{}, fmt.Errorf("failed to generate certificates: %w", err)
	}
	kubeconfigBytes, err := r.CertManager.GenerateKubeConfig(fmt.Sprintf("%s.%s", cd.Spec.ClusterName, cd.Spec.BaseDomain))
	if err != nil {
		return lca_api.KubeConfigCryptoRetention{}, fmt.Errorf("failed to generate kubeconfig: %w", err)
	}
	err = r.CreateKubeconfigSecret(ctx, cd, kubeconfigBytes)
	if err != nil {
		return lca_api.KubeConfigCryptoRetention{}, fmt.Errorf("failed to creata kubeconfig secret: %w", err)
	}
	return r.CertManager.GetCrypto(), nil
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

func (r *ImageClusterInstallReconciler) writeClusterInfo(ctx context.Context, ici *v1alpha1.ImageClusterInstall, cd *hivev1.ClusterDeployment, KubeconfigCryptoRetention lca_api.KubeConfigCryptoRetention, file string) error {
	releaseRegistry, err := r.imageSetRegistry(ctx, ici)
	if err != nil {
		return err
	}

	info := lca_api.SeedReconfiguration{
		APIVersion:                lca_api.SeedReconfigurationVersion,
		BaseDomain:                cd.Spec.BaseDomain,
		ClusterName:               cd.Spec.ClusterName,
		NodeIP:                    ici.Spec.MasterIP,
		ReleaseRegistry:           releaseRegistry,
		Hostname:                  ici.Spec.Hostname,
		KubeconfigCryptoRetention: KubeconfigCryptoRetention,
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

func (r *ImageClusterInstallReconciler) CreateKubeconfigSecret(ctx context.Context, cd *hivev1.ClusterDeployment, kubeconfigBytes []byte) error {
	kubeconfigSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cd.Spec.ClusterName + "-admin-kubeconfig",
			Namespace: cd.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
	}
	mutateFn := func() error {
		// Update the Secret object with the desired data
		kubeconfigSecret.Data = map[string][]byte{
			"kubeconfig": kubeconfigBytes,
		}
		return nil
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, kubeconfigSecret, mutateFn)
	if err != nil {
		return fmt.Errorf("failed to create kubeconfig secret: %w", err)
	}
	r.Log.Infof("kubeconfig secret %s", op)
	return nil
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

func (r *ImageClusterInstallReconciler) writePullSecretToFile(ctx context.Context, ref *corev1.LocalObjectReference, ns string, file string) error {
	if ref == nil {
		return nil
	}

	s := &corev1.Secret{}
	key := types.NamespacedName{Name: ref.Name, Namespace: ns}
	if err := r.Get(ctx, key, s); err != nil {
		return err
	}

	// override name and namespace
	s.Name = "pull-secret"
	s.Namespace = "openshift-config"

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		return err
	}

	return nil
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
			log.Info("removing image from BareMetalHost %s", key)
			bmh.Spec.Image = nil
			if err := r.Patch(ctx, bmh, patch); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("failed to patch BareMetalHost %s: %w", key, err)
			}
		}
	}

	return ctrl.Result{}, true, removeFinalizer()
}
