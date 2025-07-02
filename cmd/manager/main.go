package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"dialo.ai/vlanman/pkg/comms"
	errs "dialo.ai/vlanman/pkg/errors"
	"github.com/alecthomas/repr"
	ip "github.com/vishvananda/netlink"
	"k8s.io/client-go/tools/leaderelection"
	rl "k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

func pid(w http.ResponseWriter, _ *http.Request) {
	fmt.Println("Sending pid", os.Getpid())
	resp := comms.PIDResponse{
		PID: os.Getpid(),
	}
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(200)
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		fmt.Println("Couldn't encode resp:")
		repr.Println(resp)
	}
}

func ready(w http.ResponseWriter, _ *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	if vlanWatcher != nil {
		if vlanWatcher.UP.Load() {
			w.WriteHeader(200)
			return
		}
	}
	w.WriteHeader(500)
}

func macvlan(w http.ResponseWriter, r *http.Request) {
	h := slog.NewJSONHandler(os.Stdout, nil)
	log := slog.New(h)
	writeError := func(msg string, err error) {
		log.Error(msg, "error", err)
		mvr := comms.MacvlanResponse{
			Ok:  false,
			Err: err,
		}
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(mvr)
	}

	if r == nil || r.Body == nil {
		writeError("Request / body is nil", errs.ErrNilUnrecoverable)
		return
	}
	out, err := io.ReadAll(r.Body)
	if err != nil {
		writeError("Couldn't ready request body", errs.NewParsingError("request body", err))
		return
	}
	mvr := comms.MacvlanRequest{}
	err = json.Unmarshal(out, &mvr)
	if err != nil {
		writeError("Couldn't unmarshal request body", errs.NewParsingError("request body", err))
		return
	}

	linkName := "vlan" + strconv.FormatInt(int64(vlanID), 10)
	vlan, err := ip.LinkByName(linkName)
	if err != nil {
		writeError(fmt.Sprintf("Couldn't find link '%s' by name", linkName), errs.ErrUnrecoverable)
		return
	}

	attrs := ip.NewLinkAttrs()
	attrs.Name = "macvlan0"
	attrs.ParentIndex = vlan.Attrs().Index
	macvlan := ip.Macvlan{
		LinkAttrs: attrs,
		Mode:      ip.MACVLAN_MODE_BRIDGE,
	}
	mu.Lock()
	link, err := ip.LinkByName(attrs.Name)
	if err == nil {
		err = ip.LinkDel(link)
		if err != nil {
			mu.Unlock()
			writeError("Found existing link but error deleting", &errs.UnrecoverableError{Context: "Cleaning up (deleting link)", Err: err})
			return
		}
	}
	err = ip.LinkAdd(&macvlan)
	if err != nil {
		mu.Unlock()
		writeError(fmt.Sprintf("Couldn't create macvlan interface '%s'", attrs.Name), err)
		return
	}

	err = ip.LinkSetUp(&macvlan)
	if err != nil {
		log.Error("Couldn't set macvlan interface up", "error", err)
		if err = ip.LinkDel(&macvlan); err != nil {
			mu.Unlock()
			writeError(fmt.Sprintf("Couldn't set macvlan interface '%s' up. Cleanup failed.", attrs.Name), err)
			return
		}
		mu.Unlock()
		writeError(fmt.Sprintf("Couldn't set macvlan interface '%s' up. Cleaned up successfully.", attrs.Name), err)
		return
	}

	cmd := exec.Command("bash", "-c", fmt.Sprintf("lsns | grep %d | awk '{print $4}'", mvr.NsID))
	PID, err := cmd.Output()
	fmt.Println(string(PID))
	if err != nil {
		exErr, ok := err.(*exec.ExitError)
		if !ok {
			mu.Unlock()
			writeError("Unknown exit error while getting PID of NsID", err)
			return
		}
		mu.Unlock()
		writeError(fmt.Sprintf("Error checking PID of NsID: %s", string(exErr.Stderr)), exErr)
		return
	}

	cmd = exec.Command("ip", "link", "set", attrs.Name, "netns", strings.TrimSpace(string(PID)))
	out, err = cmd.Output()
	fmt.Println(string(out))
	if err != nil {
		exErr, ok := err.(*exec.ExitError)
		if !ok {
			mu.Unlock()
			writeError("Unknown exit error while setting ns", err)
			return
		}
		mu.Unlock()
		writeError(fmt.Sprintf("Error setting ns: %s", string(exErr.Stderr)), exErr)
		return
	}
	mu.Unlock()

	resp := comms.MacvlanResponse{
		Ok:  true,
		Err: nil,
	}
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(200)
	json.NewEncoder(w).Encode(resp)
}

