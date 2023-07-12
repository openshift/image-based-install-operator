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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	relocationv1alpha1 "github.com/carbonin/cluster-relocation-service/api/v1alpha1"
	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/sirupsen/logrus"
)

type ClusterConfigReconcilerOptions struct {
	BaseURL   string `envconfig:"BASE_URL"`
	DataDir   string `envconfig:"DATA_DIR" default:"/data"`
	ServerDir string `envconfig:"SERVER_DIR" default:"/data/server"`
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
//+kubebuilder:rbac:groups=metal3.io,resources=baremetalhosts,verbs=get;list;watch;update;patch

func (r *ClusterConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithFields(logrus.Fields{"name": req.Name, "namespace": req.Namespace})

	config := &relocationv1alpha1.ClusterConfig{}
	if err := r.Get(ctx, req.NamespacedName, config); err != nil {
		log.WithError(err).Error("failed to get referenced cluster config")
		return ctrl.Result{}, err
	}

	path := filepath.Join(req.Namespace, fmt.Sprintf("%s.iso", req.Name))
	filename := filepath.Join(r.Options.ServerDir, path)
	if err := r.createConfigISO(ctx, config, filename); err != nil {
		log.WithError(err).Error("failed to create image for config")
		return ctrl.Result{}, err
	}
	log.Info("ISO created for config")

	if r.Options.BaseURL == "" {
		log.Warn("Base URL is not set, exiting reconcile before setting URL")
		return ctrl.Result{}, nil
	}

	u, err := url.JoinPath(r.Options.BaseURL, "images", path)
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

	patch := client.MergeFrom(config.DeepCopy())
	config.Status.ImageURL = u
	if err := r.Status().Patch(ctx, config, patch); err != nil {
		log.WithError(err).Error("failed to patch cluster config")
		return ctrl.Result{}, err
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

func (r *ClusterConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := os.MkdirAll(r.Options.DataDir, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(r.Options.ServerDir, 0700); err != nil {
		return err
	}
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

	bmh.Spec.Image = &bmh_v1alpha1.Image{}
	liveIso := "live-iso"
	bmh.Spec.Online = true
	bmh.Spec.Image.URL = url
	bmh.Spec.Image.DiskFormat = &liveIso
	if err := r.Patch(ctx, bmh, patch); err != nil {
		return err
	}

	return nil
}

func (r *ClusterConfigReconciler) createConfigISO(ctx context.Context, config *relocationv1alpha1.ClusterConfig, isoPath string) error {
	// TODO: this will need sync with http server
	isoWorkDir, err := os.MkdirTemp(r.Options.DataDir, "iso-work-dir-")
	if err != nil {
		return fmt.Errorf("failed to create iso work dir: %w", err)
	}
	// if anything fails remove the workdir, if create succeeds it will remove the workdir so this will be a noop
	defer os.RemoveAll(isoWorkDir)

	if err := r.createInputData(ctx, config, isoWorkDir); err != nil {
		return fmt.Errorf("failed to write input data: %w", err)
	}
	if err := create(isoPath, isoWorkDir, "relocation-config"); err != nil {
		return fmt.Errorf("failed to create iso: %w", err)
	}
	return nil
}

// createInputData writes the required info based on the cluster config to the iso work dir
func (r *ClusterConfigReconciler) createInputData(ctx context.Context, config *relocationv1alpha1.ClusterConfig, dir string) error {
	crs := config.Spec.ClusterRelocationSpec

	data, err := json.Marshal(crs)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster relocation spec: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cluster-relocation-spec.json"), data, 0644); err != nil {
		return fmt.Errorf("failed to write cluster relocation spec: %w", err)
	}

	if err := r.writeSecretToFile(ctx, crs.APICertRef, filepath.Join(dir, "api-cert-secret.json")); err != nil {
		return fmt.Errorf("failed to write api cert secret: %w", err)
	}

	if err := r.writeSecretToFile(ctx, crs.IngressCertRef, filepath.Join(dir, "ingress-cert-secret.json")); err != nil {
		return fmt.Errorf("failed to write ingress cert secret: %w", err)
	}

	if err := r.writeSecretToFile(ctx, crs.PullSecretRef, filepath.Join(dir, "pull-secret-secret.json")); err != nil {
		return fmt.Errorf("failed to write pull secret: %w", err)
	}

	// TODO: create network config when we know what this looks like
	// no sense in spending time working on a CM if it's not going to be one in the end

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

// create builds an iso file at outPath with the given volumeLabel using the contents of the working directory
func create(outPath string, workDir string, volumeLabel string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0700); err != nil {
		return err
	}

	// Use the minimum iso size that will satisfy diskfs validations here.
	// This value doesn't determine the final image size, but is used
	// to truncate the initial file. This value would be relevant if
	// we were writing to a particular partition on a device, but we are
	// not so the minimum iso size will work for us here
	minISOSize := 38 * 1024
	d, err := diskfs.Create(outPath, int64(minISOSize), diskfs.Raw, diskfs.SectorSizeDefault)
	if err != nil {
		return err
	}

	d.LogicalBlocksize = 2048
	fspec := disk.FilesystemSpec{
		Partition:   0,
		FSType:      filesystem.TypeISO9660,
		VolumeLabel: volumeLabel,
		WorkDir:     workDir,
	}
	fs, err := d.CreateFilesystem(fspec)
	if err != nil {
		return err
	}

	iso, ok := fs.(*iso9660.FileSystem)
	if !ok {
		return fmt.Errorf("not an iso9660 filesystem")
	}

	options := iso9660.FinalizeOptions{
		RockRidge:        true,
		VolumeIdentifier: volumeLabel,
	}

	return iso.Finalize(options)
}
