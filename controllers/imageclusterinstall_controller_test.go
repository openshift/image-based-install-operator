package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/google/uuid"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	lca_api "github.com/openshift-kni/lifecycle-agent/api/seedreconfig"
	apicfgv1 "github.com/openshift/api/config/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/openshift/image-based-install-operator/internal/certs"
	"github.com/openshift/image-based-install-operator/internal/credentials"
	"github.com/openshift/image-based-install-operator/internal/monitor"
	"github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const validNMStateConfig = `
interfaces:
  - name: enp1s0
    type: ethernet
    state: up
    identifier: mac-address
    mac-address: 52:54:00:8A:88:A8
    ipv4:
      address:
        - ip: 192.168.136.15
          prefix-length: "24"
      enabled: true
    ipv6:
      enabled: false
routes:
  config:
    - destination: 0.0.0.0/0
      next-hop-address: 192.168.136.1
      next-hop-interface: enp1s0
      table-id: 254
dns-resolver:
  config:
    server:
      - 192.168.136.1
`

const validNMStateConfigBMH = `
interfaces:
  - name: enp1s0
    type: ethernet
    state: up
    identifier: mac-address
    mac-address: 52:54:00:8A:88:A8
    ipv4:
      address:
        - ip: 192.168.136.138
          prefix-length: "24"
      enabled: true
    ipv6:
      enabled: false
routes:
  config:
    - destination: 0.0.0.0/0
      next-hop-address: 192.168.136.1
      next-hop-interface: enp1s0
      table-id: 254
dns-resolver:
  config:
    server:
      - 192.168.136.1
