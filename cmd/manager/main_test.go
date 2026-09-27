package main

import (
	"testing"

	"github.com/openshift/image-based-install-operator/controllers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Main Suite")
}

var _ = Describe("serviceURL", func() {
	It("creates the correct url without a port", func() {
		opts := &controllers.ImageClusterInstallReconcilerOptions{
			ServiceName:      "name",
			ServiceNamespace: "namespace",
			ServiceScheme:    "https",
		}
		url, err := serviceURL(opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(Equal("https://name.namespace.svc"))
	})

	It("creates the correct url with a port", func() {
		opts := &controllers.ImageClusterInstallReconcilerOptions{
			ServiceName:      "name",
			ServiceNamespace: "namespace",
			ServiceScheme:    "http",
			ServicePort:      "8080",
		}
		url, err := serviceURL(opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(Equal("http://name.namespace.svc:8080"))
	})
})
