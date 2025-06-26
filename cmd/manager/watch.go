package main

import (
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/vishvananda/netlink"
)

type VlanWatcher struct {
	ID     int
	Exists atomic.Bool
	UP     atomic.Bool
}

// Can't use something like fsnotify here
// because kernel doesn't send notifications
// for virtual filesystems.
func NewWatcher(id int) *VlanWatcher {
	return &VlanWatcher{
		ID: id,
	}
}

func (v *VlanWatcher) Watch() error {
	ifaceName := "vlan" + strconv.FormatInt(int64(v.ID), 10)
	for {
		time.Sleep(time.Second / 2)
		ents, err := os.ReadDir("/sys/class/net")
		if err != nil {
			return err
		}
		for _, ent := range ents {
			if ent.Name() == ifaceName {
				v.Exists.Store(true)
				link, err := netlink.LinkByName(ifaceName)
				if err != nil {
					return err
				}
				err = netlink.LinkSetUp(link)
				if err != nil {
					return err
				}
				v.UP.Store(true)
				return nil
			}
		}
	}
}
