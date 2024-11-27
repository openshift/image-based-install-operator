package installer

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/image-based-install-operator/api/v1alpha1"
)

var _ = Describe("proxy", func() {
	It("Proxy is nil, nothing to change", func() {
		Expect(proxy(nil)).To(BeNil())
	})

	It("If https and http proxy were not set, nothing to set", func() {
		Expect(proxy(&v1alpha1.Proxy{})).To(BeNil())
	})
})
