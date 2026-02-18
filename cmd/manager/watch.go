package main

import (
	"log/slog"
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

func (v *VlanWatcher) Watch(downgrade func(), logger slog.Logger) error {
	ifaceName := "vlan" + strconv.FormatInt(int64(v.ID), 10)
	for {
		time.Sleep(time.Second / 2)
		ents, err := os.ReadDir("/sys/class/net")
		if err != nil {
			logger.Info("Reading dir failed", "err", err)
			return err
		}
		exists := false
		for _, ent := range ents {
			if ent.Name() == ifaceName {
				exists = true
				v.Exists.Store(true)
				link, err := ip.LinkByName(ifaceName)
				if err != nil {
					logger.Info("couldn't get link by name", "err", err)
					break
				}
				err = ip.LinkSetUp(link)
				if err != nil {
					logger.Info("Couldnt set link up", "err", err)
					break
				}
				v.UP.Store(true)
				v.Link = link
			}
		}
		if !exists {
			logger.Info("Interface doesn't exist, updating and downgrading")
			downgrade()
			v.Exists.Store(false)
			v.UP.Store(false)
		}
	}
}
