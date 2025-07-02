package main

import (
	"log/slog"
	"os"
	"strconv"

	u "dialo.ai/vlanman/pkg/utils"
	ip "github.com/vishvananda/netlink"
)

func main() {
	h := slog.NewJSONHandler(os.Stdout, nil)
	log := slog.New(h)

	envID := os.Getenv("ID")
	ID, err := strconv.ParseInt(envID, 10, 64)
	if err != nil {
		log.Error("Couldn't parse ID to int", "ID", envID, "error", err)
		os.Exit(1)
	}

	envPID := os.Getenv("PID")
	PID, err := strconv.ParseInt(envPID, 10, 64)
	if err != nil {
		log.Error("Couldn't parse PID to int", "PID", envPID, "error", err)
		os.Exit(1)
	}

	dflt, err := u.FindDefaultInterface()
	if err != nil {
		log.Error("Couldn't find default interface", "error", err)
		os.Exit(1)
	}

	attrs := ip.NewLinkAttrs()
	attrs.Name = "vlan" + envID
	attrs.ParentIndex = (*dflt).Attrs().Index
	vlan := ip.Vlan{
		LinkAttrs: attrs,
		VlanId:    int(ID),
	}

	link, err := ip.LinkByName(attrs.Name)
	if err == nil {
		err = ip.LinkDel(link)
		if err != nil {
			log.Error("Found existing link but error deleting", "msg", err.Error())
			os.Exit(1)
		}

	}
	err = ip.LinkAdd(&vlan)
	if err != nil {
		log.Error("Couldn't create vlan interface", "name", attrs.Name, "error", err)
		os.Exit(1)
	}

	err = ip.LinkSetUp(&vlan)
	if err != nil {
		log.Error("Couldn't set vlan interface up", "error", err)
		if err = ip.LinkDel(&vlan); err != nil {
			log.Error("Cleanup failed, couldn't delete vlan", "error", err)
		}
		os.Exit(1)
	}

	err = ip.LinkSetNsPid(&vlan, int(PID))
	if err != nil {
		log.Error("Couldn't move link to netns", "pid", PID, "link", vlan.Attrs().Name, "error", err)
		if err = ip.LinkDel(&vlan); err != nil {
			log.Error("Cleanup failed, couldn't delete vlan", "error", err)
		}
		os.Exit(1)
	}

	log.Info("Operation successful")
	os.Exit(0)
}
