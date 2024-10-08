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
	kubeconfigData = "kubeconfig"
	kubeAdminData  = "kubeadmin"
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
		log                        = logrus.FieldLogger(logrus.New())
	)
	v1alpha1.AddToScheme(scheme.Scheme)
	hivev1.AddToScheme(scheme.Scheme)
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
