package locker

import (
	"math/rand"
	"sync"
	"time"

	errs "dialo.ai/vlanman/pkg/errors"
	"github.com/jrhouston/k8slock"
	"k8s.io/client-go/kubernetes"
)

// For dry run, we don't want side effects
// so creating a lease is off the table
type leaseLocker struct {
	dryRun bool
	locker sync.Locker
}

func NewLeaseLocker(dryRun bool, client kubernetes.Clientset, leaseName, namespace string) (*leaseLocker, error) {
	tries := 0
	var locker sync.Locker
	var err error
	for tries < 3 {
		locker, err = k8slock.NewLocker(leaseName,
			k8slock.TTL(5*time.Second),
			k8slock.Namespace(namespace),
			k8slock.Clientset(&client),
		)
		if err == nil {
			break
		}

		n := ((rand.Int() % 10) + 1) * 100
		time.Sleep(time.Duration(n) * time.Millisecond)
		tries += 1
	}
	if locker == nil || err != nil {
		return nil, errs.NewClientRequestError("creating a lease locker", err)
	}

	return &leaseLocker{
		dryRun,
		locker,
	}, nil
}

func (ll *leaseLocker) Lock() {
	if !ll.dryRun {
		ll.locker.Lock()
	}
}
func (ll *leaseLocker) Unlock() {
	if !ll.dryRun {
		ll.locker.Unlock()
	}
}