func addIPAddress(ctx context.Context) {
	link, err := ip.LinkByName("macvlangw")
	if err != nil {
		fmt.Println(errs.UnrecoverableError{
			Context: "Error getting link by name on becoming leader in callback",
			Err:     err,
		})
		os.Exit(1)
	}
	err = ip.AddrAdd(link, &ip.Addr{IPNet: &gatewayIPNet})
	if err != nil {
		fmt.Println(errs.UnrecoverableError{
			Context: "Error adding ip address to vlan on becoming leader in callback",
			Err:     err,
		})
		os.Exit(1)
	}
	setupRoutes(os.Getenv("REMOTE_ROUTES"), os.Getenv("LOCAL_ROUTES"))
}
func removeIPAddress() {
	link, err := ip.LinkByName("macvlangw")
	if err != nil {
		fmt.Println(errs.UnrecoverableError{
			Context: "Error getting link by name on stopped leading in callback",
			Err:     err,
		})
		os.Exit(1)
	}
	err = ip.AddrDel(link, &ip.Addr{IPNet: &gatewayIPNet})
	if err != nil {
		fmt.Println(errs.UnrecoverableError{
			Context: "Error deleting ip address from vlan on stopped leading in callback",
			Err:     err,
		})
		os.Exit(1)
	}
}

func logNewLeader(identity string) {
	numberOfLeaderChanges.Store(numberOfLeaderChanges.Load() + 1)
	fmt.Println("New leader elected: ", identity)
	fmt.Println("Totaln number of changes", numberOfLeaderChanges.Load())
}

var (
	vlanWatcher           *VlanWatcher = nil
	gatewayLink           *ip.Macvlan
	vlanID                int
	mu                    sync.Mutex
	gatewayIPNet          net.IPNet
	remoteRoutes          string
	numberOfLeaderChanges atomic.Int64
	localRoutes           string
)

func setupRoutes(rem, loc string) error {
	var link ip.Link
	if gatewayLink != nil {
		link = gatewayLink
	} else {
		link = vlanWatcher.Link
	}
	remote := strings.SplitSeq(rem, ",")
	for r := range remote {
		_, ipnet, err := net.ParseCIDR(r)
		if err != nil {
			return err
		}
		route := ip.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       ipnet,
		}
		err = ip.RouteAdd(&route)
		if err != nil {
			return err
		}
	}
	if gatewayLink == nil {
		return nil
	}

	for r := range strings.SplitSeq(loc, ",") {
		_, ipnet, err := net.ParseCIDR(r)
		if err != nil {
			return err
		}
		route := ip.Route{
			LinkIndex: (*gatewayLink).Attrs().Index,
			Dst:       ipnet,
		}
		err = ip.RouteAdd(&route)
		if err != nil {
			return err
		}
	}
	return nil
}

func metrics(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(numberOfLeaderChanges.Load())
}

