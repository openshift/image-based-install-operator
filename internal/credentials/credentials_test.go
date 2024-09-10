package credentials

import (
	"context"
	"fmt"
	"testing"

	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/openshift/image-based-install-operator/internal/certs"
	"github.com/sirupsen/logrus"
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
			Certs:  certs.KubeConfigCertManager{},
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
	})

	It("EnsureKubeconfigSecret success", func() {
		_, err := cm.EnsureKubeconfigSecret(ctx, clusterDeployment)
		Expect(err).NotTo(HaveOccurred())
		verifyKubeconfigSecret(ctx, cm.Client, clusterDeployment)
	})
	It("EnsureKubeconfigSecret no kubeconfig secret", func() {
		_, err := cm.EnsureKubeconfigSecret(ctx, clusterDeployment)
		Expect(err).NotTo(HaveOccurred())
		verifyKubeconfigSecret(ctx, cm.Client, clusterDeployment)
	})
	It("EnsureKubeconfigSecret already exists cluster name changed", func() {
		kubeConfigCryptoRetention, err := cm.EnsureKubeconfigSecret(ctx, clusterDeployment)
		Expect(err).NotTo(HaveOccurred())
		kubeconfigSecretData := getKubeconfigFromSecret(ctx, cm.Client, clusterDeployment)
		// Call again and check the content is the same
		clusterDeployment.Spec.ClusterName = "newClusterName"
		kubeConfigCryptoRetention_2, err := cm.EnsureKubeconfigSecret(ctx, clusterDeployment)
		Expect(err).NotTo(HaveOccurred())
		verifyKubeconfigSecret(ctx, cm.Client, clusterDeployment)
		// Verify new cluster crypto
		Expect(kubeConfigCryptoRetention_2).ToNot(Equal(kubeConfigCryptoRetention))
		// Verify the kubeconfig secret data changed
		kubeconfigSecretData2 := getKubeconfigFromSecret(ctx, cm.Client, clusterDeployment)
		Expect(string(kubeconfigSecretData2)).ToNot(Equal(string(kubeconfigSecretData)))
	})
	It("EnsureKubeconfigSecret already exists and valid - content should be the same", func() {
		kubeConfigCryptoRetention, err := cm.EnsureKubeconfigSecret(ctx, clusterDeployment)
		Expect(err).NotTo(HaveOccurred())
		kubeconfigSecretData := getKubeconfigFromSecret(ctx, cm.Client, clusterDeployment)
		// Call again and check the content is the same
		kubeConfigCryptoRetention_2, err := cm.EnsureKubeconfigSecret(ctx, clusterDeployment)
		Expect(err).NotTo(HaveOccurred())
		// Verify same cluster crypto
		Expect(kubeConfigCryptoRetention_2).To(Equal(kubeConfigCryptoRetention))
		// Verify the kubeconfig secret data hasn't changed
		kubeconfigSecretData2 := getKubeconfigFromSecret(ctx, cm.Client, clusterDeployment)
		Expect(string(kubeconfigSecretData2)).To(Equal(string(kubeconfigSecretData)))
	})
	It("EnsureAdminPasswordSecret success", func() {
		passwordHash, err := cm.EnsureAdminPasswordSecret(ctx, clusterDeployment, "")
		Expect(err).NotTo(HaveOccurred())
		// Verify the password secret
		passwordSecret := &corev1.Secret{}
		err = cm.Client.Get(ctx, client.ObjectKey{Namespace: clusterDeployment.Namespace, Name: clusterDeployment.Name + "-admin-password"}, passwordSecret)
		Expect(err).NotTo(HaveOccurred())
		password, exists := passwordSecret.Data["password"]
		Expect(exists).To(BeTrue())
		err = bcrypt.CompareHashAndPassword([]byte(passwordHash), password)
		Expect(err).NotTo(HaveOccurred())
	})
	It("EnsureAdminPasswordSecret already exists", func() {
		passwordHash, err := cm.EnsureAdminPasswordSecret(ctx, clusterDeployment, "")
		Expect(err).NotTo(HaveOccurred())
		// Verify the password secret
		passwordSecret := &corev1.Secret{}
		err = cm.Client.Get(ctx, client.ObjectKey{Namespace: clusterDeployment.Namespace, Name: clusterDeployment.Name + "-admin-password"}, passwordSecret)
		Expect(err).NotTo(HaveOccurred())
		password, exists := passwordSecret.Data["password"]
		Expect(exists).To(BeTrue())

		// Call again, the password hash should be the same
		passwordHash2, err := cm.EnsureAdminPasswordSecret(ctx, clusterDeployment, passwordHash)
		Expect(err).NotTo(HaveOccurred())
		Expect(passwordHash).To(Equal(passwordHash2))

		// Verify the secret didn't change
		passwordSecret2 := &corev1.Secret{}
		err = cm.Client.Get(ctx, client.ObjectKey{Namespace: clusterDeployment.Namespace, Name: clusterDeployment.Name + "-admin-password"}, passwordSecret2)
		Expect(err).NotTo(HaveOccurred())
		password2, exists := passwordSecret2.Data["password"]
		Expect(exists).To(BeTrue())
		Expect(password).To(Equal(password2))
		//check the password against the new bcrypt hash
		err = bcrypt.CompareHashAndPassword([]byte(passwordHash2), password)
		Expect(err).NotTo(HaveOccurred())
	})
	It("EnsureAdminPasswordSecret already exists but malformed - should succeed", func() {
		passwordSecret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterDeployment.Name + "-admin-password",
				Namespace: clusterDeployment.Namespace,
			},
			Data: map[string][]byte{
				"bad data": []byte("not admin password"),
			},
		}
		err := cm.Client.Create(ctx, passwordSecret)
		Expect(err).NotTo(HaveOccurred())

		passwordHash, err := cm.EnsureAdminPasswordSecret(ctx, clusterDeployment, "")
		Expect(err).NotTo(HaveOccurred())
		// Verify the password secret
		err = cm.Client.Get(ctx, client.ObjectKey{Namespace: clusterDeployment.Namespace, Name: clusterDeployment.Name + "-admin-password"}, passwordSecret)
		Expect(err).NotTo(HaveOccurred())
		password, exists := passwordSecret.Data["password"]
		Expect(exists).To(BeTrue())
		err = bcrypt.CompareHashAndPassword([]byte(passwordHash), password)
		Expect(err).NotTo(HaveOccurred())
	})

	It("EnsureAdminPasswordSecret fail - password too long", func() {
		// Note that this shouldn't happen it's just an easy way to get a failure flow
		passwordSecret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterDeployment.Name + "-admin-password",
				Namespace: clusterDeployment.Namespace,
			},
			Data: map[string][]byte{
				"password": []byte("very long password that bcrypt will fail to create a hash for, this needs to be longer than 72 charecters..."),
			},
		}
		err := cm.Client.Create(ctx, passwordSecret)
		Expect(err).NotTo(HaveOccurred())

		_, err = cm.EnsureAdminPasswordSecret(ctx, clusterDeployment, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to generate password hash"))
	})

})

func verifyKubeconfigSecret(ctx context.Context, kClient client.Client, cd *hivev1.ClusterDeployment) {
	kubeconfigSecretData := getKubeconfigFromSecret(ctx, kClient, cd)
	conifg, err := clientcmd.Load(kubeconfigSecretData)
	Expect(err).NotTo(HaveOccurred())
	Expect(conifg.Clusters["cluster"].Server).To(Equal(fmt.Sprintf("https://api.%s.%s:6443", cd.Spec.ClusterName, cd.Spec.BaseDomain)))
	Expect(conifg.CurrentContext).To(Equal("admin"))
}

func getKubeconfigFromSecret(ctx context.Context, kClient client.Client, cd *hivev1.ClusterDeployment) []byte {
	kubeconfigSecret := &corev1.Secret{}
	err := kClient.Get(ctx, client.ObjectKey{Namespace: cd.Namespace, Name: cd.Name + "-admin-kubeconfig"}, kubeconfigSecret)
	Expect(err).NotTo(HaveOccurred())
	kubeconfigSecretData, exists := kubeconfigSecret.Data["kubeconfig"]
	Expect(exists).To(BeTrue())
	return kubeconfigSecretData
}

func TestCertManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Credentials Suite")
}
