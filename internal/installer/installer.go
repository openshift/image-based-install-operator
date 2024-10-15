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

//go:generate mockgen -source=installer.go -package=installer -destination=mock_installer.go
type Installer interface {
	CreateInstallationIso(ctx context.Context, log logrus.FieldLogger, workDir string) error
}

type installer struct {
}

func NewInstaller() Installer {
	return &installer{}
}

func (i *installer) CreateInstallationIso(ctx context.Context, log logrus.FieldLogger, workDir string) error {
	log.Infof("Creating installation ISO from %s", workDir)
	assets := []asset.WritableAsset{
		&configimage.ConfigImage{},
		&kubeconfig.ImageBasedAdminClient{},
		&password.KubeadminPassword{}}
	fetcher := assetStore.NewAssetsFetcher(workDir)
	return fetcher.FetchAndPersist(ctx, assets)
}
