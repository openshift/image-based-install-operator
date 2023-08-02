package filelock

import (
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

const lockFileName = "lock"

func lockForDir(dir string) (*flock.Flock, error) {
	p := filepath.Join(dir, lockFileName)
	_, err := os.Stat(p)
	if os.IsNotExist(err) {
		if err := os.WriteFile(p, []byte{}, 0600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return flock.New(p), nil
}

// WithWriteLock runs the given function while holding a write lock on the directory `dir`
// It returns a bool indicating whether the lock was acquired, any error that occurred acquiring
// the lock, and the error value returned by the wrapped function
func WithWriteLock(dir string, f func() error) (bool, error, error) {
	lock, err := lockForDir(dir)
	if err != nil {
		return false, err, nil
	}
	locked, err := lock.TryLock()
	if err != nil {
		return false, err, nil
	}
	if !locked {
		return false, nil, nil
	}
	defer lock.Unlock()

	return true, nil, f()
}

// WithReadLock runs the given function while holding a read lock on the directory `dir`
// It returns a bool indicating whether the lock was acquired, any error that occurred acquiring
// the lock, and the error value returned by the wrapped function
func WithReadLock(dir string, f func() error) (bool, error, error) {
	lock, err := lockForDir(dir)
	if err != nil {
		return false, err, nil
	}
	locked, err := lock.TryRLock()
	if err != nil {
		return false, err, nil
	}
	if !locked {
		return false, nil, nil
	}
	defer lock.Unlock()

	return true, nil, f()
}
