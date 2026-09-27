package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/openshift/image-based-install-operator/internal/credentials"
	"github.com/openshift/installer/pkg/asset/imagebased/configimage"
	"github.com/openshift/installer/pkg/asset/kubeconfig"
	"github.com/openshift/installer/pkg/asset/password"
	"github.com/openshift/installer/pkg/asset/store"
	"github.com/openshift/installer/pkg/types/imagebased"
)

// WriteReinstallData writes out the SeedReconfiguration and auth data required for a reinstall to `isoWorkDir`
// `tmpWorkDir` must contain an image-based-config and install-config file prior to calling this function
// Crypto data from `seedReconfigData` will be merged into the SeedReconfiguration created based on the config files in `tmpWorkDir`
func (i *installer) WriteReinstallData(ctx context.Context, tmpWorkDir, isoWorkDir string, idData credentials.IdentityData) error {
	clusterConfig, kubeconfigAssetPath, kubeadmPasswordAssetPath, err := getBaseAssets(ctx, tmpWorkDir)
	if err != nil {
		return err
	}

	secretSeedReconfig := imagebased.SeedReconfiguration{}
	if err := json.Unmarshal(idData.SeedReconfig, &secretSeedReconfig); err != nil {
		return fmt.Errorf("failed to decode local seed reconfiguration: %w", err)
	}

	if err := validateReinstallConfig(clusterConfig.Config, &secretSeedReconfig); err != nil {
		return fmt.Errorf("cluster configuration is not valid for reinstall: %w", err)
	}

	clusterConfig.Config.KubeadminPasswordHash = secretSeedReconfig.KubeadminPasswordHash
	clusterConfig.Config.KubeconfigCryptoRetention = secretSeedReconfig.KubeconfigCryptoRetention

	if err := writeSeedReconfig(isoWorkDir, clusterConfig); err != nil {
		return err
	}

	kubeconfigPath := filepath.Join(isoWorkDir, kubeconfigAssetPath)
	if err := writeAuth(kubeconfigPath, idData.Kubeconfig); err != nil {
		return err
	}

	kubeadmPasswordPath := filepath.Join(isoWorkDir, kubeadmPasswordAssetPath)
	if err := writeAuth(kubeadmPasswordPath, idData.KubeadminPassword); err != nil {
		return err
	}

	return nil
}

func validateReinstallConfig(newConfig, secretConfig *imagebased.SeedReconfiguration) error {
	if newConfig.BaseDomain != secretConfig.BaseDomain {
		return fmt.Errorf("provided base domain (%s) must match previous base domain (%s)", newConfig.BaseDomain, secretConfig.BaseDomain)
	}
	if newConfig.ClusterName != secretConfig.ClusterName {
		return fmt.Errorf("provided cluster name (%s) must match previous cluster name (%s)", newConfig.ClusterName, secretConfig.ClusterName)
	}

	return nil
}

func writeAuth(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write auth data to %s: %w", path, err)
	}

	return nil
}

func writeSeedReconfig(isoWorkDir string, clusterConfig *configimage.ClusterConfiguration) error {
	updatedSeedReconfigurationData, err := json.Marshal(clusterConfig.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated seed reconfiguration data: %w", err)
	}
	reconfigFile := filepath.Join(isoWorkDir, clusterConfig.File.Filename)
	if err := os.MkdirAll(filepath.Dir(reconfigFile), 0755); err != nil {
		return fmt.Errorf("failed to create reconfig dir: %w", err)
	}
	if err := os.WriteFile(reconfigFile, updatedSeedReconfigurationData, 0o640); err != nil {
		return fmt.Errorf("failed to write updated seed reconfig: %w", err)
	}

	return nil
}

// getBaseAssets creates a ClusterConfiguration given a working directory containing an image based config and an install config
// it also returns the relative asset paths for the auth files so that the correct auth can be written to those paths
func getBaseAssets(ctx context.Context, workdir string) (*configimage.ClusterConfiguration, string, string, error) {
	s, err := store.NewStore(workdir)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to create asset store: %w", err)
	}

	config := &configimage.ClusterConfiguration{}
	if err := s.Fetch(ctx, config); err != nil {
		return nil, "", "", fmt.Errorf("failed to fetch cluster config asset from store: %w", err)
	}

	kubeconfig := &kubeconfig.ImageBasedAdminClient{}
	if err := s.Fetch(ctx, kubeconfig); err != nil {
		return nil, "", "", fmt.Errorf("failed to fetch kubeconfig asset from store: %w", err)
	}

	kubeadminpassword := &password.KubeadminPassword{}
	if err := s.Fetch(ctx, kubeadminpassword); err != nil {
		return nil, "", "", fmt.Errorf("failed to fetch kubeadmin password asset from store: %w", err)
	}

	return config, kubeconfig.File.Filename, kubeadminpassword.File.Filename, nil
}
