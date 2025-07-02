package main

import (
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	ip "github.com/vishvananda/netlink"
)

type VlanWatcher struct {
	ID     int
	Exists atomic.Bool
	UP     atomic.Bool
	Link   ip.Link
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
				link, err := ip.LinkByName(ifaceName)
				if err != nil {
					return err
				}
				err = ip.LinkSetUp(link)
				if err != nil {
					return err
				}
				v.UP.Store(true)
				v.Link = link
				fmt.Println("Vlan found")
				return nil
			}
		}
	}
}
