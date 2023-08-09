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
