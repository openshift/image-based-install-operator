package credentials

import (
	"context"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	kubeconfigData   = "kubeconfig"
	kubeAdminData    = "kubeadmin"
	seedReconfigData = `{
  "additionalTrustBundle": {
    "userCaBundle": "",
    "proxyConfigmapName": "",
    "proxyConfigmapBundle": ""
  },
  "api_version": 1,
  "base_domain": "example.com",
  "cluster_id": "af5f4671-453c-4a4a-8b2b-bacf70552030",
  "cluster_name": "ibiotest",
  "hostname": "ibitesthost",
  "infra_id": "ibiotest-67vn4",
  "kubeadmin_password_hash": "mypasswordhash",
  "KubeconfigCryptoRetention": {
    "KubeAPICrypto": {
      "ServingCrypto": {
        "localhost_signer_private_key": "localhostsignerprivatekey",
        "service_network_signer_private_key": "servicenetworksignerprivatekey",
        "loadbalancer_external_signer_private_key": "loadabalancerexternalsignerprivatekey"
      },
      "ClientAuthCrypto": {
        "admin_ca_certificate": "admincacertificate"
      }
    },
    "IngresssCrypto": {
      "ingress_ca": "ingressca",
      "ingress_certificate_cn": "ingress-operator@1730475501"
    }
  },
  "release_registry": "quay.io",
  "pull_secret": "{\"auths\":{\"quay.io\":{\"auth\":\"cGFzc3dvcmQK\",\"email\":\"test@example.com\"}}}\n"
}`
)

