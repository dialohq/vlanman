package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"dialo.ai/vlanman/pkg/comms"
	errs "dialo.ai/vlanman/pkg/errors"
	ip "github.com/vishvananda/netlink"
)

func fatal(err error) {
	fmt.Println(err)
	os.Exit(1)
}

func main() {
	networkName := os.Getenv("VLAN_NETWORK")
	url := fmt.Sprintf("http://%s-service.vlanman-system:61410/macvlan", networkName)

	cmd := exec.Command("bash", "-c", "readlink /proc/1/ns/net | grep -o '[0-9]\\+'")
	nsidStr, err := cmd.Output()
	if err != nil {
		exErr, ok := err.(*exec.ExitError)
		if !ok {
			fatal(&errs.UnrecoverableError{
				Context: "Couldn't extract nsid, couldn't determine type of error",
				Err:     err,
			})
		}
		fatal(&errs.UnrecoverableError{
			Context: "Couldn't extract nsid",
			Err:     exErr,
		})

	}
	nsid, err := strconv.ParseInt(strings.TrimSpace(string(nsidStr)), 10, 64)
	if err != nil {
		fatal(errs.NewParsingError("nsid", err))
	}
	data := comms.MacvlanRequest{
		NsID: nsid,
	}
	payload, err := json.Marshal(data)
	if err != nil {
		fatal(&errs.UnrecoverableError{
			Context: "Couldn't marshal pid payload when requesting macvlan interface",
			Err:     errs.NewParsingError("marshaling macvlan request", err),
		})
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		fatal(&errs.RequestError{
			Action: fmt.Sprintf("Request macvlan from manager (%s), with PID %d", networkName, os.Getpid()),
			Err:    err,
		})
	}

	if resp == nil {
		fatal(&errs.RequestError{
			Action: "Check if response is nil on macvlan interface request",
			Err:    errs.ErrNilUnrecoverable,
		})
	}

	if resp.StatusCode != 200 {
		fatal(&errs.UnrecoverableError{
			Context: fmt.Sprintf("Status code of response to macvlan request is not 200, is %d. Check logs of manager pod on the same node for more information.", resp.StatusCode),
			Err:     errs.ErrUnrecoverable,
		})
	}
	link, err := ip.LinkByName("macvlan0")
	if err != nil {
		fatal(&errs.UnrecoverableError{
			Context: fmt.Sprintf("Couldn't get link by name '%s'", "macvlan0"),
			Err:     err,
		})
	}
	err = ip.LinkSetUp(link)
	if err != nil {
		fatal(&errs.UnrecoverableError{
			Context: fmt.Sprintf("Couldn't set link '%s' up", "macvlan0"),
			Err:     err,
		})
	}

	sn := os.Getenv("MACVLAN_SUBNET")
	snInt, err := strconv.ParseInt(sn, 10, 64)
	if err != nil {
		snInt = 32
	}
	ipnet := net.IPNet{
		IP:   net.ParseIP(os.Getenv("MACVLAN_IP")),
		Mask: net.CIDRMask(int(snInt), 32),
	}
	addr := ip.Addr{IPNet: &ipnet}
	err = ip.AddrAdd(link, &addr)
	if err != nil {
		fatal(&errs.UnrecoverableError{
			Context: "Failed to add IP address to macvlan",
			Err:     err,
		})
	}
	gwIP := os.Getenv("GATEWAY_IP")
	gwSN := os.Getenv("GATEWAY_SUBNET")
	isRemoteGW := false
	var gwIPNet *net.IPNet
	if gwIP != "" {
		isRemoteGW = true
		if gwSN == "" {
			gwSN = "32"
		}
		_, gwIPNet, err = net.ParseCIDR(gwIP + "/" + gwSN)
		if err != nil {
			fatal(&errs.ParsingError{
				Source: "Parsing CIRD of remote gateway",
				Err:    err,
			})
		}
		gwRoute := ip.Route{
			LinkIndex: link.Attrs().Index,
			Scope:     ip.SCOPE_LINK,
			Dst:       gwIPNet,
		}
		err = ip.RouteAdd(&gwRoute)
		if err != nil {
			fatal(&errs.UnrecoverableError{
				Context: "Failed to add route to remote gateway",
				Err:     err,
			})
		}
	}

	routes := strings.SplitSeq(os.Getenv("REMOTE_ROUTES"), ",")
	for r := range routes {
		_, ipnet, err := net.ParseCIDR(r)
		if err != nil {
			fatal(&errs.UnrecoverableError{
				Context: fmt.Sprintf("Failed to add route to remote '%s'", r),
				Err:     err,
			})
		}
		route := ip.Route{}
		if isRemoteGW {
			route = ip.Route{
				LinkIndex: link.Attrs().Index,
				Gw:        gwIPNet.IP,
				Dst:       ipnet,
			}
		} else {
			route = ip.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       ipnet,
			}
		}
		err = ip.RouteAdd(&route)
		if err != nil {
			fmt.Println(&errs.UnrecoverableError{
				Context: fmt.Sprintf("Failed to add route to remote '%s'", r),
				Err:     err,
			})
			for {
				time.Sleep(time.Second)
			}
			fatal(&errs.UnrecoverableError{
				Context: fmt.Sprintf("Failed to add route to remote '%s'", r),
				Err:     err,
			})
		}
	}
	fmt.Println("All done")
}
