package imageserver

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/sirupsen/logrus"

	"github.com/openshift/image-based-install-operator/controllers"
)

type Handler struct {
	Log        logrus.FieldLogger
	WorkDir    string
	ConfigsDir string
}

var pathRegexp = regexp.MustCompile(`^/images/(.+)/(.+)\.iso$`)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	match := pathRegexp.FindStringSubmatch(r.URL.Path)
	if match == nil || len(match) != 3 {
		h.Log.Errorf("failed to parse image path '%s'\n", r.URL.Path)
		http.NotFound(w, r)
		return
	}

	namespace := match[1]
	name := match[2]
	h.Log.Infof("Serving image for ImageClusterInstall %s/%s", namespace, name)
	outPath := filepath.Join(h.ConfigsDir, namespace, name, controllers.FilesDir, controllers.ClusterConfigDir, controllers.IsoName)
	if _, err := os.Stat(outPath); err != nil {
		h.Log.WithError(err).Error("failed to find iso file")
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, outPath)
}
