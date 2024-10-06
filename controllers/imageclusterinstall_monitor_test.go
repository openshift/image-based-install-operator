package controllers

import (
	"context"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/google/uuid"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"

	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/openshift/image-based-install-operator/api/v1alpha1"
	"github.com/openshift/image-based-install-operator/internal/credentials"
	"github.com/openshift/image-based-install-operator/internal/monitor"
	"github.com/sirupsen/logrus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Monitor", func() {
	var (
		c                       client.Client
		dataDir                 string
		r                       *ImageClusterInstallMonitor
		ctx                     = context.Background()
		clusterInstallName      = "test-cluster"
		clusterInstallNamespace = "test-namespace"
		clusterInstall          *v1alpha1.ImageClusterInstall
		clusterDeployment       *hivev1.ClusterDeployment
		bmh                     *bmh_v1alpha1.BareMetalHost
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
		r = &ImageClusterInstallMonitor{
			Client:                       c,
			Log:                          logrus.New(),
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

		bmh = &bmh_v1alpha1.BareMetalHost{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-1",
				Namespace:   "test-bmh-namespace",
				Annotations: map[string]string{ibioManagedBMH: ""},
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
				ClusterMetadata: &hivev1.ClusterMetadata{
					AdminKubeconfigSecretRef: corev1.LocalObjectReference{
						Name: credentials.KubeconfigSecretName(clusterInstallName),
					},
				},
			},
			Status: v1alpha1.ImageClusterInstallStatus{
				BareMetalHostRef: &v1alpha1.BareMetalHostReference{
					Name:      bmh.Name,
					Namespace: bmh.Namespace,
				},
				BootTime: metav1.Now(),
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
		_, err = cm.EnsureKubeconfigSecret(ctx, clusterDeployment)
		Expect(err).NotTo(HaveOccurred())

	})

	AfterEach(func() {
		Expect(os.RemoveAll(dataDir)).To(Succeed())
	})

	It("sets conditions to cluster installed when the BMH is managed and cluster is ready", func() {
		r.GetSpokeClusterInstallStatus = monitor.SuccessMonitor

		//clusterInstall.Status.BareMetalHostRef = &v1alpha1.BareMetalHostReference{
		//	Name:      bmh.Name,
		//	Namespace: bmh.Namespace,
		//}
		//clusterInstall.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
		//	AdminKubeconfigSecretRef: corev1.LocalObjectReference{
		//		Name: credentials.KubeconfigSecretName(clusterDeployment.Name),
		//	},
		//}
		//clusterInstall.Status.BootTime = metav1.Now()
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

		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallStopped)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallFailed)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionTrue))

		By("Verify BMH was detached")
		Expect(c.Get(ctx, types.NamespacedName{Namespace: bmh.Namespace, Name: bmh.Name}, bmh)).To(Succeed())
		Expect(bmh.Annotations[detachedAnnotation]).To(Equal(detachedAnnotationValue))

		By("Verify that clusterInstall was not updated on second run")
		resourceVersion := clusterInstall.ResourceVersion
		res, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		key = types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		Expect(clusterInstall.ObjectMeta.ResourceVersion).To(Equal(resourceVersion))

		By("Verify that bmh was not updated on second run")
		resourceVersion = bmh.ResourceVersion
		Expect(c.Get(ctx, types.NamespacedName{Namespace: bmh.Namespace, Name: bmh.Name}, bmh)).To(Succeed())
		Expect(bmh.ObjectMeta.ResourceVersion).To(Equal(resourceVersion))
	})

	It("requeues and sets conditions when spoke cluster is not ready yet", func() {
		r.GetSpokeClusterInstallStatus = monitor.FailureMonitor

		Expect(c.Create(ctx, clusterInstall)).To(Succeed())
		Expect(c.Create(ctx, clusterDeployment)).To(Succeed())

		key := types.NamespacedName{
			Namespace: clusterInstallNamespace,
			Name:      clusterInstallName,
		}
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
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
		By("Verify BMH was not detached")
		key = types.NamespacedName{
			Namespace: bmh.Namespace,
			Name:      bmh.Name,
		}
		Expect(c.Get(ctx, key, bmh)).To(Succeed())
		Expect(bmh.Annotations).NotTo(HaveKey(detachedAnnotation))

	})

	It("sets conditions to cluster timeout when the default timeout has passed", func() {
		// set negative timeout to ensure it triggers and so that no time is wasted in tests
		r.DefaultInstallTimeout = -time.Minute
		r.GetSpokeClusterInstallStatus = monitor.FailureMonitor

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
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: time.Hour}))

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
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: time.Hour}))

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

	It("sets conditions on cluster timeout when the override timeout has passed", func() {
		r.GetSpokeClusterInstallStatus = monitor.FailureMonitor

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
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: time.Hour}))

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

	It("verify status conditions set while host is not powered on", func() {
		bmh.Status.PoweredOn = false
		Expect(c.Update(ctx, bmh)).To(Succeed())
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
		Expect(res).To(Equal(ctrl.Result{RequeueAfter: time.Minute}))

		Expect(c.Get(ctx, key, clusterInstall)).To(Succeed())
		cond := findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallStopped)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallFailed)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.InstallInProgressReason))
		cond = findCondition(clusterInstall.Status.Conditions, hivev1.ClusterInstallCompleted)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(corev1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1alpha1.InstallInProgressReason))

	})
	It("verify status conditions not set if fail to find BMH", func() {
		Expect(c.Delete(ctx, bmh)).To(Succeed())
		// set negative timeout to ensure it triggers and so that no time is wasted in tests
		clusterInstall.Annotations = map[string]string{installTimeoutAnnotation: "-1m"}
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

		Expect(clusterInstall.Status.Conditions).To(BeNil())
	})

	It("does not set timeout when the default timeout has passed but the cluster is already installed", func() {
		// set negative timeout to ensure it triggers and so that no time is wasted in tests
		r.DefaultInstallTimeout = -time.Minute
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
		Expect(cond.Reason).To(Equal(v1alpha1.InstallSucceededReason))
		Expect(cond.Message).To(Equal(v1alpha1.InstallSucceededMessage))
	})
})
