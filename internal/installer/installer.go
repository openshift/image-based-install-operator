package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-openapi/swag"
	apicfgv1 "github.com/openshift/api/config/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/installer/pkg/asset"
	"github.com/openshift/installer/pkg/asset/imagebased/configimage"
	"github.com/openshift/installer/pkg/asset/kubeconfig"
	"github.com/openshift/installer/pkg/asset/password"
	assetStore "github.com/openshift/installer/pkg/asset/store"
	"github.com/openshift/installer/pkg/ipnet"
	installertypes "github.com/openshift/installer/pkg/types"
	"github.com/openshift/installer/pkg/types/imagebased"
	"github.com/openshift/installer/pkg/types/none"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/image-based-install-operator/api/v1alpha1"
)

//go:generate mockgen -source=installer.go -package=installer -destination=mock_installer.go
type Installer interface {
	CreateInstallationIso(ctx context.Context, log logrus.FieldLogger, workDir string) error
	CreateInstallationManifest(ctx context.Context, log logrus.FieldLogger, workDir string) error
}

type installer struct {
}

func NewInstaller() Installer {
	return &installer{}
}

func (i *installer) CreateInstallationManifest(ctx context.Context, log logrus.FieldLogger, workDir string) error {
	log.Infof("Creating installation manifests from %s", workDir)
	assets := []asset.WritableAsset{
		&kubeconfig.ImageBasedAdminClient{},
		&password.KubeadminPassword{},
		&configimage.ClusterConfiguration{},
	}
	fetcher := assetStore.NewAssetsFetcher(workDir)
	return fetcher.FetchAndPersist(ctx, assets)
}

func (i *installer) CreateInstallationIso(ctx context.Context, log logrus.FieldLogger, workDir string) error {
	log.Infof("Creating installation ISO from %s", workDir)
	assets := []asset.WritableAsset{
		&configimage.ConfigImage{},
	}
	fetcher := assetStore.NewAssetsFetcher(workDir)
	return fetcher.FetchAndPersist(ctx, assets)
}

func WriteImageBaseConfig(ctx context.Context,
	ici *v1alpha1.ImageClusterInstall,
	releaseRegistry, nmstateConfig, file string) error {
	if ici.Spec.ClusterMetadata == nil || ici.Spec.ClusterMetadata.ClusterID == "" || ici.Spec.ClusterMetadata.InfraID == "" {
		return fmt.Errorf("clusterID and infraID are missing from cluster metadata")
	}

	config := &imagebased.Config{
		TypeMeta:             metav1.TypeMeta{APIVersion: imagebased.ImageBasedConfigVersion},
		Hostname:             ici.Spec.Hostname,
		ReleaseRegistry:      releaseRegistry,
		AdditionalNTPSources: ici.Spec.AdditionalNTPSources,
		NetworkConfig:        &aiv1beta1.NetConfig{Raw: []byte(nmstateConfig)},
		ClusterID:            ici.Spec.ClusterMetadata.ClusterID,
		InfraID:              ici.Spec.ClusterMetadata.InfraID,
	}

	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster info: %w", err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		return fmt.Errorf("failed to write cluster info: %w", err)
	}

	return nil
}

func WriteInstallConfig(
	ici *v1alpha1.ImageClusterInstall,
	cd *hivev1.ClusterDeployment,
	psData, caBundle, file string) error {

	installConfig := installertypes.InstallConfig{
		TypeMeta:   metav1.TypeMeta{APIVersion: installertypes.InstallConfigVersion},
		BaseDomain: cd.Spec.BaseDomain,
		ObjectMeta: metav1.ObjectMeta{Name: cd.Spec.ClusterName},
		Networking: &installertypes.Networking{NetworkType: "OVNKubernetes"},
		SSHKey:     ici.Spec.SSHKey,
		ControlPlane: &installertypes.MachinePool{
			Replicas: swag.Int64(1),
			Name:     "master",
		},
		Compute: []installertypes.MachinePool{{
			Replicas: swag.Int64(0),
			Name:     "worker",
		}},
		PullSecret:            psData,
		Proxy:                 proxy(ici.Spec.Proxy),
		AdditionalTrustBundle: caBundle,
		Platform:              installertypes.Platform{None: &none.Platform{}},
	}
	if caBundle != "" {
		installConfig.AdditionalTrustBundle = caBundle
	}

	if ici.Spec.MachineNetwork != "" {
		cidr, err := ipnet.ParseCIDR(ici.Spec.MachineNetwork)
		if err != nil {
			return fmt.Errorf("failed to parse machine network CIDR: %w", err)
		}
		installConfig.Networking.MachineNetwork = []installertypes.MachineNetworkEntry{{CIDR: *cidr}}
	}

	if ici.Spec.ImageDigestSources != nil {
		installConfig.ImageDigestSources = ConvertIDMToIDS(ici.Spec.ImageDigestSources)
	}

	data, err := json.Marshal(installConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster info: %w", err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		return fmt.Errorf("failed to write cluster info: %w", err)
	}

	return nil

}

// all the logic of creating right noProxy is part of LCA, here we just pass it as is
func proxy(iciProxy *v1alpha1.Proxy) *installertypes.Proxy {
	if iciProxy == nil || (iciProxy.HTTPSProxy == "" && iciProxy.HTTPProxy == "") {
		return nil
	}
	return &installertypes.Proxy{
		HTTPProxy:  iciProxy.HTTPProxy,
		HTTPSProxy: iciProxy.HTTPSProxy,
		NoProxy:    iciProxy.NoProxy,
	}
}

func ConvertIDMToIDS(imageDigestMirrors []apicfgv1.ImageDigestMirrors) []installertypes.ImageDigestSource {
	imageDigestSources := make([]installertypes.ImageDigestSource, len(imageDigestMirrors))
	for i, idm := range imageDigestMirrors {
		imageDigestSources[i] = installertypes.ImageDigestSource{
			Source: idm.Source,
		}
		for _, mirror := range idm.Mirrors {
			imageDigestSources[i].Mirrors = append(imageDigestSources[i].Mirrors, string(mirror))
		}
	}
	return imageDigestSources
}
