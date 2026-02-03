package installer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/image-based-install-operator/internal/credentials"
	"github.com/openshift/installer/pkg/types/imagebased"
)

const validImageBasedConfig = `
apiVersion: v1beta1
kind: ImageBasedConfig
metadata:
  name: example-image-based-config
hostname: ostest-extraworker-0
releaseRegistry: quay.io
cluster_id: e6a68c95-ee5c-4edd-9bb1-8141ac3eb348
infra_id: test-sktvm
`

const validInstallConfig = `
apiVersion: v1
baseDomain: example.com
compute:
- name: worker
  platform: {}
  replicas: 0
  architecture: amd64
controlPlane:
  architecture: amd64
  name: master
  platform: {}
  replicas: 1
metadata:
  creationTimestamp: null
  name: test
networking:
  networkType: OVNKubernetes
platform:
  none: {}
pullSecret: '{"auths":{"quay.io":{"auth":"dXNlcjpwYXNzCg=="}}}'
`

//nolint:gosec // fake credentials for testing
const secretSeedReconfig = `
{
  "api_version": 1,
  "base_domain": "example.com",
  "cluster_id": "e6a68c95-ee5c-4edd-9bb1-8141ac3eb348",
  "cluster_name": "test",
  "hostname": "ostest-extraworker-0",
  "infra_id": "test-sktvm",
  "kubeadmin_password_hash": "mypasswordhash",
  "KubeconfigCryptoRetention": {
    "KubeAPICrypto": {
      "ServingCrypto": {
        "localhost_signer_private_key": "mylocalhostsignerprivatekey",
        "service_network_signer_private_key": "myservicenetworksignerprivatekey",
        "loadbalancer_external_signer_private_key": "myloadbalancerexternalsignerprivatekey"
      },
      "ClientAuthCrypto": {
        "admin_ca_certificate": "myadmincacertificate"
      }
    },
    "IngresssCrypto": {
      "ingress_ca": "myingressca",
      "ingress_certificate_cn": "myingresscertificatecn"
    }
  },
  "release_registry": "quay.io",
  "pull_secret": "{\"auths\":{\"quay.io\":{\"auth\":\"b2xkdXNlcjpvbGRwYXNzCg==\"}}}"
}
`

