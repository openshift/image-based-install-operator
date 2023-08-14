package v1alpha1

import (
	cro "github.com/RHsyseng/cluster-relocation-operator/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ValidateUpdate", func() {
	It("succeeds when BMH ref is not set", func() {
		oldConfig := &ClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ClusterConfigSpec{
				ClusterRelocationSpec: cro.ClusterRelocationSpec{
					Domain:  "thing.example.com",
					SSHKeys: []string{"ssh-rsa sshkeyhere foo@example.com"},
				},
			},
		}
		newConfig := oldConfig.DeepCopy()
		newConfig.Spec.Domain = "stuff.example.com"

		warns, err := newConfig.ValidateUpdate(oldConfig)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds when BMH ref is changed from nil to non-nil", func() {
		oldConfig := &ClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ClusterConfigSpec{
				ClusterRelocationSpec: cro.ClusterRelocationSpec{
					Domain:  "thing.example.com",
					SSHKeys: []string{"ssh-rsa sshkeyhere foo@example.com"},
				},
			},
		}
		newConfig := oldConfig.DeepCopy()
		newConfig.Spec.BareMetalHostRef = &BareMetalHostReference{
			Name:      "test-bmh",
			Namespace: "test-bmh-namespace",
		}

		warns, err := newConfig.ValidateUpdate(oldConfig)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds when BMH ref is changed from non-nil to nil", func() {
		oldConfig := &ClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ClusterConfigSpec{
				ClusterRelocationSpec: cro.ClusterRelocationSpec{
					Domain:  "thing.example.com",
					SSHKeys: []string{"ssh-rsa sshkeyhere foo@example.com"},
				},
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newConfig := oldConfig.DeepCopy()
		newConfig.Spec.BareMetalHostRef = nil

		warns, err := newConfig.ValidateUpdate(oldConfig)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("succeeds when BMH ref updated", func() {
		oldConfig := &ClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ClusterConfigSpec{
				ClusterRelocationSpec: cro.ClusterRelocationSpec{
					Domain:  "thing.example.com",
					SSHKeys: []string{"ssh-rsa sshkeyhere foo@example.com"},
				},
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newConfig := oldConfig.DeepCopy()
		newConfig.Spec.BareMetalHostRef = &BareMetalHostReference{
			Name:      "other-bmh",
			Namespace: "test-bmh-namespace",
		}

		warns, err := newConfig.ValidateUpdate(oldConfig)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("fails when BMH ref is set for non BMH updates", func() {
		oldConfig := &ClusterConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ClusterConfigSpec{
				ClusterRelocationSpec: cro.ClusterRelocationSpec{
					Domain:  "thing.example.com",
					SSHKeys: []string{"ssh-rsa sshkeyhere foo@example.com"},
				},
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newConfig := oldConfig.DeepCopy()
		newConfig.Spec.Domain = "stuff.example.com"

		warns, err := newConfig.ValidateUpdate(oldConfig)
		Expect(warns).To(BeNil())
		Expect(err).ToNot(BeNil())
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
