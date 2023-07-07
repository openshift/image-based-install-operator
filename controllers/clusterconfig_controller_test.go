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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Reconcile", func() {
	var (
		c         client.Client
		dataDir   string
		serverDir string
		r         *ClusterConfigReconciler
		ctx       = context.Background()
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

	It("creates an iso with the correct content", func() {
		config := &relocationv1alpha1.ClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: "test-namespace",
			},
			Spec: relocationv1alpha1.ClusterConfigSpec{
				ClusterRelocationSpec: cro.ClusterRelocationSpec{
					Domain:  "thing.example.com",
					SSHKeys: []string{"ssh-rsa sshkeyhere foo@example.com"},
				},
			},
		}
		Expect(c.Create(ctx, config)).To(Succeed())

		key := types.NamespacedName{
			Namespace: config.ObjectMeta.Namespace,
			Name:      config.ObjectMeta.Name,
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
		Expect(*relocationSpec).To(Equal(config.Spec.ClusterRelocationSpec))
	})

	It("sets the image url in status", func() {
	})
})
