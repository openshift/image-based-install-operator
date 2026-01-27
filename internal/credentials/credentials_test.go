package credentials

import (
	"context"
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/sirupsen/logrus"
	gomock "go.uber.org/mock/gomock"
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

	verifySecretPreservationLabel := func(name, namespace string) {
		key := types.NamespacedName{Name: name, Namespace: namespace}
		secret := &corev1.Secret{}
		Expect(c.Get(ctx, key, secret)).To(Succeed())
		Expect(secret.Labels).To(HaveKeyWithValue(secretPreservationLabel, secretPreservationValue))
	}

	Describe("EnsureKubeconfigSecret", func() {
		getKubeconfigFromSecret := func(ctx context.Context, kClient client.Client, cd *hivev1.ClusterDeployment) []byte {
			kubeconfigSecret := &corev1.Secret{}
			err := kClient.Get(ctx, client.ObjectKey{Namespace: cd.Namespace, Name: cd.Name + "-admin-kubeconfig"}, kubeconfigSecret)
			Expect(err).NotTo(HaveOccurred())
			kubeconfigSecretData, exists := kubeconfigSecret.Data["kubeconfig"]
			Expect(exists).To(BeTrue())
			return kubeconfigSecretData
		}

		verifyKubeconfigSecret := func(ctx context.Context, kClient client.Client, cd *hivev1.ClusterDeployment, data string) {
			kubeconfigSecretData := getKubeconfigFromSecret(ctx, kClient, cd)
			Expect(string(kubeconfigSecretData)).To(Equal(data))
		}

		It("success", func() {
			err := cm.EnsureKubeconfigSecret(ctx, log, clusterDeployment, kubeconfigFile)
			Expect(err).NotTo(HaveOccurred())
			verifyKubeconfigSecret(ctx, cm.Client, clusterDeployment, kubeconfigData)
		})

		It("sets the siteconfig secret preservation label", func() {
			Expect(cm.EnsureKubeconfigSecret(ctx, log, clusterDeployment, kubeconfigFile)).To(Succeed())
			verifySecretPreservationLabel(clusterDeployment.Name+"-admin-kubeconfig", clusterDeployment.Namespace)
		})

		It("already exists but data changed", func() {
			err := cm.EnsureKubeconfigSecret(ctx, log, clusterDeployment, kubeconfigFile)
			Expect(err).NotTo(HaveOccurred())
			verifyKubeconfigSecret(ctx, cm.Client, clusterDeployment, kubeconfigData)

			kubeconfigFile, err = createTempFile("kubeconfig-new", "kubeconfig-new")
			Expect(err).NotTo(HaveOccurred())
			err = cm.EnsureKubeconfigSecret(ctx, log, clusterDeployment, kubeconfigFile)
			Expect(err).NotTo(HaveOccurred())
			verifyKubeconfigSecret(ctx, cm.Client, clusterDeployment, "kubeconfig-new")
		})

		It("file doesn't exists", func() {
			err := cm.EnsureKubeconfigSecret(ctx, log, clusterDeployment, "non-existing-file")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("EnsureAdminPasswordSecret", func() {
		It("success", func() {
			err := cm.EnsureAdminPasswordSecret(ctx, log, clusterDeployment, kubeAdminFile)
			Expect(err).NotTo(HaveOccurred())
			secretRef := types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: KubeadminPasswordSecretName(clusterDeployment.Name)}
			exists, err := cm.secretExistsAndValid(ctx, log, secretRef, "password", []byte(kubeAdminData))
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("sets the siteconfig secret preservation label", func() {
			Expect(cm.EnsureAdminPasswordSecret(ctx, log, clusterDeployment, kubeconfigFile)).To(Succeed())
			verifySecretPreservationLabel(KubeadminPasswordSecretName(clusterDeployment.Name), clusterDeployment.Namespace)
		})

		It("already exists but data changed", func() {
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

		It("file doesn't exists", func() {
			err := cm.EnsureAdminPasswordSecret(ctx, log, clusterDeployment, "non-existing-file")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("EnsureSeedReconfigurationSecret", func() {
		It("success", func() {
			err := cm.EnsureSeedReconfigurationSecret(ctx, log, clusterDeployment, seedReconfigurationFile)
			Expect(err).NotTo(HaveOccurred())
			secretRef := types.NamespacedName{Namespace: clusterDeployment.Namespace, Name: SeedReconfigurationSecretName(clusterDeployment.Name)}
			exists, err := cm.secretExistsAndValid(ctx, log, secretRef, SeedReconfigurationFileName, []byte(seedReconfigData))
			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("sets the siteconfig secret preservation label", func() {
			Expect(cm.EnsureSeedReconfigurationSecret(ctx, log, clusterDeployment, kubeconfigFile)).To(Succeed())
			verifySecretPreservationLabel(SeedReconfigurationSecretName(clusterDeployment.Name), clusterDeployment.Namespace)
		})

		It("already exists but data changed", func() {
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

		It("file doesn't exists", func() {
			err := cm.EnsureSeedReconfigurationSecret(ctx, log, clusterDeployment, "non-existing-file")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("createOrUpdateClusterCredentialSecret", func() {
		It("sets filter label if it was removed", func() {
			name := "test-secret"
			data := map[string][]byte{"thing": []byte("stuff")}
			Expect(cm.createOrUpdateClusterCredentialSecret(ctx, log, clusterDeployment, name, data, "thing")).To(Succeed())

			secret := corev1.Secret{}
			key := types.NamespacedName{Name: name, Namespace: clusterDeployment.Namespace}
			Expect(c.Get(ctx, key, &secret)).To(Succeed())
			delete(secret.Labels, SecretResourceLabel)
			Expect(c.Update(ctx, &secret)).To(Succeed())

			Expect(cm.createOrUpdateClusterCredentialSecret(ctx, log, clusterDeployment, name, data, "thing")).To(Succeed())
			Expect(c.Get(ctx, key, &secret)).To(Succeed())
			Expect(secret.Labels).To(HaveKeyWithValue(SecretResourceLabel, SecretResourceValue))
		})

		It("updates OwnerReference if it was removed", func() {
			name := "test-secret"
			data := map[string][]byte{"thing": []byte("stuff")}
			Expect(cm.createOrUpdateClusterCredentialSecret(ctx, log, clusterDeployment, name, data, "thing")).To(Succeed())

			secret := corev1.Secret{}
			key := types.NamespacedName{Name: name, Namespace: clusterDeployment.Namespace}
			Expect(c.Get(ctx, key, &secret)).To(Succeed())
			secret.OwnerReferences = nil
			Expect(c.Update(ctx, &secret)).To(Succeed())

			Expect(cm.createOrUpdateClusterCredentialSecret(ctx, log, clusterDeployment, name, data, "thing")).To(Succeed())
			Expect(c.Get(ctx, key, &secret)).To(Succeed())
			Expect(len(secret.OwnerReferences)).To(Equal(1))
			Expect(secret.OwnerReferences[0].Name).To(Equal(clusterDeployment.Name))
		})
	})

	createSecret := func(name string, data map[string][]byte) {
		s := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: clusterDeployment.Namespace,
			},
			Data: data,
		}
		Expect(c.Create(ctx, &s)).To(Succeed())
	}

	Describe("SeedReconfigSecretClusterIDs", func() {
		var secretName string

		BeforeEach(func() {
			secretName = SeedReconfigurationSecretName(clusterDeployment.Name)
		})

		It("returns empty strings with no error if the secret doesn't exist", func() {
			clusterID, infraID, err := cm.SeedReconfigSecretClusterIDs(ctx, log, clusterDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterID).To(Equal(""))
			Expect(infraID).To(Equal(""))
		})

		It("returns empty strings with no error if secret key is not found", func() {
			createSecret(secretName, map[string][]byte{"asdf": []byte("stuff")})
			clusterID, infraID, err := cm.SeedReconfigSecretClusterIDs(ctx, log, clusterDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterID).To(Equal(""))
			Expect(infraID).To(Equal(""))
		})

		It("fails if the secret contains invalid json", func() {
			createSecret(secretName, map[string][]byte{SeedReconfigurationFileName: []byte(`{"asdf": ["a",]}`)})
			clusterID, infraID, err := cm.SeedReconfigSecretClusterIDs(ctx, log, clusterDeployment)
			Expect(err).To(HaveOccurred())
			Expect(clusterID).To(Equal(""))
			Expect(infraID).To(Equal(""))
		})

		It("returns the ids from the secret", func() {
			createSecret(secretName, map[string][]byte{SeedReconfigurationFileName: []byte(seedReconfigData)})
			clusterID, infraID, err := cm.SeedReconfigSecretClusterIDs(ctx, log, clusterDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterID).To(Equal("af5f4671-453c-4a4a-8b2b-bacf70552030"))
			Expect(infraID).To(Equal("ibiotest-67vn4"))
		})
	})

	// test separately to reduce the permutations required when testing ClusterIdentitySecrets
	Describe("getSecretContent", func() {
		It("returns the requested key data", func() {
			secretName := "mysecret"
			key := "asdf"
			val := []byte("stuff")
			createSecret(secretName, map[string][]byte{key: val})

			ref := types.NamespacedName{Name: secretName, Namespace: clusterDeployment.Namespace}
			val, exists, err := cm.getSecretContent(ctx, ref, key)

			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeTrue())
			Expect(val).To(Equal(val))
		})

		It("returns false when the secret doesn't exist", func() {
			secretName := "mysecret"
			key := "asdf"

			ref := types.NamespacedName{Name: secretName, Namespace: clusterDeployment.Namespace}
			_, exists, err := cm.getSecretContent(ctx, ref, key)

			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("returns false when the key doesn't exist in the secret", func() {
			secretName := "mysecret"
			key := "asdf"
			val := []byte("stuff")
			createSecret(secretName, map[string][]byte{key: val})

			ref := types.NamespacedName{Name: secretName, Namespace: clusterDeployment.Namespace}
			_, exists, err := cm.getSecretContent(ctx, ref, "qwer")

			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("returns false when the key exists with zero length", func() {
			secretName := "mysecret"
			key := "asdf"
			val := []byte("")
			createSecret(secretName, map[string][]byte{key: val})

			ref := types.NamespacedName{Name: secretName, Namespace: clusterDeployment.Namespace}
			_, exists, err := cm.getSecretContent(ctx, ref, key)

			Expect(err).NotTo(HaveOccurred())
			Expect(exists).To(BeFalse())
		})

		It("returns an error when Get fails", func() {
			mockCtrl := gomock.NewController(GinkgoT())
			defer mockCtrl.Finish()

			mockClient := NewMockClient(mockCtrl)
			cm.Client = mockClient

			secretName := "mysecret"
			key := "asdf"
			ref := types.NamespacedName{Name: secretName, Namespace: clusterDeployment.Namespace}
			mockClient.EXPECT().Get(gomock.Any(), ref, gomock.Any()).Return(fmt.Errorf("Get failed"))
			_, _, err := cm.getSecretContent(ctx, ref, key)

			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ClusterIdentitySecrets", func() {
		It("returns the contents and existence when all secrets exist", func() {
			createSecret(KubeconfigSecretName(clusterDeployment.Name), map[string][]byte{"kubeconfig": []byte(kubeconfigData)})
			createSecret(KubeadminPasswordSecretName(clusterDeployment.Name), map[string][]byte{kubeAdminKey: []byte(kubeAdminData)})
			createSecret(SeedReconfigurationSecretName(clusterDeployment.Name), map[string][]byte{SeedReconfigurationFileName: []byte(seedReconfigData)})

			idData, exist, err := cm.ClusterIdentitySecrets(ctx, clusterDeployment)

			Expect(err).NotTo(HaveOccurred())
			Expect(exist).To(BeTrue())
			Expect(idData.Kubeconfig).To(Equal([]byte(kubeconfigData)))
			Expect(idData.KubeadminPassword).To(Equal([]byte(kubeAdminData)))
			Expect(idData.SeedReconfig).To(Equal([]byte(seedReconfigData)))
		})

		It("returns exist == false when a secret doesn't exist", func() {
			createSecret(KubeconfigSecretName(clusterDeployment.Name), map[string][]byte{"kubeconfig": []byte(kubeconfigData)})
			createSecret(SeedReconfigurationSecretName(clusterDeployment.Name), map[string][]byte{SeedReconfigurationFileName: []byte(seedReconfigData)})

			_, exist, err := cm.ClusterIdentitySecrets(ctx, clusterDeployment)

			Expect(err).NotTo(HaveOccurred())
			Expect(exist).To(BeFalse())
		})

		It("returns an error when an error is encountered", func() {
			mockCtrl := gomock.NewController(GinkgoT())
			defer mockCtrl.Finish()
			mockClient := NewMockClient(mockCtrl)
			cm.Client = mockClient
			mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("Get failed"))

			_, _, err := cm.ClusterIdentitySecrets(ctx, clusterDeployment)

			Expect(err).To(HaveOccurred())
		})
	})
})

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
