package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	lca_api "github.com/openshift-kni/lifecycle-agent/api/seedreconfig"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/openshift/image-based-install-operator/internal/certs"
	"github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Reconcile", func() {
	var (
		c                       client.Client
		dataDir                 string
		r                       *ImageClusterInstallReconciler
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster"
		clusterInstallNamespace = "test-namespace"
		clusterInstall          *v1alpha1.ImageClusterInstall
		clusterDeployment       *hivev1.ClusterDeployment
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()
		var err error
		dataDir, err = os.MkdirTemp("", "imageclusterinstall_controller_test_data")
		Expect(err).NotTo(HaveOccurred())

		r = &ImageClusterInstallReconciler{
			Client:  c,
			Scheme:  scheme.Scheme,
			Log:     logrus.New(),
			BaseURL: "http://service.namespace",
			Options: &ImageClusterInstallReconcilerOptions{
				ServiceName:      "service",
				ServiceNamespace: "namespace",
				ServiceScheme:    "http",
				DataDir:          dataDir,
			},
			CertManager: certs.KubeConfigCertManager{},
		}

		imageSet := &hivev1.ClusterImageSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "imageset",
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterImageSetSpec{
				ReleaseImage: "registry.example.com/releases/ocp@sha256:0ec9d715c717b2a592d07dd83860013613529fae69bc9eecb4b2d4ace679f6f3",
			},
		}
		Expect(c.Create(ctx, imageSet)).To(Succeed())

		clusterInstall = &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				ImageSetRef: hivev1.ClusterImageSetReference{
					Name: imageSet.Name,
				},
				ClusterDeploymentRef: &corev1.LocalObjectReference{Name: clusterInstallName},
			},
		}

		clusterDeployment = &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Group:   clusterInstall.GroupVersionKind().Group,
					Version: clusterInstall.GroupVersionKind().Version,
					Kind:    clusterInstall.GroupVersionKind().Kind,
					Name:    clusterInstall.Name,
				},
			},
		}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dataDir)).To(Succeed())
	})

	outputFilePath := func(elem ...string) string {
		last := filepath.Join(elem...)
		return filepath.Join(dataDir, "namespaces", clusterInstallNamespace, clusterInstallName, "files", last)
	}

	validateExtraManifestContent := func(file string, data string) {
		content, err := os.ReadFile(outputFilePath(extraManifestsDir, file))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(data))
	}

	It("creates the correct cluster info manifest", func() {
		clusterInstall.Spec.NodeIP = "192.0.2.1"
		clusterInstall.Spec.Hostname = "thing"
		clusterInstall.Spec.SSHKey = "my ssh key"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		clusterDeployment.Spec.BaseDomain = "example.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		Expect(infoOut.APIVersion).To(Equal(lca_api.SeedReconfigurationVersion))
		Expect(infoOut.BaseDomain).To(Equal(clusterDeployment.Spec.BaseDomain))
		Expect(infoOut.ClusterName).To(Equal(clusterDeployment.Spec.ClusterName))
		Expect(infoOut.ClusterID).ToNot(Equal(""))
		Expect(infoOut.NodeIP).To(Equal(clusterInstall.Spec.NodeIP))
		Expect(infoOut.ReleaseRegistry).To(Equal("registry.example.com"))
		Expect(infoOut.Hostname).To(Equal(clusterInstall.Spec.Hostname))
		Expect(infoOut.SSHKey).To(Equal(clusterInstall.Spec.SSHKey))
	})

	It("keep cluster crypto if name and base domain didn't change", func() {
		baseDomain := "example.com"
		clusterName := "thingcluster"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		clusterDeployment.Spec.ClusterName = clusterName
		clusterDeployment.Spec.BaseDomain = baseDomain
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		// save the current KubeconfigCryptoRetention
		clusterCrypto := infoOut.KubeconfigCryptoRetention
		// reconcile again and verify that the cluster crypto stay the same
		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		content, err = os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut = &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(clusterCrypto).To(Equal(infoOut.KubeconfigCryptoRetention))
	})
	It("regenerate cluster crypto in case the cluster name or base domain changed", func() {
		baseDomain := "example.com"
		clusterName := "thingcluster"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		clusterDeployment.Spec.ClusterName = clusterName
		clusterDeployment.Spec.BaseDomain = baseDomain
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		// save the current KubeconfigCryptoRetention
		clusterCrypto := infoOut.KubeconfigCryptoRetention

		clusterDeployment.Spec.BaseDomain = "new.base.domain"
		Expect(c.Update(ctx, clusterDeployment)).To(Succeed())
		// reconcile again and verify that the cluster crypto got updated
		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		content, err = os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut = &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(clusterCrypto).ToNot(Equal(infoOut.KubeconfigCryptoRetention))

		// save the current KubeconfigCryptoRetention
		clusterCrypto = infoOut.KubeconfigCryptoRetention
		clusterDeployment.Spec.ClusterName = "newName"
		Expect(c.Update(ctx, clusterDeployment)).To(Succeed())
		// reconcile again and verify that the cluster crypto got updated
		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		content, err = os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut = &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(clusterCrypto).ToNot(Equal(infoOut.KubeconfigCryptoRetention))

	})
	It("creates the pull secret without extra metadata", func() {
		pullSecretData := map[string][]byte{"pullsecret": []byte("pullsecret")}
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-pull-secret",
				Namespace: clusterInstallNamespace,
				UID:       types.UID("22ce1ffc-aa2d-477e-87b6-2a755f332e41"),
			},
			Data: pullSecretData,
		}
		Expect(c.Create(ctx, s)).To(Succeed())

		clusterDeployment.Spec.PullSecretRef = &corev1.LocalObjectReference{Name: "my-pull-secret"}
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifests", "pull-secret-secret.json"))
		Expect(err).NotTo(HaveOccurred())
		secret := &corev1.Secret{}
		Expect(json.Unmarshal(content, secret)).To(Succeed())

		Expect(secret.Namespace).To(Equal("openshift-config"))
		Expect(secret.Name).To(Equal("pull-secret"))
		Expect(secret.UID).To(Equal(types.UID("")))
		Expect(secret.Data).To(Equal(pullSecretData))
	})

	It("creates the ca bundle", func() {
		caData := map[string]string{caBundleFileName: "mycabundle"}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ca-bundle",
				Namespace: "test-namespace",
			},
			Data: caData,
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		clusterInstall.Spec.CABundleRef = &corev1.LocalObjectReference{
			Name: "ca-bundle",
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, caBundleFileName))
		Expect(err).NotTo(HaveOccurred())

		Expect(content).To(Equal([]byte("mycabundle")))
	})

	It("creates files for referenced nmconnection files", func() {
		netConfigName := "netconfig"
		netConfigData := map[string]string{
			"eth0.nmconnection": "some\nconnection\nstring",
			"eth1.nmconnection": "other\nconnection\nstring",
			"file":              "stuff",
		}
		s := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      netConfigName,
				Namespace: "test-namespace",
			},
			Data: netConfigData,
		}
		Expect(c.Create(ctx, s)).To(Succeed())

		clusterInstall.Spec.NetworkConfigRef = &corev1.LocalObjectReference{
			Name: netConfigName,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(networkConfigDir, "eth0.nmconnection"))
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(Equal([]byte("some\nconnection\nstring")))

		content, err = os.ReadFile(outputFilePath(networkConfigDir, "eth1.nmconnection"))
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(Equal([]byte("other\nconnection\nstring")))

		_, err = os.Stat(outputFilePath(networkConfigDir, "file"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("creates extra manifests", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "manifests",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string]string{
				"manifest1.yaml": "thing: stuff",
				"manifest2.yaml": "other: foo",
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		clusterInstall.Spec.ExtraManifestsRefs = []corev1.LocalObjectReference{
			{Name: "manifests"},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		validateExtraManifestContent("manifest1.yaml", "thing: stuff")
		validateExtraManifestContent("manifest2.yaml", "other: foo")
	})

	It("validates extra manifests", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "manifests",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string]string{
				"manifest1.yaml": "thing: \"st\"uff",
			},
		}
		Expect(c.Create(ctx, cm)).To(Succeed())

		clusterInstall.Spec.ExtraManifestsRefs = []corev1.LocalObjectReference{
			{Name: "manifests"},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
	})

	It("creates certificates", func() {
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		clusterDeployment.Spec.ClusterName = "test-cluster"
		clusterDeployment.Spec.BaseDomain = "redhat.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		// Verify the kubeconfig secret
		kubeconfigSecret := &corev1.Secret{}
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: clusterInstallNamespace, Name: clusterDeployment.Spec.ClusterName + "-admin-kubeconfig"}, kubeconfigSecret)
		Expect(err).NotTo(HaveOccurred())
		kubeconfigSecretData, exists := kubeconfigSecret.Data["kubeconfig"]
		Expect(exists).To(BeTrue())
		kubeconfig, err := clientcmd.Load(kubeconfigSecretData)
		Expect(err).NotTo(HaveOccurred())
		// verify the cluster URL
		Expect(kubeconfig.Clusters["cluster"].Server).To(Equal(fmt.Sprintf("https://api.%s.%s:6443", clusterDeployment.Spec.ClusterName, clusterDeployment.Spec.BaseDomain)))

		// verify all signer keys exists
		// verify the admin client CA cert exists
	})

	It("configures a referenced BMH", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateAvailable,
				},
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).NotTo(BeNil())
		Expect(bmh.Spec.Image.URL).To(Equal(fmt.Sprintf("http://service.namespace/images/%s/%s.iso", clusterInstallNamespace, clusterInstallName)))
		Expect(bmh.Spec.Image.DiskFormat).To(HaveValue(Equal("live-iso")))
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))
	})

	It("sets the BMH ref in the cluster install status", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateAvailable,
				},
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: clusterInstall.Namespace,
			Name:      clusterInstall.Name,
		}
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		Expect(clusterInstall.Status.BareMetalHostRef).To(HaveValue(Equal(*clusterInstall.Spec.BareMetalHostRef)))
	})

	It("sets detached on a referenced BMH after it is provisioned", func() {
		liveISO := "live-iso"
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Spec: bmh_v1alpha1.BareMetalHostSpec{
				Image: &bmh_v1alpha1.Image{
					URL:        fmt.Sprintf("http://service.namespace/images/%s/%s.iso", clusterInstallNamespace, clusterInstallName),
					DiskFormat: &liveISO,
				},
				Online: true,
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateProvisioned,
				},
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Annotations[detachedAnnotation]).To(Equal("imageclusterinstall-controller"))
	})

	It("doesn't error for a missing imageclusterinstall", func() {
		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("doesn't error when ClusterDeploymentRef is unset", func() {
		clusterInstall.Spec.ClusterDeploymentRef = nil
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("sets the image ready condition", func() {
		clusterInstall.Spec.Hostname = "thing"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := meta.FindStatusCondition(clusterInstall.Status.ConfigConditions, v1alpha1.ImageReadyCondition)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.ImageReadyReason))
		Expect(cond.Message).To(Equal(v1alpha1.ImageReadyMessage))
	})

	It("sets the host configured condition when the host can be configured", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateAvailable,
				},
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := meta.FindStatusCondition(clusterInstall.Status.ConfigConditions, v1alpha1.HostConfiguredCondition)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.HostConfiguraionSucceededReason))
		Expect(cond.Message).To(Equal(v1alpha1.HostConfigurationSucceededMessage))
	})

	It("sets the host configured condition to false when the host is missing", func() {
		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      "test-bmh",
			Namespace: "test-bmh-namespace",
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := meta.FindStatusCondition(clusterInstall.Status.ConfigConditions, v1alpha1.HostConfiguredCondition)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.HostConfiguraionFailedReason))
	})

	It("removes the image from a BMH when the reference is removed", func() {
		liveISO := "live-iso"
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Spec: bmh_v1alpha1.BareMetalHostSpec{
				Image: &bmh_v1alpha1.Image{
					URL:        fmt.Sprintf("http://service.namespace/images/%s/%s.iso", clusterInstallNamespace, clusterInstallName),
					DiskFormat: &liveISO,
				},
				Online: true,
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Status = v1alpha1.ImageClusterInstallStatus{
			BareMetalHostRef: &v1alpha1.BareMetalHostReference{
				Name:      bmh.Name,
				Namespace: bmh.Namespace,
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).To(BeNil())
	})

	It("removes the reference and configures a new BMH when the reference is changed", func() {
		liveISO := "live-iso"
		oldBMH := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "old-bmh",
				Namespace: "test-bmh-namespace",
			},
			Spec: bmh_v1alpha1.BareMetalHostSpec{
				Image: &bmh_v1alpha1.Image{
					URL:        fmt.Sprintf("http://service.namespace/images/%s/%s.iso", clusterInstallNamespace, clusterInstallName),
					DiskFormat: &liveISO,
				},
				Online: true,
			},
		}
		Expect(c.Create(ctx, oldBMH)).To(Succeed())

		newBMH := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "new-bmh",
				Namespace: "test-bmh-namespace",
			},
		}
		Expect(c.Create(ctx, newBMH)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      newBMH.Name,
			Namespace: newBMH.Namespace,
		}
		clusterInstall.Status = v1alpha1.ImageClusterInstallStatus{
			BareMetalHostRef: &v1alpha1.BareMetalHostReference{
				Name:      oldBMH.Name,
				Namespace: oldBMH.Namespace,
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		oldKey := types.NamespacedName{
			Namespace: oldBMH.Namespace,
			Name:      oldBMH.Name,
		}
		Expect(c.Get(ctx, oldKey, oldBMH)).To(Succeed())
		Expect(oldBMH.Spec.Image).To(BeNil())

		newKey := types.NamespacedName{
			Namespace: newBMH.Namespace,
			Name:      newBMH.Name,
		}
		Expect(c.Get(ctx, newKey, newBMH)).To(Succeed())
		Expect(newBMH.Spec.Image).ToNot(BeNil())
	})

	It("updates the cluster install and cluster deployment metadata", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateAvailable,
				},
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.NodeIP = "192.0.2.1"
		clusterInstall.Spec.Hostname = "thing"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		clusterDeployment.Spec.BaseDomain = "example.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		validateMeta := func(meta *hivev1.ClusterMetadata) {
			Expect(meta).ToNot(BeNil())
			Expect(meta.ClusterID).To(Equal(infoOut.ClusterID))
			Expect(meta.InfraID).To(HavePrefix("thingcluster"))
			Expect(meta.InfraID).To(Equal(infoOut.InfraID))
			Expect(meta.AdminKubeconfigSecretRef.Name).To(Equal("test-cluster-admin-kubeconfig"))
		}

		updatedICI := v1alpha1.ImageClusterInstall{}
		Expect(c.Get(ctx, key, &updatedICI)).To(Succeed())
		validateMeta(updatedICI.Spec.ClusterMetadata)

		updatedCD := hivev1.ClusterDeployment{}
		Expect(c.Get(ctx, key, &updatedCD)).To(Succeed())

		Expect(updatedCD.Spec.Installed).To(BeTrue())
		validateMeta(updatedCD.Spec.ClusterMetadata)
	})

	It("sets the clusterID to the manifest.json value if it exists", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateAvailable,
				},
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		existingInfo := lca_api.SeedReconfiguration{
			ClusterID: "5eaf4f02-2410-4fff-8e94-74aa6b5dc2cd",
		}
		content, err := json.Marshal(existingInfo)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.MkdirAll(outputFilePath(clusterConfigDir), 0700)).To(Succeed())
		Expect(os.WriteFile(outputFilePath(clusterConfigDir, "manifest.json"), content, 0644)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err = os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		updatedICI := v1alpha1.ImageClusterInstall{}
		Expect(c.Get(ctx, key, &updatedICI)).To(Succeed())

		Expect(updatedICI.Spec.ClusterMetadata).ToNot(BeNil())
		Expect(updatedICI.Spec.ClusterMetadata.ClusterID).To(Equal(existingInfo.ClusterID))
		Expect(infoOut.ClusterID).To(Equal(existingInfo.ClusterID))
	})

	It("sets the infraID to the manifest.json value if it exists", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateAvailable,
				},
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		existingInfo := lca_api.SeedReconfiguration{
			InfraID: "thingcluster-nvvbf",
		}
		content, err := json.Marshal(existingInfo)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.MkdirAll(outputFilePath(clusterConfigDir), 0700)).To(Succeed())
		Expect(os.WriteFile(outputFilePath(clusterConfigDir, "manifest.json"), content, 0644)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err = os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		updatedICI := v1alpha1.ImageClusterInstall{}
		Expect(c.Get(ctx, key, &updatedICI)).To(Succeed())

		Expect(updatedICI.Spec.ClusterMetadata).ToNot(BeNil())
		Expect(updatedICI.Spec.ClusterMetadata.InfraID).To(Equal(existingInfo.InfraID))
		Expect(infoOut.InfraID).To(Equal(existingInfo.InfraID))
	})
})