func main() {
	numberOfLeaderChanges.Store(0)
	klog.InitFlags(nil)
	flag.Set("v", "5")
	flag.Parse()
	klog.Info("klogging")
	h := slog.NewJSONHandler(os.Stdout, nil)
	log := slog.New(h)
	log.Info("Starting server")

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-ch
		log.Info("Received signal", "signal", sig)
		os.Exit(1)
	}()

	vid := os.Getenv("VLAN_ID")
	if vid == "" {
		log.Error("VLAN_ID is empty")
		os.Exit(1)
	}
	vlanID64, err := strconv.ParseInt(vid, 10, 64)
	if err != nil {
		log.Error("Couldn't parse vlan id", "id", vid)
		os.Exit(1)
	}
	vlanID = int(vlanID64)

	namespace := os.Getenv("NAMESPACE")
	lockName := os.Getenv("LOCK_NAME")
	localGatewayIP := os.Getenv("LOCAL_GATEWAY_IP")
	isLocalGatewayOn := localGatewayIP != ""

	vlanWatcher = NewWatcher(int(vlanID64))
	go func() {
		err = vlanWatcher.Watch()
		if err != nil {
			log.Error("Error watching for vlan creation", "error", err)
			os.Exit(1)
		}

		if isLocalGatewayOn {
			gatewaySubnet := os.Getenv("LOCAL_GATEWAY_SUBNET")
			gwInt, err := strconv.ParseInt(gatewaySubnet, 10, 64)
			if err != nil {
				gwInt = 32
			}
			gatewayIPNet = net.IPNet{
				IP:   net.ParseIP(localGatewayIP),
				Mask: net.CIDRMask(int(gwInt), 32),
			}
			attrs := ip.NewLinkAttrs()
			attrs.Name = "macvlangw"
			attrs.ParentIndex = vlanWatcher.Link.Attrs().Index
			macvlan := ip.Macvlan{
				LinkAttrs: attrs,
				Mode:      ip.MACVLAN_MODE_BRIDGE,
			}
			err = ip.LinkAdd(&macvlan)
			if err != nil {
				fmt.Println("Error creating macvlan gateway", err)
				os.Exit(1)
			}
			err = ip.LinkSetUp(&macvlan)
			if err != nil {
				fmt.Println("Error setting link up macvlan", err)
				os.Exit(1)
			}
			gatewayLink = &macvlan
			fmt.Println("Created macvlan", (*gatewayLink).Attrs().Name)

			log.Info("Starting leader election")
			cfg := ctrl.GetConfigOrDie()
			hostname, err := os.Hostname()
			if err != nil {
				log.Error("failed to find hostname", "error", err)
				os.Exit(1)
			}
			l, err := rl.NewFromKubeconfig(
				rl.LeasesResourceLock,
				namespace,
				lockName,
				rl.ResourceLockConfig{
					Identity: hostname,
				},
				cfg,
				time.Second*2,
			)
			if err != nil {
				log.Error("Failed to create a resource locker", "error", err)
				os.Exit(1)
			}
			el, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
				Lock:          l,
				LeaseDuration: time.Second * 1,
				RenewDeadline: time.Second / 2,
				RetryPeriod:   time.Second / 10,
				Callbacks: leaderelection.LeaderCallbacks{
					OnStartedLeading: addIPAddress,
					OnStoppedLeading: removeIPAddress,
					OnNewLeader:      logNewLeader,
				},
			})
			if err != nil {
				log.Error("failed to create a leader election")
				os.Exit(1)
			}
			for {
				el.Run(context.Background())
			}
		} else {
			log.Info("Skipping leader election, gateway is off")
			setupRoutes(os.Getenv("REMOTE_ROUTES"), os.Getenv("LOCAL_ROUTES"))
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/pid", pid)
	mux.HandleFunc("/ready", ready)
	mux.HandleFunc("/macvlan", macvlan)
	mux.HandleFunc("/metrics", metrics)
	server := http.Server{
		Handler: mux,
		Addr:    ":61410",
	}
	if err := server.ListenAndServe(); err != nil {
		log.Error("Error listening", "addr", server.Addr, "error", err)
	}
}
