package lock

import (
	"time"

	"6.5840/kvsrv1/rpc"
	"6.5840/kvtest1"
)

type Lock struct {
	// IKVClerk is a go interface for k/v clerks: the interface hides
	// the specific Clerk type of ck but promises that ck supports
	// Put and Get.  The tester passes the clerk in when calling
	// MakeLock().
	ck   kvtest.IKVClerk
	name string
	id   string
}

// The tester calls MakeLock() and passes in a k/v clerk; your code can
// perform a Put or Get by calling lk.ck.Put() or lk.ck.Get().
//
// This interface supports multiple locks by means of the
// lockname argument; locks with different names should be
// independent.
func MakeLock(ck kvtest.IKVClerk, lockname string) *Lock {
	lk := &Lock{ck: ck, name: lockname, id: kvtest.RandValue(8)}
	return lk
}

func (lk *Lock) Acquire() {
	for {
		val, ver, err := lk.ck.Get(lk.name)
		if err == rpc.OK && val == lk.id {
			return
		}
		if err == rpc.ErrNoKey {
			err = lk.ck.Put(lk.name, lk.id, 0)
		} else if val == "" {
			err = lk.ck.Put(lk.name, lk.id, ver)
		} else {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err == rpc.OK {
			return
		}
		// ErrMaybe: next Get will show if we own it.
	}
}

func (lk *Lock) Release() {
	for {
		val, ver, err := lk.ck.Get(lk.name)
		if err != rpc.OK || val != lk.id {
			return
		}
		err = lk.ck.Put(lk.name, "", ver)
		if err == rpc.OK {
			return
		}
		// ErrMaybe: next Get will show if we still own it.
	}
}