var _ = Describe("mapBMHToICI", func() {
	var (
		c                       client.Client
		r                       *ImageClusterInstallReconciler
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster-install"
		clusterInstallNamespace = "test-namespace"
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()

		r = &ImageClusterInstallReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    logrus.New(),
		}
	})

	It("returns a request for the cluster install referencing the given BMH", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      bmh.Name,
					Namespace: bmh.Namespace,
				},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterInstall = &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster-install",
				Namespace: clusterInstallNamespace,
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		requests := r.mapBMHToICI(ctx, bmh)
		Expect(len(requests)).To(Equal(1))
		Expect(requests[0].NamespacedName).To(Equal(types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}))
	})

	It("returns an empty list when no cluster install matches", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      "other-bmh",
					Namespace: bmh.Namespace,
				},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterInstall = &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-cluster-install",
				Namespace: clusterInstallNamespace,
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		requests := r.mapBMHToICI(ctx, bmh)
		Expect(len(requests)).To(Equal(0))
	})
})

var _ = Describe("mapCDToICI", func() {
	var (
		c                       client.Client
		r                       *ImageClusterInstallReconciler
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster-install"
		clusterInstallNamespace = "test-namespace"
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()

		r = &ImageClusterInstallReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    logrus.New(),
		}
	})

	It("returns a request for the cluster install referenced by the given ClusterDeployment", func() {
		cd := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cd",
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Group:   v1alpha1.Group,
					Version: v1alpha1.Version,
					Kind:    "ImageClusterInstall",
					Name:    clusterInstallName,
				},
			},
		}
		Expect(c.Create(ctx, cd)).To(Succeed())

		requests := r.mapCDToICI(ctx, cd)
		Expect(len(requests)).To(Equal(1))
		Expect(requests[0].NamespacedName).To(Equal(types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}))
	})

	It("returns an empty list when no cluster install is set", func() {
		cd := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cd",
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{},
		}
		Expect(c.Create(ctx, cd)).To(Succeed())

		requests := r.mapCDToICI(ctx, cd)
		Expect(len(requests)).To(Equal(0))
	})

	It("returns an empty list when the cluster install does not match our GVK", func() {
		cd := &hivev1.ClusterDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cd",
				Namespace: clusterInstallNamespace,
			},
			Spec: hivev1.ClusterDeploymentSpec{
				ClusterInstallRef: &hivev1.ClusterInstallLocalReference{
					Group:   "extensions.hive.openshift.io",
					Version: "v1beta2",
					Kind:    "AgentClusterInstall",
					Name:    clusterInstallName,
				},
			},
		}
		Expect(c.Create(ctx, cd)).To(Succeed())

		requests := r.mapCDToICI(ctx, cd)
		Expect(len(requests)).To(Equal(0))
	})
})

