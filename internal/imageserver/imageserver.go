package imageserver

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	"github.com/openshift/cluster-relocation-service/internal/filelock"
	"github.com/sirupsen/logrus"
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
	configDir := filepath.Join(h.ConfigsDir, namespace, name)
	filesDir := filepath.Join(configDir, "files")
	if _, err := os.Stat(configDir); err != nil {
		h.Log.WithError(err).Error("failed to stat config dir")
		http.NotFound(w, r)
		return
	}
	h.Log.Infof("Serving image for ClusterConfig %s/%s", namespace, name)

	isoWorkDir, err := os.MkdirTemp(h.WorkDir, "build")
	if err != nil {
		h.Log.WithError(err).Error("failed to create iso work dir")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// if anything fails remove the workdir, if create succeeds it will remove the workdir so this will be a noop
	defer os.RemoveAll(isoWorkDir)

	// TODO: improve this to wait for some timout (use ctx?) instead of erroring on a lock failure immediately
	locked, lockErr, funcErr := filelock.WithReadLock(configDir, func() error {
		return copyDir(isoWorkDir, filesDir)
	})
	if lockErr != nil {
		h.Log.WithError(lockErr).Error("failed to acquire file lock")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if funcErr != nil {
		h.Log.WithError(funcErr).Error("failed to acquire file lock")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !locked {
		h.Log.Error("failed to acquire file lock")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	outPath, err := tempFileName(h.WorkDir)
	if err != nil {
		h.Log.WithError(err).Error("failed to create iso output file")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := create(outPath, isoWorkDir, "relocation-config"); err != nil {
		h.Log.WithError(err).Error("failed to create iso")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer os.Remove(outPath)

	http.ServeFile(w, r, outPath)
}

func copyDir(dst, src string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, strings.TrimPrefix(path, src))

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		dest, err := os.OpenFile(targetPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dest.Close()

		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()

		_, err = io.Copy(dest, src)
		return err
	})
}

func tempFileName(dir string) (string, error) {
	f, err := os.CreateTemp(dir, "tempiso")
	if err != nil {
		return "", err
	}
	path := f.Name()

	if err := os.Remove(path); err != nil {
		return "", err
	}

	return path, nil
}

// create builds an iso file at outPath with the given volumeLabel using the contents of the working directory
func create(outPath string, workDir string, volumeLabel string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0700); err != nil {
		return err
	}
	if err := os.RemoveAll(outPath); err != nil {
		return err
	}

	// Use the minimum iso size that will satisfy diskfs validations here.
	// This value doesn't determine the final image size, but is used
	// to truncate the initial file. This value would be relevant if
	// we were writing to a particular partition on a device, but we are
	// not so the minimum iso size will work for us here
	minISOSize := 38 * 1024
	d, err := diskfs.Create(outPath, int64(minISOSize), diskfs.Raw, diskfs.SectorSizeDefault)
	if err != nil {
		return err
	}

	d.LogicalBlocksize = 2048
	fspec := disk.FilesystemSpec{
		Partition:   0,
		FSType:      filesystem.TypeISO9660,
		VolumeLabel: volumeLabel,
		WorkDir:     workDir,
	}
	fs, err := d.CreateFilesystem(fspec)
	if err != nil {
		return err
	}

	iso, ok := fs.(*iso9660.FileSystem)
	if !ok {
		return fmt.Errorf("not an iso9660 filesystem")
	}

	options := iso9660.FinalizeOptions{
		RockRidge:        true,
		VolumeIdentifier: volumeLabel,
	}

	return iso.Finalize(options)
}
