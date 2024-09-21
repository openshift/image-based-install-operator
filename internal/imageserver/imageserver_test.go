package imageserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	"github.com/openshift/image-based-install-operator/controllers"
)

func TestImageServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ImagerServer Suite")
}

var _ = Describe("ServeHttp", func() {
	var (
		server *httptest.Server
		client *http.Client

		tempDir    string
		workDir    string
		configsDir string

		namespace = "image-based-install-operator"
		name      = "config"
	)

	BeforeEach(func() {
		// create system data directories
		var err error
		tempDir, err = os.MkdirTemp("", "imageserver_test")
		Expect(err).NotTo(HaveOccurred())
		workDir = filepath.Join(tempDir, "workdir")
		Expect(os.MkdirAll(workDir, 0700)).To(Succeed())
		configsDir = filepath.Join(tempDir, "configdir")
		Expect(os.MkdirAll(configsDir, 0700)).To(Succeed())

		// create http server
		s := &Handler{
			Log:        logrus.New(),
			WorkDir:    workDir,
			ConfigsDir: configsDir,
		}
		server = httptest.NewServer(s)
		client = server.Client()

		// create test data
		filesDir := filepath.Join(configsDir, namespace, name, controllers.FilesDir, controllers.ClusterConfigDir)
		Expect(os.MkdirAll(filepath.Join(filesDir, "testDir"), 0700)).To(Succeed())

		Expect(os.WriteFile(filepath.Join(filesDir, controllers.IsoName), []byte("content1"), 0600)).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	It("fails for non-matching path format", func() {
		url, err := url.JoinPath(server.URL, "/things/stuff")
		Expect(err).NotTo(HaveOccurred())
		resp, err := client.Get(url)
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("fails for non-existing configs", func() {
		url, err := url.JoinPath(server.URL, "images/namespace/name.iso")
		Expect(err).NotTo(HaveOccurred())
		resp, err := client.Get(url)
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("found image", func() {
		url, err := url.JoinPath(server.URL, fmt.Sprintf("images/%s/%s.iso", namespace, name))
		Expect(err).NotTo(HaveOccurred())
		resp, err := client.Get(url)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})
})