var _ = Describe("serviceURL", func() {
	It("creates the correct url without a port", func() {
		opts := &ImageClusterInstallReconcilerOptions{
			ServiceName:      "name",
			ServiceNamespace: "namespace",
			ServiceScheme:    "http",
		}
		Expect(serviceURL(opts)).To(Equal("http://name.namespace"))
	})
	It("creates the correct url with a port", func() {
		opts := &ImageClusterInstallReconcilerOptions{
			ServiceName:      "name",
			ServiceNamespace: "namespace",
			ServiceScheme:    "http",
			ServicePort:      "8080",
		}
		Expect(serviceURL(opts)).To(Equal("http://name.namespace:8080"))
	})
})

var _ = Describe("handleFinalizer", func() {
	var (
		c                       client.Client
		dataDir                 string
		r                       *ImageClusterInstallReconciler
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster-install"
		clusterInstallNamespace = "test-namespace"
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()
		var err error
		dataDir, err = os.MkdirTemp("", "imageclusterinstall_controller_test_data")
		Expect(err).NotTo(HaveOccurred())

		r = &ImageClusterInstallReconciler{
			Client: c,
			Scheme: scheme.Scheme,
			Log:    logrus.New(),
			Options: &ImageClusterInstallReconcilerOptions{
				DataDir: dataDir,
			},
		}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dataDir)).To(Succeed())
	})

	It("adds the finalizer if the cluster install is not being deleted", func() {
		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{Requeue: true}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		key := types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).To(ContainElement(clusterInstallFinalizerName))
	})

	It("noops if the finalizer is already present", func() {
		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(stop).To(BeFalse())
		Expect(err).ToNot(HaveOccurred())
	})

	It("deletes the local files when the config is deleted", func() {
		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		// mark clusterInstall as deleted to call the finalizer handler
		now := metav1.Now()
		clusterInstall.ObjectMeta.DeletionTimestamp = &now

		filesDir := filepath.Join(dataDir, "namespaces", clusterInstall.Namespace, clusterInstall.Name, "files")
		testFilePath := filepath.Join(filesDir, "testfile")
		Expect(os.MkdirAll(filesDir, 0700)).To(Succeed())
		Expect(os.WriteFile(testFilePath, []byte("stuff"), 0644)).To(Succeed())

		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		_, err = os.Stat(testFilePath)
		Expect(os.IsNotExist(err)).To(BeTrue())

		key := types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).ToNot(ContainElement(clusterInstallFinalizerName))
	})

	It("removes the BMH image url when the config is deleted", func() {
		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Spec: bmh_v1alpha1.BareMetalHostSpec{
				Image: &bmh_v1alpha1.Image{
					URL: "https://service.example.com/namespace/name.iso",
				},
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      bmh.Name,
					Namespace: bmh.Namespace,
				},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		// mark clusterInstall as deleted to call the finalizer handler
		now := metav1.Now()
		clusterInstall.ObjectMeta.DeletionTimestamp = &now

		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		bmhKey := types.NamespacedName{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Get(ctx, bmhKey, bmh)).To(Succeed())
		Expect(bmh.Spec.Image).To(BeNil())

		clusterInstallKey := types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}
		Expect(c.Get(ctx, clusterInstallKey, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).ToNot(ContainElement(clusterInstallFinalizerName))
	})

	It("removes the finalizer if the referenced BMH doesn't exist", func() {
		clusterInstall := &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		// mark clusterInstall as deleted to call the finalizer handler
		now := metav1.Now()
		clusterInstall.ObjectMeta.DeletionTimestamp = &now

		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		clusterInstallKey := types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}
		Expect(c.Get(ctx, clusterInstallKey, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).ToNot(ContainElement(clusterInstallFinalizerName))
	})
})
