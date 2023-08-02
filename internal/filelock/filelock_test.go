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
		locked, lerr, ferr := WithWriteLock(dir, func() error { return nil })
		Expect(locked).To(BeTrue())
		Expect(lerr).ToNot(HaveOccurred())
		Expect(ferr).ToNot(HaveOccurred())
	})

	It("returns the inner function error", func() {
		origError := fmt.Errorf("error")
		locked, lerr, ferr := WithWriteLock(dir, func() error { return origError })
		Expect(locked).To(BeTrue())
		Expect(lerr).ToNot(HaveOccurred())
		Expect(ferr).To(HaveOccurred())
		Expect(ferr).To(Equal(origError))
	})

	It("fails when a write lock is held already", func() {
		c := make(chan int)

		l1, lerr, ferr := WithWriteLock(dir, func() error {
			go func() {
				defer func() { c <- 1 }()
				l2, lerr2, ferr2 := WithWriteLock(dir, func() error { return nil })
				Expect(l2).To(BeFalse())
				Expect(lerr2).ToNot(HaveOccurred())
				Expect(ferr2).ToNot(HaveOccurred())
			}()
			<-c
			return nil
		})
		Expect(l1).To(BeTrue())
		Expect(lerr).NotTo(HaveOccurred())
		Expect(ferr).NotTo(HaveOccurred())
	})

	It("fails when a read lock is held already", func() {
		c := make(chan int)

		l1, lerr, ferr := WithReadLock(dir, func() error {
			go func() {
				defer func() { c <- 1 }()
				l2, lerr2, ferr2 := WithWriteLock(dir, func() error { return nil })
				Expect(l2).To(BeFalse())
				Expect(lerr2).ToNot(HaveOccurred())
				Expect(ferr2).ToNot(HaveOccurred())
			}()
			<-c
			return nil
		})
		Expect(l1).To(BeTrue())
		Expect(lerr).NotTo(HaveOccurred())
		Expect(ferr).NotTo(HaveOccurred())
	})

	It("fails if the directory doesn't exist", func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
		locked, lerr, _ := WithWriteLock(dir, func() error { return nil })
		Expect(locked).To(BeFalse())
		Expect(lerr).To(HaveOccurred())
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
		locked, lerr, ferr := WithReadLock(dir, func() error { return nil })
		Expect(locked).To(BeTrue())
		Expect(lerr).ToNot(HaveOccurred())
		Expect(ferr).ToNot(HaveOccurred())
	})

	It("returns the inner function error", func() {
		origError := fmt.Errorf("error")
		locked, lerr, ferr := WithReadLock(dir, func() error { return origError })
		Expect(locked).To(BeTrue())
		Expect(lerr).ToNot(HaveOccurred())
		Expect(ferr).To(HaveOccurred())
		Expect(ferr).To(Equal(origError))
	})

	It("succeeds when a read lock is held already", func() {
		c := make(chan int)

		l1, lerr, ferr := WithReadLock(dir, func() error {
			go func() {
				defer func() { c <- 1 }()
				l2, lerr2, ferr2 := WithReadLock(dir, func() error { return nil })
				Expect(l2).To(BeTrue())
				Expect(lerr2).ToNot(HaveOccurred())
				Expect(ferr2).ToNot(HaveOccurred())
			}()
			<-c
			return nil
		})
		Expect(l1).To(BeTrue())
		Expect(lerr).NotTo(HaveOccurred())
		Expect(ferr).NotTo(HaveOccurred())
	})

	It("fails when a write lock is held already", func() {
		c := make(chan int)

		l1, lerr, ferr := WithWriteLock(dir, func() error {
			go func() {
				defer func() { c <- 1 }()
				l2, lerr2, ferr2 := WithReadLock(dir, func() error { return nil })
				Expect(l2).To(BeFalse())
				Expect(lerr2).ToNot(HaveOccurred())
				Expect(ferr2).ToNot(HaveOccurred())
			}()
			<-c
			return nil
		})
		Expect(l1).To(BeTrue())
		Expect(lerr).NotTo(HaveOccurred())
		Expect(ferr).NotTo(HaveOccurred())
	})

	It("fails if the directory doesn't exist", func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
		locked, lerr, _ := WithReadLock(dir, func() error { return nil })
		Expect(locked).To(BeFalse())
		Expect(lerr).To(HaveOccurred())
	})
})

func TestFileLock(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Filelock Suite")
}