var _ = Describe("Credentials", func() {
	var (
		c                          client.Client
		cm                         Credentials
		clusterDeployment          *hivev1.ClusterDeployment
		clusterDeploymentName      = "test-cluster"
		clusterDeploymentNamespace = "test-namespace"
		clusterName                = "sno"
		baseDomain                 = "redhat.com"
		ctx                        = context.Background()
		kubeconfigFile             = ""
		kubeAdminFile              = ""
		seedReconfigurationFile    = ""
		log                        = logrus.FieldLogger(logrus.New())
	)
	_ = v1alpha1.AddToScheme(scheme.Scheme)
	_ = hivev1.AddToScheme(scheme.Scheme)
	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()
		var err error
		Expect(err).NotTo(HaveOccurred())
		cm = Credentials{
			Client: c,
			Log:    logrus.New(),
			Scheme: scheme.Scheme,
		}
		clusterDeployment = &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterDeploymentName,
				Namespace: clusterDeploymentNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{
				ClusterName: clusterName,
				BaseDomain:  baseDomain,
			},
		}
		kubeconfigFile, err = createTempFile("kubeconfig", kubeconfigData)
		Expect(err).NotTo(HaveOccurred())

		kubeAdminFile, err = createTempFile("kubeadmin", kubeAdminData)
		Expect(err).NotTo(HaveOccurred())

		seedReconfigurationFile, err = createTempFile("manifest.json", seedReconfigData)
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func() {
		os.Remove(kubeconfigFile)
	})

	It("EnsureKubeconfigSecret success", func() {
		err := cm.EnsureKubeconfigSecret(ctx, log, clusterDeployment, kubeconfigFile)
		Expect(err).NotTo(HaveOccurred())
		verifyKubeconfigSecret(ctx, cm.Client, clusterDeployment, kubeconfigData)
	})

	It("EnsureKubeconfigSecret already exists but data changed", func() {
		err := cm.EnsureKubeconfigSecret(ctx, log, clusterDeployment, kubeconfigFile)
		Expect(err).NotTo(HaveOccurred())
		verifyKubeconfigSecret(ctx, cm.Client, clusterDeployment, kubeconfigData)

		kubeconfigFile, err = createTempFile("kubeconfig-new", "kubeconfig-new")
		Expect(err).NotTo(HaveOccurred())
		err = cm.EnsureKubeconfigSecret(ctx, log, clusterDeployment, kubeconfigFile)
		Expect(err).NotTo(HaveOccurred())
		verifyKubeconfigSecret(ctx, cm.Client, clusterDeployment, "kubeconfig-new")
	})

	It("EnsureKubeconfigSecret file doesn't exists", func() {
		err := cm.EnsureKubeconfigSecret(ctx, log, clusterDeployment, "non-existing-file")
		Expect(err).To(HaveOccurred())
	})

	It("EnsureAdminPasswordSecret success", func() {
		err := cm.EnsureAdminPasswordSecret(ctx, log, clusterDeployment, kubeAdminFile)
		Expect(err).NotTo(HaveOccurred())
		secretRef := types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: KubeadminPasswordSecretName(clusterDeployment.Name)}
		exists, err := cm.secretExistsAndValid(ctx, log, secretRef, "password", []byte(kubeAdminData))
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	It("EnsureAdminPasswordSecret already exists but data changed", func() {
		err := cm.EnsureAdminPasswordSecret(ctx, log, clusterDeployment, kubeAdminFile)
		Expect(err).NotTo(HaveOccurred())
		secretRef := types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: KubeadminPasswordSecretName(clusterDeployment.Name)}
		exists, err := cm.secretExistsAndValid(ctx, log, secretRef, "password", []byte(kubeAdminData))
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue())

		kubeAdminFile, err = createTempFile("kubeAdminData-new", "kubeAdminData-new")
		Expect(err).NotTo(HaveOccurred())
		err = cm.EnsureAdminPasswordSecret(ctx, log, clusterDeployment, kubeAdminFile)
		Expect(err).NotTo(HaveOccurred())
		exists, err = cm.secretExistsAndValid(ctx, log, secretRef, "password", []byte("kubeAdminData-new"))
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	It("EnsureAdminPasswordSecret file doesn't exists", func() {
		err := cm.EnsureAdminPasswordSecret(ctx, log, clusterDeployment, "non-existing-file")
		Expect(err).To(HaveOccurred())
	})

	It("EnsureSeedReconfiguration success", func() {
		err := cm.EnsureSeedReconfigurationSecret(ctx, log, clusterDeployment, seedReconfigurationFile)
		Expect(err).NotTo(HaveOccurred())
		secretRef := types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: SeedReconfigurationSecretName(clusterDeployment.Name)}
		exists, err := cm.secretExistsAndValid(ctx, log, secretRef, SeedReconfigurationFileName, []byte(seedReconfigData))
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	It("EnsureSeedReconfiguration already exists but data changed", func() {
		err := cm.EnsureSeedReconfigurationSecret(ctx, log, clusterDeployment, seedReconfigurationFile)
		Expect(err).NotTo(HaveOccurred())
		secretRef := types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: SeedReconfigurationSecretName(clusterDeployment.Name)}
		exists, err := cm.secretExistsAndValid(ctx, log, secretRef, SeedReconfigurationFileName, []byte(seedReconfigData))
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue())

		seedReconfigurationFile, err = createTempFile("seedReconfiguration-new", "seedReconfiguration-new")
		Expect(err).NotTo(HaveOccurred())
		err = cm.EnsureSeedReconfigurationSecret(ctx, log, clusterDeployment, seedReconfigurationFile)
		Expect(err).NotTo(HaveOccurred())
		exists, err = cm.secretExistsAndValid(ctx, log, secretRef, SeedReconfigurationFileName, []byte("seedReconfiguration-new"))
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	It("EnsureSeedReconfiguration file doesn't exists", func() {
		err := cm.EnsureSeedReconfigurationSecret(ctx, log, clusterDeployment, "non-existing-file")
		Expect(err).To(HaveOccurred())
	})

	Describe("SeedReconfigSecretClusterIDs", func() {
		createSecret := func(data map[string][]byte) {
			s := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      SeedReconfigurationSecretName(clusterDeployment.Name),
					Namespace: clusterDeployment.Namespace,
				},
				Data: data,
			}
			Expect(c.Create(ctx, &s)).To(Succeed())
		}

		It("returns empty strings with no error if the secret doesn't exist", func() {
			clusterID, infraID, err := cm.SeedReconfigSecretClusterIDs(ctx, log, clusterDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterID).To(Equal(""))
			Expect(infraID).To(Equal(""))
		})

		It("fails if the required secret key is not found", func() {
			createSecret(map[string][]byte{"asdf": []byte("stuff")})
			clusterID, infraID, err := cm.SeedReconfigSecretClusterIDs(ctx, log, clusterDeployment)
			Expect(err).To(HaveOccurred())
			Expect(clusterID).To(Equal(""))
			Expect(infraID).To(Equal(""))
		})

		It("fails if the secret contains invalid json", func() {
			createSecret(map[string][]byte{SeedReconfigurationFileName: []byte(`{"asdf": ["a",]}`)})
			clusterID, infraID, err := cm.SeedReconfigSecretClusterIDs(ctx, log, clusterDeployment)
			Expect(err).To(HaveOccurred())
			Expect(clusterID).To(Equal(""))
			Expect(infraID).To(Equal(""))
		})

		It("returns the ids from the secret", func() {
			createSecret(map[string][]byte{SeedReconfigurationFileName: []byte(seedReconfigData)})
			clusterID, infraID, err := cm.SeedReconfigSecretClusterIDs(ctx, log, clusterDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterID).To(Equal("af5f4671-453c-4a4a-8b2b-bacf70552030"))
			Expect(infraID).To(Equal("ibiotest-67vn4"))
		})
	})
})

func verifyKubeconfigSecret(ctx context.Context, kClient client.Client, cd *hivev1.ClusterDeployment, data string) {
	kubeconfigSecretData := getKubeconfigFromSecret(ctx, kClient, cd)
	Expect(string(kubeconfigSecretData)).To(Equal(data))
}

func getKubeconfigFromSecret(ctx context.Context, kClient client.Client, cd *hivev1.ClusterDeployment) []byte {
	kubeconfigSecret := &corev1.Secret{}
	err := kClient.Get(ctx, client.ObjectKey{Namespace: cd.Namespace, Name: cd.Name + "-admin-kubeconfig"}, kubeconfigSecret)
	Expect(err).NotTo(HaveOccurred())
	kubeconfigSecretData, exists := kubeconfigSecret.Data["kubeconfig"]
	Expect(exists).To(BeTrue())
	return kubeconfigSecretData
}

func createTempFile(prefix, data string) (string, error) {
	f, err := os.CreateTemp("", prefix)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.Write([]byte(data))
	if err != nil {
		return "", err
	}
	return f.Name(), nil

}

func TestCertManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Credentials Suite")
}
