package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
)

var _ = Describe("ValidateUpdate", func() {

	setClusterInstallCondition := func(conditionType hivev1.ClusterInstallConditionType, status corev1.ConditionStatus, reason string) []hivev1.ClusterInstallCondition {
		return []hivev1.ClusterInstallCondition{
			{
				Type:    conditionType,
				Status:  status,
				Reason:  reason,
				Message: "",
			},
		}
	}
	It("update succeeds when ssh key is valid and image isn't ready", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
			},
			Status: ImageClusterInstallStatus{
				Conditions: setClusterInstallCondition(hivev1.ClusterInstallRequirementsMet, corev1.ConditionFalse, ImageCreationFailedReason),
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.SSHKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQChSAVWhBU4NBaSI67Gvm0oywPk/dzeh+KlT05VLz3OODld8O0Y95+xg02qMOkNOYz8ucq0BwzTx82DLyl5A/WX64t/Kf2WtnOQ8A02xtVl3LcS9Fzmdi6bA168O/sNKfQ1jeVtZyPBwNkKGgp9qhi/JGVzuwLVV+crMjxsSobsEbHij3FWxLqoNNMPHN8FJFiZZaGbltShheFsepiMf9kY04FZjDKyLrI/rueQWuqhLPfJTOGQktKWEYeLYgKiH82/1x3BNYYuuZxUI0KtQ4S50+M6GSQ1yaR7B7/RI+g/CCwGurccOASqcUtqUDzL53p+Y1ffJfn0WubkxrmNmC/jE0YWDepqDsrLXXdo+k3otWkBx1KhUJ5y/jmJZDkDPVFieqh7yRQ2G1J1ByvBRc4h214PHPztFK63xQ9crsQjlzLCR7esGqJ2iIqoGk1BrXbHlAB9FLPhQXDN+IvpWyO1L02ggGZQnLV7ds0dZApexu2g79HcQrCuKu2W9nPTEZ0= eran@fedora"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
	It("update succeeds when hostname is valid and image isn't ready", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
			},
			Status: ImageClusterInstallStatus{
				Conditions: setClusterInstallCondition(hivev1.ClusterInstallRequirementsMet, corev1.ConditionFalse, ImageCreationFailedReason),
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.Hostname = "other-valid-hostname"
		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})
	It("update fail when ssh key is invalid and image isn't ready", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
			},
			Status: ImageClusterInstallStatus{
				Conditions: setClusterInstallCondition(hivev1.ClusterInstallRequirementsMet, corev1.ConditionFalse, ImageCreationFailedReason),
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.SSHKey = "ssh-rsa invalid ssh key"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid ssh key"))
	})
	It("update fail when hostname is invalid and image isn't ready", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
			},
			Status: ImageClusterInstallStatus{
				Conditions: setClusterInstallCondition(hivev1.ClusterInstallRequirementsMet, corev1.ConditionFalse, ImageCreationFailedReason),
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.Hostname = "invalid_hostname&"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid hostname"))
	})
	It("update fail when installation started", func() {
		bareMetalHostRef := &BareMetalHostReference{
			Name:      "test-bmh",
			Namespace: "test-bmh-namespace",
		}

		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname:         "test",
				BareMetalHostRef: bareMetalHostRef,
			},
			Status: ImageClusterInstallStatus{
				BareMetalHostRef: bareMetalHostRef,
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.Hostname = "other-valid-hostname"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("cannot update ImageClusterInstall when the configImage is ready"))
	})

	It("create succeeds when hostname and ssh key are valid", func() {
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
	It("create fail when ssh key is invalid", func() {
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
	It("create fail when hostname is invalid", func() {
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

	It("create success when dual-stack machine networks are valid", func() {
		newClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				MachineNetworks: []MachineNetworkEntry{
					{CIDR: "192.0.2.0/24"},
					{CIDR: "2001:db8::/64"},
				},
			},
		}

		warns, err := newClusterInstall.ValidateCreate()
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("create fail when machine networks have invalid CIDR", func() {
		newClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				MachineNetworks: []MachineNetworkEntry{
					{CIDR: "192.0.2.0/24"},
					{CIDR: "invalid_cidr"},
				},
			},
		}

		warns, err := newClusterInstall.ValidateCreate()
		Expect(warns).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid machine network"))
		Expect(err.Error()).To(ContainSubstring("machine network 1"))
	})

	It("create success when both legacy and new machine network fields are valid", func() {
		newClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				MachineNetwork: "192.0.2.0/24", // Legacy field
				MachineNetworks: []MachineNetworkEntry{ // New field takes precedence
					{CIDR: "192.0.2.0/24"},
					{CIDR: "2001:db8::/64"},
				},
			},
		}

		warns, err := newClusterInstall.ValidateCreate()
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("create fail when proxy is invalid", func() {
		newClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Proxy: &Proxy{
					HTTPProxy:  "http://proxy.com:3128",
					HTTPSProxy: "http://proxy.com:3128",
					NoProxy:    "noproxy.com",
				},
			},
		}

		warns, err := newClusterInstall.ValidateCreate()
		Expect(warns).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid proxy"))
	})

	It("update succeeds BMH ref update while image isn't ready", func() {
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname: "test",
			},
			Status: ImageClusterInstallStatus{
				Conditions: setClusterInstallCondition(hivev1.ClusterInstallRequirementsMet, corev1.ConditionFalse, ImageCreationFailedReason),
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

	It("update fails BMH ref update when installation started", func() {
		bareMetalHostRef := &BareMetalHostReference{
			Name:      "test-bmh",
			Namespace: "test-bmh-namespace",
		}
		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname:         "test",
				BareMetalHostRef: bareMetalHostRef,
			},
			Status: ImageClusterInstallStatus{
				BareMetalHostRef: bareMetalHostRef,
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Spec.BareMetalHostRef = nil

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).ToNot(BeNil())
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

	It("succeeds status update when image is ready", func() {
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
			Status: ImageClusterInstallStatus{
				Conditions: setClusterInstallCondition(hivev1.ClusterInstallRequirementsMet, corev1.ConditionTrue, HostConfigurationSucceededReason),
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Status.Conditions = []hivev1.ClusterInstallCondition{{
			Type:    hivev1.ClusterInstallCompleted,
			Status:  corev1.ConditionTrue,
			Reason:  InstallSucceededReason,
			Message: InstallSucceededMessage,
		}}

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("metadata update succeeds when image is ready", func() {
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
			Status: ImageClusterInstallStatus{
				Conditions: setClusterInstallCondition(hivev1.ClusterInstallRequirementsMet, corev1.ConditionTrue, HostConfigurationSucceededReason),
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.ObjectMeta.Finalizers = []string{"somefinalizer"}

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).To(BeNil())
	})

	It("fail status and spec update when installation started", func() {
		bareMetalHostRef := &BareMetalHostReference{
			Name:      "test-bmh",
			Namespace: "test-bmh-namespace",
		}

		oldClusterInstall := &ImageClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
				Namespace: "test-namespace",
			},
			Spec: ImageClusterInstallSpec{
				Hostname:         "test",
				BareMetalHostRef: bareMetalHostRef,
			},
			Status: ImageClusterInstallStatus{
				BareMetalHostRef: bareMetalHostRef,
			},
		}
		newClusterInstall := oldClusterInstall.DeepCopy()
		newClusterInstall.Status.Conditions = []hivev1.ClusterInstallCondition{{
			Type:    hivev1.ClusterInstallCompleted,
			Status:  corev1.ConditionTrue,
			Reason:  InstallSucceededReason,
			Message: InstallSucceededMessage,
		}}
		newClusterInstall.Spec.Hostname = "stuff"

		warns, err := newClusterInstall.ValidateUpdate(oldClusterInstall)
		Expect(warns).To(BeNil())
		Expect(err).NotTo(BeNil())
	})

	It("allows ClusterMetadata updates when image is ready", func() {
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
			Status: ImageClusterInstallStatus{
				Conditions: setClusterInstallCondition(hivev1.ClusterInstallRequirementsMet, corev1.ConditionTrue, HostConfigurationSucceededReason),
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
