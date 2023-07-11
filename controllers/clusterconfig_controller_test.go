package controllers

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	cro "github.com/RHsyseng/cluster-relocation-operator/api/v1beta1"
	relocationv1alpha1 "github.com/carbonin/cluster-relocation-service/api/v1alpha1"
	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/filesystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Reconcile", func() {
	var (
		c               client.Client
		dataDir         string
		serverDir       string
		r               *ClusterConfigReconciler
		ctx             = context.Background()
		configName      = "test-config"
		configNamespace = "test-namespace"
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		var err error
		dataDir, err = os.MkdirTemp("", "clusterconfig_controller_test_data")
		Expect(err).NotTo(HaveOccurred())
		serverDir, err = os.MkdirTemp("", "clusterconfig_controller_test_server")
		Expect(err).NotTo(HaveOccurred())

		r = &ClusterConfigReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    logrus.New(),
			Options: &ClusterConfigReconcilerOptions{
				BaseURL:   "https://example.com/",
				DataDir:   dataDir,
				ServerDir: serverDir,
			},
		}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dataDir)).To(Succeed())
		Expect(os.RemoveAll(serverDir)).To(Succeed())
	})

	createConfig := func(spec relocationv1alpha1.ClusterConfigSpec) {
		config := &relocationv1alpha1.ClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: "test-namespace",
			},
			Spec: spec,
		}
		Expect(c.Create(ctx, config)).To(Succeed())
	}

	createSecret := func(name string, data map[string][]byte) {
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "test-namespace",
			},
			Data: data,
		}
		Expect(c.Create(ctx, s)).To(Succeed())
	}

	validateISOSecretContent := func(fs filesystem.FileSystem, file string, data map[string][]byte) {
		f, err := fs.OpenFile(file, os.O_RDONLY)
		Expect(err).NotTo(HaveOccurred())

		content, err := io.ReadAll(f)
		Expect(err).NotTo(HaveOccurred())
		secret := &corev1.Secret{}
		Expect(json.Unmarshal(content, secret)).To(Succeed())
		Expect(secret.Data).To(Equal(data))
	}

	It("creates an iso with the correct relocation content", func() {
		spec := relocationv1alpha1.ClusterConfigSpec{
			ClusterRelocationSpec: cro.ClusterRelocationSpec{
				Domain:  "thing.example.com",
				SSHKeys: []string{"ssh-rsa sshkeyhere foo@example.com"},
			},
		}
		createConfig(spec)

		key := types.NamespacedName{
			Namespace: configNamespace,
			Name:      configName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		isoPath := filepath.Join(serverDir, "test-namespace", "test-config.iso")
		d, err := diskfs.Open(isoPath, diskfs.WithOpenMode(diskfs.ReadOnly))
		Expect(err).NotTo(HaveOccurred())
		fs, err := d.GetFilesystem(0)
		Expect(err).NotTo(HaveOccurred())
		f, err := fs.OpenFile("/cluster-relocation-spec.json", os.O_RDONLY)
		Expect(err).NotTo(HaveOccurred())

		content, err := io.ReadAll(f)
		Expect(err).NotTo(HaveOccurred())
		relocationSpec := &cro.ClusterRelocationSpec{}
		Expect(json.Unmarshal(content, relocationSpec)).To(Succeed())
		Expect(*relocationSpec).To(Equal(spec.ClusterRelocationSpec))
	})

	It("creates the referenced secrets", func() {
		apiCertData := map[string][]byte{"apicert": []byte("apicert")}
		ingressCertData := map[string][]byte{"ingresscert": []byte("ingresscert")}
		pullSecretData := map[string][]byte{"pullsecret": []byte("pullsecret")}
		createSecret("api-cert", apiCertData)
		createSecret("ingress-cert", ingressCertData)
		createSecret("pull-secret", pullSecretData)

		spec := relocationv1alpha1.ClusterConfigSpec{
			ClusterRelocationSpec: cro.ClusterRelocationSpec{
				APICertRef: &corev1.SecretReference{
					Name: "api-cert", Namespace: configNamespace,
				},
				IngressCertRef: &corev1.SecretReference{
					Name: "ingress-cert", Namespace: configNamespace,
				},
				PullSecretRef: &corev1.SecretReference{
					Name: "pull-secret", Namespace: configNamespace,
				},
			},
		}
		createConfig(spec)

		key := types.NamespacedName{
			Namespace: configNamespace,
			Name:      configName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		isoPath := filepath.Join(serverDir, "test-namespace", "test-config.iso")
		d, err := diskfs.Open(isoPath, diskfs.WithOpenMode(diskfs.ReadOnly))
		Expect(err).NotTo(HaveOccurred())
		fs, err := d.GetFilesystem(0)
		Expect(err).NotTo(HaveOccurred())

		validateISOSecretContent(fs, "/api-cert-secret.json", apiCertData)
		validateISOSecretContent(fs, "/ingress-cert-secret.json", ingressCertData)
		validateISOSecretContent(fs, "/pull-secret-secret.json", pullSecretData)
	})

	It("sets the image url in status", func() {
		spec := relocationv1alpha1.ClusterConfigSpec{
			ClusterRelocationSpec: cro.ClusterRelocationSpec{
				Domain:  "thing.example.com",
				SSHKeys: []string{"ssh-rsa sshkeyhere foo@example.com"},
			},
		}
		createConfig(spec)

		key := types.NamespacedName{
			Namespace: configNamespace,
			Name:      configName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		config := &relocationv1alpha1.ClusterConfig{}
		Expect(c.Get(ctx, key, config)).To(Succeed())
		Expect(config.Status.ImageURL).To(Equal("https://example.com/images/test-namespace/test-config.iso"))
	})
})
