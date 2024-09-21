package installer

import (
	"context"

	"github.com/openshift/installer/pkg/asset"
	"github.com/openshift/installer/pkg/asset/imagebased/configimage"
	"github.com/openshift/installer/pkg/asset/kubeconfig"
	"github.com/openshift/installer/pkg/asset/password"
	assetStore "github.com/openshift/installer/pkg/asset/store"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen --build_flags=--mod=mod -package=installer -destination=mock_installer.go . Installer
type Installer interface {
	CreateInstallationIso(ctx context.Context, log logrus.FieldLogger, workDir string) error
}

type installer struct {
	assets []asset.WritableAsset
}

func NewInstaller() Installer {
	return &installer{assets: []asset.WritableAsset{
		&configimage.ConfigImage{},
		&kubeconfig.ImageBasedAdminClient{},
		&password.KubeadminPassword{},
	}}
}

func (i *installer) CreateInstallationIso(ctx context.Context, log logrus.FieldLogger, workDir string) error {
	log.Info("Creating installation ISO from %s", workDir)
	fetcher := assetStore.NewAssetsFetcher(workDir)
	return fetcher.FetchAndPersist(ctx, i.assets)
}
