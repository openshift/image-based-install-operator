package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift-kni/lifecycle-agent/ibu-imager/clusterinfo"
	relocationv1alpha1 "github.com/openshift/cluster-relocation-service/api/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
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
		clusterInstall          *relocationv1alpha1.ImageClusterInstall
		clusterDeployment       *hivev1.ClusterDeployment
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&relocationv1alpha1.ImageClusterInstall{}).
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
		}

		clusterInstall = &relocationv1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
			Spec: relocationv1alpha1.ImageClusterInstallSpec{
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
		info := clusterinfo.ClusterInfo{
			Domain:          "example.com",
			ClusterName:     "thingcluster",
			MasterIP:        "192.0.2.1",
			ReleaseRegistry: "registry.example.com",
			Hostname:        "thing",
		}
		clusterInstall.Spec.MasterIP = info.MasterIP
		clusterInstall.Spec.ReleaseRegistry = info.ReleaseRegistry
		clusterInstall.Spec.Hostname = info.Hostname
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = info.ClusterName
		clusterDeployment.Spec.BaseDomain = info.Domain
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
		infoOut := &clusterinfo.ClusterInfo{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())

		Expect(*infoOut).To(Equal(info))
	})

	It("creates the pull secret", func() {
		pullSecretData := map[string][]byte{"pullsecret": []byte("pullsecret")}
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-pull-secret",
				Namespace: clusterInstallNamespace,
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

		clusterInstall.Spec.BareMetalHostRef = &relocationv1alpha1.BareMetalHostReference{
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

		clusterInstall.Spec.BareMetalHostRef = &relocationv1alpha1.BareMetalHostReference{
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

		clusterInstall.Spec.BareMetalHostRef = &relocationv1alpha1.BareMetalHostReference{
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
		cond := meta.FindStatusCondition(clusterInstall.Status.ConfigConditions, relocationv1alpha1.ImageReadyCondition)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(relocationv1alpha1.ImageReadyReason))
		Expect(cond.Message).To(Equal(relocationv1alpha1.ImageReadyMessage))
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

		clusterInstall.Spec.BareMetalHostRef = &relocationv1alpha1.BareMetalHostReference{
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
		cond := meta.FindStatusCondition(clusterInstall.Status.ConfigConditions, relocationv1alpha1.HostConfiguredCondition)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(relocationv1alpha1.HostConfiguraionSucceededReason))
		Expect(cond.Message).To(Equal(relocationv1alpha1.HostConfigurationSucceededMessage))
	})

	It("sets the host configured condition to false when the host is missing", func() {
		clusterInstall.Spec.BareMetalHostRef = &relocationv1alpha1.BareMetalHostReference{
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
		cond := meta.FindStatusCondition(clusterInstall.Status.ConfigConditions, relocationv1alpha1.HostConfiguredCondition)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(relocationv1alpha1.HostConfiguraionFailedReason))
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

		clusterInstall.Status = relocationv1alpha1.ImageClusterInstallStatus{
			BareMetalHostRef: &relocationv1alpha1.BareMetalHostReference{
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

		clusterInstall.Spec.BareMetalHostRef = &relocationv1alpha1.BareMetalHostReference{
			Name:      newBMH.Name,
			Namespace: newBMH.Namespace,
		}
		clusterInstall.Status = relocationv1alpha1.ImageClusterInstallStatus{
			BareMetalHostRef: &relocationv1alpha1.BareMetalHostReference{
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
			WithStatusSubresource(&relocationv1alpha1.ImageClusterInstall{}).
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

		clusterInstall := &relocationv1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
			Spec: relocationv1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &relocationv1alpha1.BareMetalHostReference{
					Name:      bmh.Name,
					Namespace: bmh.Namespace,
				},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterInstall = &relocationv1alpha1.ImageClusterInstall{
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

		clusterInstall := &relocationv1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterInstallName,
				Namespace: clusterInstallNamespace,
			},
			Spec: relocationv1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &relocationv1alpha1.BareMetalHostReference{
					Name:      "other-bmh",
					Namespace: bmh.Namespace,
				},
			},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterInstall = &relocationv1alpha1.ImageClusterInstall{
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
			WithStatusSubresource(&relocationv1alpha1.ImageClusterInstall{}).
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
					Group:   relocationv1alpha1.Group,
					Version: relocationv1alpha1.Version,
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
			WithStatusSubresource(&relocationv1alpha1.ImageClusterInstall{}).
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
		clusterInstall := &relocationv1alpha1.ImageClusterInstall{
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
		clusterInstall := &relocationv1alpha1.ImageClusterInstall{
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
		clusterInstall := &relocationv1alpha1.ImageClusterInstall{
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

		clusterInstall := &relocationv1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
			Spec: relocationv1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &relocationv1alpha1.BareMetalHostReference{
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
		clusterInstall := &relocationv1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
			},
			Spec: relocationv1alpha1.ImageClusterInstallSpec{
				BareMetalHostRef: &relocationv1alpha1.BareMetalHostReference{
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
