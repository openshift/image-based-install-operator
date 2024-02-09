package main

import (
	"context"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift/image-based-install-operator/controllers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Main Suite")
}

var _ = Describe("routeURL", func() {
	var (
		c     client.Client
		ctx   = context.Background()
		route *routev1.Route
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().
			WithScheme(scheme).
			Build()
		route = &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "name",
				Namespace: "namespace",
			},
			Spec: routev1.RouteSpec{},
		}
	})

	It("fails when the route doesn't exist", func() {
		opts := &controllers.ImageClusterInstallReconcilerOptions{
			RouteName:      "name",
			RouteNamespace: "namespace",
			RouteScheme:    "https",
		}
		_, err := routeURL(opts, c)
		Expect(err).To(HaveOccurred())
	})

	It("fails when the route doesn't have Spec.Host set", func() {
		Expect(c.Create(ctx, route)).To(Succeed())
		opts := &controllers.ImageClusterInstallReconcilerOptions{
			RouteName:      "name",
			RouteNamespace: "namespace",
			RouteScheme:    "https",
		}
		_, err := routeURL(opts, c)
		Expect(err).To(HaveOccurred())
	})

	Context("when the route exists and has a host set", func() {
		BeforeEach(func() {
			route.Spec.Host = "name-namespace.cluster.example.com"
			Expect(c.Create(ctx, route)).To(Succeed())
		})

		It("creates the correct url without a port", func() {
			opts := &controllers.ImageClusterInstallReconcilerOptions{
				RouteName:      "name",
				RouteNamespace: "namespace",
				RouteScheme:    "https",
			}
			url, err := routeURL(opts, c)
			Expect(err).NotTo(HaveOccurred())
			Expect(url).To(Equal("https://name-namespace.cluster.example.com"))
		})

		It("creates the correct url with a port", func() {
			opts := &controllers.ImageClusterInstallReconcilerOptions{
				RouteName:      "name",
				RouteNamespace: "namespace",
				RouteScheme:    "http",
				RoutePort:      "8080",
			}
			url, err := routeURL(opts, c)
			Expect(err).NotTo(HaveOccurred())
			Expect(url).To(Equal("http://name-namespace.cluster.example.com:8080"))
		})
	})
})