var _ = Describe("WriteReinstallData", func() {
	var (
		inst       Installer
		tmpWorkDir string
		isoWorkDir string
	)

	BeforeEach(func() {
		inst = &installer{}
		var err error
		tmpWorkDir, err = os.MkdirTemp("", "installer-test-tempworkdir-")
		Expect(err).NotTo(HaveOccurred())
		isoWorkDir, err = os.MkdirTemp("", "installer-test-isoworkdir-")
		Expect(err).NotTo(HaveOccurred())

		Expect(os.WriteFile(filepath.Join(tmpWorkDir, "image-based-config.yaml"), []byte(validImageBasedConfig), 0644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmpWorkDir, "install-config.yaml"), []byte(validInstallConfig), 0644)).To(Succeed())
	})

	AfterEach(func() {
		os.RemoveAll(tmpWorkDir)
		os.RemoveAll(isoWorkDir)
	})

	It("writes the auth files in the iso workdir", func() {
		idData := credentials.IdentityData{
			Kubeconfig:        []byte("kubeconfig"),
			KubeadminPassword: []byte("password"),
			SeedReconfig:      []byte(secretSeedReconfig),
		}

		Expect(inst.WriteReinstallData(context.Background(), tmpWorkDir, isoWorkDir, idData)).To(Succeed())

		kubeconfigContent, err := os.ReadFile(filepath.Join(isoWorkDir, "auth", "kubeconfig"))
		Expect(err).NotTo(HaveOccurred())
		Expect(kubeconfigContent).To(Equal(idData.Kubeconfig))
		kubeadmPasswordContent, err := os.ReadFile(filepath.Join(isoWorkDir, "auth", "kubeadmin-password"))
		Expect(err).NotTo(HaveOccurred())
		Expect(kubeadmPasswordContent).To(Equal(idData.KubeadminPassword))
	})

	It("overwrites the seed reconfig crypto with what is passed", func() {
		idData := credentials.IdentityData{
			Kubeconfig:        []byte("kubeconfig"),
			KubeadminPassword: []byte("password"),
			SeedReconfig:      []byte(secretSeedReconfig),
		}

		Expect(inst.WriteReinstallData(context.Background(), tmpWorkDir, isoWorkDir, idData)).To(Succeed())
		seedReconfigContent, err := os.ReadFile(filepath.Join(isoWorkDir, "cluster-configuration", "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		seedReconfig := imagebased.SeedReconfiguration{}
		Expect(json.Unmarshal(seedReconfigContent, &seedReconfig)).To(Succeed())

		Expect(seedReconfig.KubeadminPasswordHash).To(Equal("mypasswordhash"))
		expectedCrypto := imagebased.KubeConfigCryptoRetention{
			KubeAPICrypto: imagebased.KubeAPICrypto{
				ServingCrypto: imagebased.ServingCrypto{
					LocalhostSignerPrivateKey:      "mylocalhostsignerprivatekey",
					ServiceNetworkSignerPrivateKey: "myservicenetworksignerprivatekey",
					LoadbalancerSignerPrivateKey:   "myloadbalancerexternalsignerprivatekey",
				},
				ClientAuthCrypto: imagebased.ClientAuthCrypto{
					AdminCACertificate: "myadmincacertificate",
				},
			},
			IngresssCrypto: imagebased.IngresssCrypto{
				IngressCAPrivateKey:  "myingressca",
				IngressCertificateCN: "myingresscertificatecn",
			},
		}
		Expect(seedReconfig.KubeconfigCryptoRetention).To(Equal(expectedCrypto))
	})

	It("maintains new values from config", func() {
		idData := credentials.IdentityData{
			Kubeconfig:        []byte("kubeconfig"),
			KubeadminPassword: []byte("password"),
			SeedReconfig:      []byte(secretSeedReconfig),
		}

		Expect(inst.WriteReinstallData(context.Background(), tmpWorkDir, isoWorkDir, idData)).To(Succeed())
		seedReconfigContent, err := os.ReadFile(filepath.Join(isoWorkDir, "cluster-configuration", "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		seedReconfig := imagebased.SeedReconfiguration{}
		Expect(json.Unmarshal(seedReconfigContent, &seedReconfig)).To(Succeed())

		// note this is the value from the installconfig which is different from the secret seed reconfig
		Expect(seedReconfig.PullSecret).To(Equal("{\"auths\":{\"quay.io\":{\"auth\":\"dXNlcjpwYXNzCg==\"}}}"))
	})

	DescribeTable("reinstall validations",
		func(reconfigKey, reconfigValue, errSubstring string) {
			seedReconfig := map[string]interface{}{}
			Expect(json.Unmarshal([]byte(secretSeedReconfig), &seedReconfig)).To(Succeed())
			seedReconfig[reconfigKey] = interface{}(reconfigValue)
			data, err := json.Marshal(seedReconfig)
			Expect(err).ToNot(HaveOccurred())

			idData := credentials.IdentityData{
				Kubeconfig:        []byte("kubeconfig"),
				KubeadminPassword: []byte("password"),
				SeedReconfig:      []byte(data), //nolint:unconvert
			}

			err = inst.WriteReinstallData(context.Background(), tmpWorkDir, isoWorkDir, idData)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(errSubstring))
		},
		Entry("check base domain has not changed", "base_domain", "example.org", "provided base domain (example.com) must match previous base domain (example.org)"),
		Entry("check cluster name has not changed", "cluster_name", "mycluster", "provided cluster name (test) must match previous cluster name (mycluster)"),
	)
})
