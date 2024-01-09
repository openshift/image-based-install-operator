package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/lifecycle-agent/ibu-imager/clusterinfo"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ValidateUpdate", func() {
	It("succeeds when BMH ref is not set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				ClusterInfo: clusterinfo.ClusterInfo{Domain: "thing.example.com"},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.Domain = "stuff.example.com"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds when BMH ref is changed from nil to non-nil", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				ClusterInfo: clusterinfo.ClusterInfo{Domain: "thing.example.com"},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.BareMetalHostRef = &BareMetalHostReference{
			Name:      "test-bmh",
			Namespace: "test-bmh-namespace",
		}

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds when BMH ref is changed from non-nil to nil", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				ClusterInfo: clusterinfo.ClusterInfo{Domain: "thing.example.com"},
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.BareMetalHostRef = nil

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds when BMH ref updated", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				ClusterInfo: clusterinfo.ClusterInfo{Domain: "thing.example.com"},
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.BareMetalHostRef = &BareMetalHostReference{
			Name:      "other-bmh",
			Namespace: "test-bmh-namespace",
		}

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("fails when BMH ref is set for non BMH updates", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				ClusterInfo: clusterinfo.ClusterInfo{Domain: "thing.example.com"},
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.Domain = "stuff.example.com"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).ToNot(BeNil())
	})
	It("succeeds status update when BMH ref is set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				ClusterInfo: clusterinfo.ClusterInfo{Domain: "thing.example.com"},
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		cond := metav1.Condition{
			Type:    ImageReadyCondition,
			Status:  metav1.ConditionTrue,
			Reason:  ImageReadyReason,
			Message: ImageReadyMessage,
		}
		meta.SetStatusCondition(&newClusterInstall.Status.Conditions, cond)

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
	It("fail status and spec update when BMH ref is set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				ClusterInfo: clusterinfo.ClusterInfo{Domain: "thing.example.com"},
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		cond := metav1.Condition{
			Type:    ImageReadyCondition,
			Status:  metav1.ConditionTrue,
			Reason:  ImageReadyReason,
			Message: ImageReadyMessage,
		}
		meta.SetStatusCondition(&newClusterInstall.Status.Conditions, cond)
		newClusterInstall.Spec.Domain = "stuff.example.com"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).NotTo(BeNil())
	})

})

var _ = Describe("BMHRefsMatch", func() {
	var ref1, ref2 *BareMetalHostReference
	BeforeEach(func() {
		ref1 = &BareMetalHostReference{Name: "bmh", Namespace: "test"}
		ref2 = &BareMetalHostReference{Name: "other-bmh", Namespace: "test"}
	})

	It("returns true when both are nil", func() {
		Expect(BMHRefsMatch(nil, nil)).To(Equal(true))
	})
	It("returns true when refs match", func() {
		Expect(BMHRefsMatch(ref1, ref1.DeepCopy())).To(Equal(true))
	})
	It("returns false when refs do not match", func() {
		Expect(BMHRefsMatch(ref1, ref2)).To(Equal(false))
	})
	It("returns false for nil and set refs", func() {
		Expect(BMHRefsMatch(nil, ref2)).To(Equal(false))
	})
	It("returns false for set and nil refs", func() {
		Expect(BMHRefsMatch(ref1, nil)).To(Equal(false))
	})
})
