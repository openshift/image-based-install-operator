package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

var _ = Describe("ValidateUpdate", func() {
	It("succeeds when BMH ref is not set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname:       "test",
				MachineNetwork: "192.168.126.0/24",
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.Hostname = "stuff"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
	It("update succeeds when ssh key is valid and BMH ref is not set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.SSHKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQChSAVWhBU4NBaSI67Gvm0oywPk/dzeh+KlT05VLz3OODld8O0Y95+xg02qMOkNOYz8ucq0BwzTx82DLyl5A/WX64t/Kf2WtnOQ8A02xtVl3LcS9Fzmdi6bA168O/sNKfQ1jeVtZyPBwNkKGgp9qhi/JGVzuwLVV+crMjxsSobsEbHij3FWxLqoNNMPHN8FJFiZZaGbltShheFsepiMf9kY04FZjDKyLrI/rueQWuqhLPfJTOGQktKWEYeLYgKiH82/1x3BNYYuuZxUI0KtQ4S50+M6GSQ1yaR7B7/RI+g/CCwGurccOASqcUtqUDzL53p+Y1ffJfn0WubkxrmNmC/jE0YWDepqDsrLXXdo+k3otWkBx1KhUJ5y/jmJZDkDPVFieqh7yRQ2G1J1ByvBRc4h214PHPztFK63xQ9crsQjlzLCR7esGqJ2iIqoGk1BrXbHlAB9FLPhQXDN+IvpWyO1L02ggGZQnLV7ds0dZApexu2g79HcQrCuKu2W9nPTEZ0= eran@fedora"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
	It("update succeeds when hostname is valid and BMH ref is not set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.Hostname = "other-valid-hostname"
		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
	It("update fail when ssh key is invalid and BMH ref is not set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.SSHKey = "ssh-rsa invalid ssh key"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid ssh key"))
	})
	It("update fail when hostname is invalid and BMH ref is not set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.Hostname = "invalid_hostname&"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid hostname"))
	})
	It("create succeeds when hostname and ssh key are valid and BMH ref is not set", func() {
		newClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
				SSHKey:   "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQChSAVWhBU4NBaSI67Gvm0oywPk/dzeh+KlT05VLz3OODld8O0Y95+xg02qMOkNOYz8ucq0BwzTx82DLyl5A/WX64t/Kf2WtnOQ8A02xtVl3LcS9Fzmdi6bA168O/sNKfQ1jeVtZyPBwNkKGgp9qhi/JGVzuwLVV+crMjxsSobsEbHij3FWxLqoNNMPHN8FJFiZZaGbltShheFsepiMf9kY04FZjDKyLrI/rueQWuqhLPfJTOGQktKWEYeLYgKiH82/1x3BNYYuuZxUI0KtQ4S50+M6GSQ1yaR7B7/RI+g/CCwGurccOASqcUtqUDzL53p+Y1ffJfn0WubkxrmNmC/jE0YWDepqDsrLXXdo+k3otWkBx1KhUJ5y/jmJZDkDPVFieqh7yRQ2G1J1ByvBRc4h214PHPztFK63xQ9crsQjlzLCR7esGqJ2iIqoGk1BrXbHlAB9FLPhQXDN+IvpWyO1L02ggGZQnLV7ds0dZApexu2g79HcQrCuKu2W9nPTEZ0= eran@fedora",
			},
		}

		warns, err := newClusterInstall.ValidateCreate()
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
	It("create fail when ssh key is invalid and BMH ref is not set", func() {
		newClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
				SSHKey:   "ssh-rsa invalid ssh key",
			},
		}

		warns, err := newClusterInstall.ValidateCreate()
		Expect(warns).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid ssh key"))
	})
	It("create fail when hostname is invalid and BMH ref is not set", func() {
		newClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test_not_valid",
			},
		}

		warns, err := newClusterInstall.ValidateCreate()
		Expect(warns).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid hostname"))
	})

	It("create fail when machine network is invalid", func() {
		newClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				MachineNetwork: "test_not_valid",
			},
		}

		warns, err := newClusterInstall.ValidateCreate()
		Expect(warns).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid machine network"))
	})

	It("succeeds when BMH ref is changed from nil to non-nil", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
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
				Hostname: "test",
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
				Hostname: "test",
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
				Hostname: "test",
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.Hostname = "stuff"

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
				Hostname: "test",
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Status.Conditions = []hivev1.ClusterInstallCondition{{
			Type:    hivev1.ClusterInstallRequirementsMet,
			Status:  corev1.ConditionTrue,
			Reason:  ImageReadyReason,
			Message: ImageReadyMessage,
		}}

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("metadata update succeeds when BMH ref is set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.ObjectMeta.Finalizers = []string{"somefinalizer"}

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
				Hostname: "test",
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Status.Conditions = []hivev1.ClusterInstallCondition{{
			Type:    hivev1.ClusterInstallRequirementsMet,
			Status:  corev1.ConditionTrue,
			Reason:  ImageReadyReason,
			Message: ImageReadyMessage,
		}}
		newClusterInstall.Spec.Hostname = "stuff"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).NotTo(BeNil())
	})

	It("allows ClusterMetadata updates when BMH ref is set", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
				BareMetalHostRef: &BareMetalHostReference{
					Name:      "test-bmh",
					Namespace: "test-bmh-namespace",
				},
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.ClusterMetadata = &hivev1.ClusterMetadata{
			ClusterID: "asdf",
			InfraID:   "qwer",
			AdminKubeconfigSecretRef: corev1.LocalObjectReference{
				Name: "secret",
			},
		}
		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
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
