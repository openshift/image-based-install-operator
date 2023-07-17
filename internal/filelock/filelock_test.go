package filelock

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WithWriteLock", func() {
	var (
		dir string
	)
	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "write_lock_test_data")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
	})

	It("succeeds with no other locks", func() {
		locked, err := WithWriteLock(dir, func() error { return nil })
		Expect(locked).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns the inner function error", func() {
		origError := fmt.Errorf("error")
		locked, err := WithWriteLock(dir, func() error { return origError })
		Expect(locked).To(BeTrue())
		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(origError))
	})

	It("fails when a write lock is held already", func() {
		c := make(chan int)

		l1, err := WithWriteLock(dir, func() error {
			go func() {
				defer func() { c <- 1 }()
				l2, err := WithWriteLock(dir, func() error { return nil })
				Expect(l2).To(BeFalse())
				Expect(err).ToNot(HaveOccurred())
			}()
			<-c
			return nil
		})
		Expect(l1).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails when a read lock is held already", func() {
		c := make(chan int)

		l1, err := WithReadLock(dir, func() error {
			go func() {
				defer func() { c <- 1 }()
				l2, err := WithWriteLock(dir, func() error { return nil })
				Expect(l2).To(BeFalse())
				Expect(err).ToNot(HaveOccurred())
			}()
			<-c
			return nil
		})
		Expect(l1).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails if the directory doesn't exist", func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
		locked, err := WithWriteLock(dir, func() error { return nil })
		Expect(locked).To(BeFalse())
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("WithReadLock", func() {
	var (
		dir string
	)
	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "read_lock_test_data")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
	})

	It("succeeds with no other locks", func() {
		locked, err := WithReadLock(dir, func() error { return nil })
		Expect(locked).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns the inner function error", func() {
		origError := fmt.Errorf("error")
		locked, err := WithReadLock(dir, func() error { return origError })
		Expect(locked).To(BeTrue())
		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(origError))
	})

	It("succeeds when a read lock is held already", func() {
		c := make(chan int)

		l1, err := WithReadLock(dir, func() error {
			go func() {
				defer func() { c <- 1 }()
				l2, err := WithReadLock(dir, func() error { return nil })
				Expect(l2).To(BeTrue())
				Expect(err).ToNot(HaveOccurred())
			}()
			<-c
			return nil
		})
		Expect(l1).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails when a write lock is held already", func() {
		c := make(chan int)

		l1, err := WithWriteLock(dir, func() error {
			go func() {
				defer func() { c <- 1 }()
				l2, err := WithReadLock(dir, func() error { return nil })
				Expect(l2).To(BeFalse())
				Expect(err).ToNot(HaveOccurred())
			}()
			<-c
			return nil
		})
		Expect(l1).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())
	})

	It("fails if the directory doesn't exist", func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
		locked, err := WithReadLock(dir, func() error { return nil })
		Expect(locked).To(BeFalse())
		Expect(err).To(HaveOccurred())
	})
})

func TestFileLock(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Filelock Suite")
}