`

func bmhInState(state bmh_v1alpha1.ProvisioningState) *bmh_v1alpha1.BareMetalHost {
	return &bmh_v1alpha1.BareMetalHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-bmh",
			Namespace: "test-bmh-namespace",
		},
		Status: bmh_v1alpha1.BareMetalHostStatus{
			Provisioning: bmh_v1alpha1.ProvisionStatus{
				State: state,
			},
			HardwareDetails: &bmh_v1alpha1.HardwareDetails{NIC: []bmh_v1alpha1.NIC{{IP: "1.1.1.1"}}},
			PoweredOn:       state == bmh_v1alpha1.StateExternallyProvisioned,
		},
		Spec: bmh_v1alpha1.BareMetalHostSpec{
			ExternallyProvisioned: state == bmh_v1alpha1.StateExternallyProvisioned,
		},
	}
}

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
		pullSecret              *corev1.Secret
		testPullSecretVal       = `{"auths":{"cloud.openshift.com":{"auth":"dXNlcjpwYXNzd29yZAo=","email":"r@r.com"}}}`
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha1.ImageClusterInstall{}).
			Build()
		var err error
		dataDir, err = os.MkdirTemp("", "imageclusterinstall_controller_test_data")
		Expect(err).NotTo(HaveOccurred())
		cm := credentials.Credentials{
			Client: c,
			Log:    logrus.New(),
			Scheme: scheme.Scheme,
		}
		r = &ImageClusterInstallReconciler{
			Client:      c,
			Credentials: cm,
			Scheme:      scheme.Scheme,
			Log:         logrus.New(),
			BaseURL:     "https://images-namespace.cluster.example.com",
			Options: &ImageClusterInstallReconcilerOptions{
				DataDir: dataDir,
			},
			CertManager:                  certs.KubeConfigCertManager{},
			DefaultInstallTimeout:        time.Hour,
			GetSpokeClusterInstallStatus: monitor.SuccessMonitor,
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
		pullSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ps",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(testPullSecretVal)},
		}
		Expect(c.Create(ctx, pullSecret)).To(Succeed())

		bmh := &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-1",
				Namespace: "test-bmh-namespace",
			},
			Status: bmh_v1alpha1.BareMetalHostStatus{
				Provisioning: bmh_v1alpha1.ProvisionStatus{
					State: bmh_v1alpha1.StateExternallyProvisioned,
				},
				HardwareDetails: &bmh_v1alpha1.HardwareDetails{NIC: []bmh_v1alpha1.NIC{{IP: "1.1.1.1"}}},
				PoweredOn:       true,
			},
			Spec: bmh_v1alpha1.BareMetalHostSpec{
				ExternallyProvisioned: true,
			},
		}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall = &v1alpha1.ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:       clusterInstallName,
				Namespace:  clusterInstallNamespace,
				Finalizers: []string{clusterInstallFinalizerName},
				UID:        types.UID(uuid.New().String()),
			},
			Spec: v1alpha1.ImageClusterInstallSpec{
				ImageSetRef: hivev1.ClusterImageSetReference{
					Name: imageSet.Name,
				},
				ClusterDeploymentRef: &corev1.LocalObjectReference{Name: clusterInstallName},
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      bmh.Name,
					Namespace: bmh.Namespace,
				},
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
				PullSecretRef: &corev1.LocalObjectReference{
					Name: "ps",
				},
			}}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dataDir)).To(Succeed())
	})

	imageURL := func() string {
		return fmt.Sprintf("https://images-namespace.cluster.example.com/images/%s/%s.iso", clusterInstallNamespace, clusterInstall.ObjectMeta.UID)
	}

	outputFilePath := func(elem ...string) string {
		last := filepath.Join(elem...)
		return filepath.Join(dataDir, "namespaces", clusterInstallNamespace, string(clusterInstall.ObjectMeta.UID), "files", last)
	}

	validateExtraManifestContent := func(file string, data string) {
		content, err := os.ReadFile(outputFilePath(extraManifestsDir, file))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(data))
	}

	It("creates the correct cluster info manifest", func() {
		clusterInstall.Spec.MachineNetwork = "192.0.2.0/24"
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
		Expect(infoOut.MachineNetwork).To(Equal(clusterInstall.Spec.MachineNetwork))
		Expect(infoOut.ReleaseRegistry).To(Equal("registry.example.com"))
		Expect(infoOut.Hostname).To(Equal(clusterInstall.Spec.Hostname))
		Expect(infoOut.SSHKey).To(Equal(clusterInstall.Spec.SSHKey))
		Expect(infoOut.PullSecret).To(Equal(testPullSecretVal))
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
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		Expect(c.Create(ctx, bmh)).To(Succeed())
		baseDomain := "example.com"
		clusterName := "thingcluster"

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}

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
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: 1 * time.Minute}))
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
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: 1 * time.Minute}))
		content, err = os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut = &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(clusterCrypto).ToNot(Equal(infoOut.KubeconfigCryptoRetention))

	})
	It("pullSecret not set", func() {
		clusterDeployment.Spec.PullSecretRef = nil
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing reference to pull secret"))
		Expect(res).To(Equal(ctrl.Result{}))
	})
	It("missing pullSecret", func() {
		clusterDeployment.Spec.PullSecretRef.Name = "nonExistingPS"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to find secret"))
		Expect(res).To(Equal(ctrl.Result{}))
	})
	It("pullSecret missing dockerconfigjson key", func() {
		pullSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ps",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string][]byte{},
		}
		Expect(c.Update(ctx, pullSecret)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("secret ps did not contain key .dockerconfigjson"))
		Expect(res).To(Equal(ctrl.Result{}))
	})
	It("malformed pullSecret", func() {
		pullSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ps",
				Namespace: clusterInstallNamespace,
			},
			Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte("garbage")},
		}
		Expect(c.Update(ctx, pullSecret)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid pull secret data in secret pull secret must be a well-formed JSON"))
		Expect(res).To(Equal(ctrl.Result{}))
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

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect("mycabundle").To(Equal(infoOut.AdditionalTrustBundle.UserCaBundle))

	})
	It("creates the invoker CM", func() {
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifests", invokerCMFileName))
		Expect(err).NotTo(HaveOccurred())
		cm := corev1.ConfigMap{}
		json.Unmarshal(content, &cm)
		Expect(cm.Data["invoker"]).To(Equal(imageBasedInstallInvoker))
	})
	It("creates the imageDigestMirrorSet", func() {
		imageDigestMirrors := []apicfgv1.ImageDigestMirrors{
			{
				Source: "registry.ci.openshift.org/ocp/release",
				Mirrors: []apicfgv1.ImageMirror{
					"virthost.ostest.test.metalkube.org:5000/localimages/local-release-image",
				},
			},
			{
				Source: "quay.io/openshift-release-dev/ocp-v4.0-art-dev",
				Mirrors: []apicfgv1.ImageMirror{
					"virthost.ostest.test.metalkube.org:5000/localimages/local-release-image",
				},
			},
		}
		clusterInstall.Spec.ImageDigestSources = imageDigestMirrors
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifests", imageDigestMirrorSetFileName))
		Expect(err).NotTo(HaveOccurred())
		expectedImageDigestMirrorSet := &apicfgv1.ImageDigestMirrorSet{
			TypeMeta: metav1.TypeMeta{
				APIVersion: apicfgv1.SchemeGroupVersion.String(),
				Kind:       "ImageDigestMirrorSet",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "image-digest-mirror",
				// not namespaced
			},
			Spec: apicfgv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: imageDigestMirrors,
			},
		}

		expectedcontent, err := json.Marshal(expectedImageDigestMirrorSet)
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(Equal(expectedcontent))
	})

	It("copies the nmstate config bmh preprovisioningNetworkDataName", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "netconfig",
				Namespace: bmh.Namespace,
			},
			Data: map[string][]byte{nmstateSecretKey: []byte(validNMStateConfigBMH)},
		}
		Expect(c.Create(ctx, secret)).To(Succeed())

		bmh.Spec.PreprovisioningNetworkDataName = "netconfig"
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

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(infoOut.RawNMStateConfig).To(Equal(validNMStateConfigBMH))
	})
	It("fails if nmstate config is bad yaml", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		invalidNmstateString := "some\nnmstate\nstring"
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "netconfig",
				Namespace: bmh.Namespace,
			},
			Data: map[string][]byte{nmstateSecretKey: []byte(invalidNmstateString)},
		}
		Expect(c.Create(ctx, secret)).To(Succeed())

		bmh.Spec.PreprovisioningNetworkDataName = "netconfig"
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
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
	})
	It("fails when a referenced nmstate secret is missing", func() {
		// note that we don't create the netconfig secret.
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Spec.PreprovisioningNetworkDataName = "netconfig"
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
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
	})
	It("fails when the referenced nmstate secret is missing the required key", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "netconfig",
				Namespace: bmh.Namespace,
			},
			Data: map[string][]byte{"wrongKey": []byte(validNMStateConfigBMH)},
		}
		Expect(c.Create(ctx, secret)).To(Succeed())
		bmh.Spec.PreprovisioningNetworkDataName = "netconfig"
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
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
	})
	It("when a referenced clusterdeployment is missing", func() {
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Message).To(Equal("clusterDeployment with name 'test-cluster' in namespace 'test-namespace' not found"))
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
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: clusterInstallNamespace, Name: clusterDeployment.Name + "-admin-kubeconfig"}, kubeconfigSecret)
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

	It("creates kubeadmin password", func() {
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
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: clusterInstallNamespace, Name: clusterDeployment.Name + "-admin-password"}, kubeconfigSecret)
		Expect(err).NotTo(HaveOccurred())
		username, exists := kubeconfigSecret.Data["username"]
		Expect(exists).To(BeTrue())
		Expect(string(username)).To(Equal(credentials.DefaultUser))
		_, exists = kubeconfigSecret.Data["password"]
		Expect(exists).To(BeTrue())

	})

	It("configures a referenced BMH with state registering and ExternallyProvisioned true", func() {
		bmh := bmhInState(bmh_v1alpha1.StateRegistering)
		bmh.Spec.ExternallyProvisioned = true
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
		Expect(bmh.Spec.Image).To(BeNil())
		// We don't update the power state of a BMH with registering provisioning state
		Expect(bmh.Spec.Online).To(BeFalse())
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})

	It("configures a referenced BMH with state available, ExternallyProvisioned false and online true", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Spec.Online = true
		bmh.Spec.ExternallyProvisioned = false
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
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.ExternallyProvisioned).To(BeTrue())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})
	It("configures a referenced BMH with state available, ExternallyProvisioned false and online false", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Spec.Online = false
		bmh.Spec.ExternallyProvisioned = false
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
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.ExternallyProvisioned).To(BeTrue())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})
	It("configures a referenced BMH with state externally provisioned and online false", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		bmh.Spec.ExternallyProvisioned = true
		bmh.Status.PoweredOn = false
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
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})
	It("configures a referenced BMH with status externally provisioned and online true", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		bmh.Spec.ExternallyProvisioned = true
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
		Expect(bmh.Spec.Image).To(BeNil())
		Expect(bmh.Spec.Online).To(BeTrue())
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))
		Expect(bmh.Annotations).To(HaveKey(ibioManagedBMH))
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})

	It("sets the BMH ref in the cluster install status", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
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

	It("validate image is not cleanuped on data change if bmh started installation", func() {
		r.GetSpokeClusterInstallStatus = monitor.FailureMonitor
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}

		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))

		By("Changing cluster install params nothing should change in bmh")
		Expect(c.Get(ctx, req.NamespacedName, clusterInstall)).To(Succeed())
		clusterInstall.Spec.Hostname = "test"
		Expect(c.Update(ctx, clusterInstall)).To(Succeed())
		res, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: 1 * time.Minute}))
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		dataImage = bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))

		By("verify image not cleanuped in case bmh is provisioned")
		bmh.Status.Provisioning.State = bmh_v1alpha1.StateProvisioned
		Expect(c.Update(ctx, bmh)).To(Succeed())

		Expect(c.Get(ctx, req.NamespacedName, clusterInstall)).To(Succeed())
		clusterInstall.Spec.Hostname = "test2"
		Expect(c.Update(ctx, clusterInstall)).To(Succeed())
		res, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		res, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: 1 * time.Minute}))
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		dataImage = bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))
	})

	It("sets detached on a referenced BMH after it is externally provisioned", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		bmh.Spec.Online = true
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: clusterInstallNamespace,
				Name:      clusterInstallName,
			},
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(c.Get(ctx, key, bmh)).To(Succeed())

		dataImage := bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(HaveOccurred())
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		dataImage = bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(Succeed())
		Expect(dataImage.Spec.URL).To(Equal(imageURL()))

		res, err = r.Reconcile(ctx, req)
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Annotations[detachedAnnotation]).To(Equal(detachedAnnotationValue))

		By("Verify that bmh was not updated on third run")
		resourceVersion := bmh.ResourceVersion
		res, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.ObjectMeta.ResourceVersion).To(Equal(resourceVersion))
	})

	It("sets disables AutomatedCleaningMode", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
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
		Expect(bmh.Spec.AutomatedCleaningMode).To(Equal(bmh_v1alpha1.CleaningModeDisabled))
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
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Message).To(Equal("clusterDeploymentRef is unset"))

	})

	It("sets conditions to show cluster installed when the host can be configured and cluster is ready", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)

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

		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())

		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallStopped)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallFailed)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		resourceVersion := clusterInstall.ResourceVersion

		By("Verify that clusterInstall was not updated on second run")
		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		Expect(clusterInstall.ObjectMeta.ResourceVersion).To(Equal(resourceVersion))
	})

	It("requeues and sets conditions when spoke cluster is not ready yet", func() {
		r.GetSpokeClusterInstallStatus = monitor.FailureMonitor

		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
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

		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(time.Minute))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())

		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallStopped)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Message).To(Equal("Cluster is installing\nClusterVersion Status: Cluster version is not available\nNodes Status: Node test is NotReady"))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallFailed)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))

		By("Verify that clusterInstall was not updated on second run")
		resourceVersion := clusterInstall.ResourceVersion
		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(time.Minute))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		Expect(clusterInstall.ObjectMeta.ResourceVersion).To(Equal(resourceVersion))
	})

	It("sets conditions to show cluster timeout when the default timeout has passed", func() {
		// set negative timeout to ensure it triggers and so that no time is wasted in tests
		r.DefaultInstallTimeout = -time.Minute
		r.GetSpokeClusterInstallStatus = monitor.FailureMonitor
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
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

		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())

		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallStopped)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallFailed)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.InstallTimedoutReason))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.InstallTimedoutReason))
	})

	It("sets cluster installed in case timeout was set but cluster succeeded after it", func() {
		// set negative timeout to ensure it triggers and so that no time is wasted in tests
		r.DefaultInstallTimeout = -time.Minute
		r.GetSpokeClusterInstallStatus = monitor.FailureMonitor
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
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

		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())

		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallStopped)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallFailed)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.InstallTimedoutReason))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.InstallTimedoutReason))

		By("Successful run after cluster was timed out should still set installed")
		r.GetSpokeClusterInstallStatus = monitor.SuccessMonitor

		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())

		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallStopped)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallFailed)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
	})

	It("sets conditions to show cluster timeout when the override timeout has passed", func() {
		r.GetSpokeClusterInstallStatus = monitor.FailureMonitor
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)

		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		// set negative timeout to ensure it triggers and so that no time is wasted in tests
		clusterInstall.Annotations = map[string]string{installTimeoutAnnotation: "-1m"}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())

		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallStopped)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallFailed)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.InstallTimedoutReason))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.InstallTimedoutReason))
	})

	It("verify status not set till host is not provisioned", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		// set negative timeout to ensure it triggers and so that no time is wasted in tests
		clusterInstall.Annotations = map[string]string{installTimeoutAnnotation: "-1m"}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: 1 * time.Minute}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())

		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallStopped)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionUnknown))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallFailed)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionUnknown))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionUnknown))
	})

	It("does not set timeout when the default timeout has passed but the cluster is already installed", func() {
		// set negative timeout to ensure it triggers and so that no time is wasted in tests
		r.DefaultInstallTimeout = -time.Minute

		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Status = v1alpha1.ImageClusterInstallStatus{
			Conditions: []hivev1.ClusterInstallCondition{
				{
					Type:    hivev1.ClusterInstallCompleted,
					Status:  corev1.ConditionTrue,
					Reason:  v1alpha1.InstallSucceededReason,
					Message: v1alpha1.InstallSucceededMessage,
				},
			},
		}

		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		clusterDeployment.Spec.Installed = true
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())

		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).NotTo(Equal(v1alpha1.InstallTimedoutReason))
	})

	It("sets the ClusterInstallRequirementsMet condition to true when the host is missing", func() {
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
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.HostConfiguraionFailedReason))
	})

	It("updates the cluster install and cluster deployment metadata", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
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

		updatedICI := v1alpha1.ImageClusterInstall{}
		Expect(c.Get(ctx, key, &updatedICI)).To(Succeed())
		meta := updatedICI.Spec.ClusterMetadata
		Expect(meta).ToNot(BeNil())
		Expect(meta.ClusterID).To(Equal(infoOut.ClusterID))
		Expect(meta.InfraID).To(HavePrefix("thingcluster"))
		Expect(meta.InfraID).To(Equal(infoOut.InfraID))
		Expect(meta.AdminKubeconfigSecretRef.Name).To(Equal("test-cluster-admin-kubeconfig"))
		Expect(meta.AdminPasswordSecretRef.Name).To(Equal("test-cluster-admin-password"))
	})

	It("sets the clusterID to the manifest.json value if it exists", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
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
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
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

	It("succeeds in case bmh has ip in provided machine network", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Status.HardwareDetails.NIC = []bmh_v1alpha1.NIC{{IP: "192.168.1.30"}}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(infoOut.MachineNetwork).To(Equal(clusterInstall.Spec.MachineNetwork))
	})

	It("succeeds in case bmh has disabled inspection and no hw details", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Status.HardwareDetails = nil
		if bmh.ObjectMeta.Annotations == nil {
			bmh.ObjectMeta.Annotations = make(map[string]string)
		}
		bmh.ObjectMeta.Annotations[inspectAnnotation] = "disabled"
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(infoOut.MachineNetwork).To(Equal(clusterInstall.Spec.MachineNetwork))
	})

	It("fails in case there is not actual bmh under the reference", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("baremetalhosts.metal3.io \"test-bmh\" not found"))
	})

	It("reque in case bmh has no hw details but after adding them it succeeds", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Status.HardwareDetails = nil
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(Equal(ctrl.Result{}))
		Expect(res.RequeueAfter).To(Equal(30 * time.Second))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationPending))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).ToNot(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(infoOut.MachineNetwork).To(Equal(clusterInstall.Spec.MachineNetwork))

		// good one
		Expect(c.Get(ctx, types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}, bmh)).To(Succeed())

		bmh.Status.HardwareDetails = &bmh_v1alpha1.HardwareDetails{NIC: []bmh_v1alpha1.NIC{
			{IP: "192.168.50.30"},
			{IP: "192.168.1.30"}}}

		Expect(c.Update(ctx, bmh)).To(Succeed())
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())

		content, err = os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(infoOut.MachineNetwork).To(Equal(clusterInstall.Spec.MachineNetwork))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationSucceeded))
	})

	It("fails in case bmh has no ip in provided machine network but after changing machine network it succeeds", func() {
		bmh := bmhInState(bmh_v1alpha1.StateAvailable)
		bmh.Status.HardwareDetails.NIC = []bmh_v1alpha1.NIC{{IP: "192.168.1.30"}}
		Expect(c.Create(ctx, bmh)).To(Succeed())

		clusterInstall.Spec.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
			Name:      bmh.Name,
			Namespace: bmh.Namespace,
		}
		// bad machine network
		clusterInstall.Spec.MachineNetwork = "192.168.5.0/24"
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		clusterDeployment.Spec.ClusterName = "thingcluster"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("bmh host doesn't have any nic with ip in provided"))

		content, err := os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).ToNot(HaveOccurred())
		infoOut := &lca_api.SeedReconfiguration{}
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(infoOut.MachineNetwork).To(Equal(clusterInstall.Spec.MachineNetwork))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationFailedReason))

		// good one
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		clusterInstall.Spec.MachineNetwork = "192.168.1.0/24"
		Expect(c.Update(ctx, clusterInstall)).To(Succeed())

		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())

		content, err = os.ReadFile(outputFilePath(clusterConfigDir, "manifest.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(content, infoOut)).To(Succeed())
		Expect(infoOut.MachineNetwork).To(Equal(clusterInstall.Spec.MachineNetwork))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallRequirementsMet)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1alpha1.HostValidationSucceeded))
	})

	It("labels secrets for backup", func() {
		clusterInstall.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
			AdminKubeconfigSecretRef: corev1.LocalObjectReference{Name: clusterDeployment.Name + "-admin-kubeconfig"},
			AdminPasswordSecretRef:   &corev1.LocalObjectReference{Name: clusterDeployment.Name + "-admin-password"},
		}
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		clusterDeployment.Spec.ClusterName = "test"
		clusterDeployment.Spec.BaseDomain = "example.com"
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		secretRefs := []types.NamespacedName{
			{Namespace: pullSecret.Namespace, Name: pullSecret.Name},
			{Namespace: clusterInstallNamespace, Name: clusterInstall.Spec.ClusterMetadata.AdminKubeconfigSecretRef.Name},
			{Namespace: clusterInstallNamespace, Name: clusterInstall.Spec.ClusterMetadata.AdminPasswordSecretRef.Name},
		}
		for _, key := range secretRefs {
			testSecret := &corev1.Secret{}
			Expect(c.Get(ctx, key, testSecret)).To(Succeed())
			Expect(testSecret.GetLabels()).To(HaveKeyWithValue(backupLabel, backupLabelValue), "Secret %s/%s missing annotation", testSecret.Namespace, testSecret.Name)
		}
	})

	It("labels configmaps for backup", func() {
		configMaps := []*corev1.ConfigMap{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "manifest1",
					Namespace: clusterInstallNamespace,
				},
				Data: map[string]string{
					"manifest1.yaml": "thing: stuff",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "manifest2",
					Namespace: clusterInstallNamespace,
				},
				Data: map[string]string{
					"manifest2.yaml": "other: foo",
				},
			},
		}
		clusterInstall.Spec.ExtraManifestsRefs = []corev1.LocalObjectReference{
			{Name: "manifest1"},
			{Name: "manifest2"},
		}
		configMaps = append(configMaps,
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ca-bundle",
					Namespace: "test-namespace",
				},
				Data: map[string]string{caBundleFileName: "mycabundle"},
			},
		)
		clusterInstall.Spec.CABundleRef = &corev1.LocalObjectReference{
			Name: "ca-bundle",
		}

		for _, cm := range configMaps {
			Expect(c.Create(ctx, cm)).To(Succeed())
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

		for _, cm := range configMaps {
			testCM := &corev1.ConfigMap{}
			key := types.NamespacedName{
				Name:      cm.Name,
				Namespace: cm.Namespace,
			}
			Expect(c.Get(ctx, key, testCM)).To(Succeed())
			Expect(testCM.GetLabels()).To(HaveKeyWithValue(backupLabel, backupLabelValue), "ConfigMap %s/%s missing annotation", testCM.Namespace, testCM.Name)
		}
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

		dataImage := bmh_v1alpha1.DataImage{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bmh",
				Namespace: "test-bmh-namespace",
			},
			Spec: bmh_v1alpha1.DataImageSpec{
				URL: fmt.Sprintf("https://images-namespace.cluster.example.com/images/%s/%s.iso", clusterInstallNamespace, clusterInstall.ObjectMeta.UID),
			},
		}
		Expect(c.Create(ctx, &dataImage)).To(Succeed())

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
		key := types.NamespacedName{
			Namespace: dataImage.Namespace,
			Name:      dataImage.Name,
		}
		dataImage = bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(HaveOccurred())

	})

	It("removes dataimage on ici delete", func() {
		bmh := bmhInState(bmh_v1alpha1.StateExternallyProvisioned)
		bmh.Spec.Online = true
		setAnnotationIfNotExists(&bmh.ObjectMeta, detachedAnnotation, detachedAnnotationValue)

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
		Expect(c.Create(ctx, bmh)).To(Succeed())
		Expect(c.Create(ctx, clusterInstall)).To(Succeed())

		dataImage := bmh_v1alpha1.DataImage{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bmh.Name,
				Namespace: bmh.Namespace,
			},
			Spec: bmh_v1alpha1.DataImageSpec{
				URL: fmt.Sprintf("https://images-namespace.cluster.example.com/images/%s/%s.iso", clusterInstallNamespace, clusterInstall.ObjectMeta.UID),
			},
		}
		Expect(c.Create(ctx, &dataImage)).To(Succeed())

		// Mark clusterInstall as deleted to call the finalizer handler
		now := metav1.Now()
		clusterInstall.ObjectMeta.DeletionTimestamp = &now

		// First round will delete the dataImage (mark for deletion) and ask the bmh to reboot
		res, stop, err := r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: 1 * time.Minute}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		clusterInstallKey := types.NamespacedName{
			Name:      clusterInstallName,
			Namespace: clusterInstallNamespace,
		}
		// Validate the finalizer wasn't removed yet
		Expect(c.Get(ctx, clusterInstallKey, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).To(ContainElement(clusterInstallFinalizerName))

		key := types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}

		// Validate the bmh is now attached and got the reboot annotation
		bmh = &bmh_v1alpha1.BareMetalHost{}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Annotations).ToNot(HaveKey(detachedAnnotation))
		Expect(bmh.Annotations).To(HaveKey(rebootAnnotation))

		dataImage = bmh_v1alpha1.DataImage{}
		Expect(c.Get(ctx, key, &dataImage)).To(HaveOccurred())
		// Once the dataImage is deleted the finalizer should be removed

		// Mark clusterInstall as deleted to call the finalizer handler
		clusterInstall.ObjectMeta.DeletionTimestamp = &now
		res, stop, err = r.handleFinalizer(ctx, r.Log, clusterInstall)
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(stop).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		// Validate the finalizer get removed after the data image is deleted
		Expect(c.Get(ctx, clusterInstallKey, clusterInstall)).To(Succeed())
		Expect(clusterInstall.GetFinalizers()).ToNot(ContainElement(clusterInstallFinalizerName))
	})
})

var _ = Describe("proxy", func() {
	var (
		r *ImageClusterInstallReconciler
	)

	BeforeEach(func() {

		r = &ImageClusterInstallReconciler{
			Client: nil,
			Scheme: scheme.Scheme,
			Log:    logrus.New(),
		}
	})

	It("Proxy is nil, nothing to change", func() {
		Expect(r.proxy(nil)).To(BeNil())
	})

	It("If https and http proxy were not set, nothing to set", func() {
		Expect(r.proxy(&v1alpha1.Proxy{})).To(BeNil())
	})

	It("Verify no proxy was set", func() {
		iciProxy := &v1alpha1.Proxy{HTTPSProxy: "aaa", NoProxy: "test"}
		proxy := r.proxy(iciProxy)
		Expect(proxy.HTTPSProxy).To(Equal(iciProxy.HTTPSProxy))
		Expect(proxy.NoProxy).To(Equal(iciProxy.NoProxy))
	})
})
