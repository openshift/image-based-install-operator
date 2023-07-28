package imageserver

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/diskfs/go-diskfs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
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

		namespace = "cluster-relocation"
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
		filesDir := filepath.Join(configsDir, namespace, name, "files")
		Expect(os.MkdirAll(filepath.Join(filesDir, "testDir"), 0700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(filesDir, "file1"), []byte("content1"), 0600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(filesDir, "testDir", "file2"), []byte("content2"), 0600)).To(Succeed())
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

	It("contains the correct content for existing configs", func() {
		url, err := url.JoinPath(server.URL, fmt.Sprintf("images/%s/%s.iso", namespace, name))
		Expect(err).NotTo(HaveOccurred())
		resp, err := client.Get(url)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		f, err := os.CreateTemp("", "imageserver_test_iso")
		isoPath := f.Name()
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(isoPath)
		_, err = io.Copy(f, resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Close()).To(Succeed())

		d, err := diskfs.Open(isoPath, diskfs.WithOpenMode(diskfs.ReadOnly))
		Expect(err).NotTo(HaveOccurred())
		fs, err := d.GetFilesystem(0)
		Expect(err).NotTo(HaveOccurred())

		Expect(fs.Label()).To(HavePrefix("ZTC SNO"))

		isoFile, err := fs.OpenFile("/file1", os.O_RDONLY)
		Expect(err).NotTo(HaveOccurred())
		content, err := io.ReadAll(isoFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(Equal([]byte("content1")))

		isoFile, err = fs.OpenFile("/testDir/file2", os.O_RDONLY)
		Expect(err).NotTo(HaveOccurred())
		content, err = io.ReadAll(isoFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(Equal([]byte("content2")))
	})
})
