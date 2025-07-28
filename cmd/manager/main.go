package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"

	"syscall"
	"time"

	"dialo.ai/vlanman/pkg/comms"
	errs "dialo.ai/vlanman/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/procfs"
	ip "github.com/vishvananda/netlink"
	"k8s.io/client-go/tools/leaderelection"
	rl "k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

var logger slog.Logger

func isFileExistsErr(e error) bool {
	return strings.Contains(e.Error(), "file exists")
}

func pid(w http.ResponseWriter, r *http.Request) {
	logger.Info("Sending pid", "pid", os.Getpid())
	resp := comms.PIDResponse{
		PID: os.Getpid(),
	}
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(200)
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		logger.Error("Failed to encode PID response", "msg", err)
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
	writeError := func(msg string, err error) {
		logger.Error(msg, "error", err)
		mvr := comms.MacvlanResponse{}
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
	attrs.Name = "macvlan" + strconv.FormatInt(int64(vlanID), 10)
	attrs.ParentIndex = vlan.Attrs().Index
	macvlan := ip.Macvlan{
		LinkAttrs: attrs,
		Mode:      ip.MACVLAN_MODE_BRIDGE,
	}
	link, err := ip.LinkByName(attrs.Name)
	if err == nil {
		err = ip.LinkDel(link)
		if err != nil {
			writeError("Found existing link but error deleting", &errs.UnrecoverableError{Context: "Cleaning up (deleting link)", Err: err})
			return
		}
	}
	err = ip.LinkAdd(&macvlan)
	if err != nil {
		writeError(fmt.Sprintf("Couldn't create macvlan interface '%s'", attrs.Name), err)
		return
	}

	err = ip.LinkSetUp(&macvlan)
	if err != nil {
		logger.Error("Couldn't set macvlan interface up", "msg", err)
		if err = ip.LinkDel(&macvlan); err != nil {
			writeError(fmt.Sprintf("Couldn't set macvlan interface '%s' up. Cleanup failed.", attrs.Name), err)
			return
		}
		writeError(fmt.Sprintf("Couldn't set macvlan interface '%s' up. Cleaned up successfully.", attrs.Name), err)
		return
	}

	cmd := exec.Command("bash", "-c", fmt.Sprintf("lsns | grep %d | awk '{print $4}'", mvr.NsID))
	PID, err := cmd.Output()
	if err != nil {
		exErr, ok := err.(*exec.ExitError)
		if !ok {
			writeError("Unknown exit error while getting PID of NsID", err)
			return
		}
		writeError(fmt.Sprintf("Error checking PID of NsID: %s", string(exErr.Stderr)), exErr)
		return
	}

	cmd = exec.Command("ip", "link", "set", attrs.Name, "netns", strings.TrimSpace(string(PID)))
	out, err = cmd.Output()
	if err != nil {
		exErr, ok := err.(*exec.ExitError)
		if !ok {
			writeError("Unknown exit error while setting ns", err)
			return
		}
		if !strings.Contains(string(exErr.Stderr), "File exists") {
			writeError(fmt.Sprintf("Error setting ns: %s", string(exErr.Stderr)), exErr)
			return
		}
	}
	resp := comms.MacvlanResponse{
		Id: envs.vlanID,
	}
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(200)

	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		logger.Error("Error encoding response to macvlan request", "err", &errs.RequestError{
			Action: "Encoding json response in macvlan",
			Err:    err,
		})
	}
}

func addIPAddress(ctx context.Context) {
	link, err := ip.LinkByName("macvlangw" + strconv.FormatInt(int64(envs.vlanID), 10))
	if err != nil {
		logger.Error("Failed to get macvlangw link when becoming leader", "msg", &errs.UnrecoverableError{
			Context: "Error getting link by name on becoming leader in callback",
			Err:     err,
		})
		os.Exit(1)
	}
	err = ip.AddrAdd(link, &ip.Addr{IPNet: &gatewayIPNet})
	if err != nil && !isFileExistsErr(err) {
		logger.Error("Failed to add IP address to VLAN when becoming leader", "msg", &errs.UnrecoverableError{
			Context: "Error adding ip address to vlan on becoming leader in callback",
			Err:     err,
		})
		os.Exit(1)
	}
	setupRoutes(os.Getenv("REMOTE_ROUTES"), os.Getenv("LOCAL_ROUTES"))
}
func removeIPAddress() {
	link, err := ip.LinkByName("macvlangw" + strconv.FormatInt(int64(envs.vlanID), 10))
	if err != nil {
		logger.Error("Failed to get macvlangw link when stopped leading", "msg", &errs.UnrecoverableError{
			Context: "Error getting link by name on stopped leading in callback",
			Err:     err,
		})
		os.Exit(1)
	}
	err = ip.AddrDel(link, &ip.Addr{IPNet: &gatewayIPNet})
	if err != nil {
		logger.Error("msg", "Failed to delete IP address from VLAN when stopped leading", &errs.UnrecoverableError{
			Context: "Error deleting ip address from vlan on stopped leading in callback",
			Err:     err,
		})
		os.Exit(1)
	}
}

func logNewLeader(identity string) {
	leaderChanges.Add(1)
	lastLeaderChange.Store(time.Now())
}

var (
	vlanWatcher      *VlanWatcher = nil
	gatewayLink      *ip.Macvlan
	vlanID           int
	gatewayIPNet     net.IPNet
	remoteRoutes     string
	leaderChanges    atomic.Int64
	lastLeaderChange atomic.Value
	localRoutes      string
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

func begin() context.Context {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	//TODO add kv's to ctx
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := <-ch
		logger.Info("Received termination signal", "signal", sig)
		cancel()
		os.Exit(1)
	}()
	return ctx
}

type Envs struct {
	ownerNetName     string
	namespace        string
	vlanID           int
	lockName         string
	localGatewayIP   string
	isLocalGatewayOn bool
}

func getEnvs() Envs {
	klog.InitFlags(nil)
	flag.Set("v", "5")
	flag.Parse()

	ownerNetName := os.Getenv("OWNER_NETWORK")
	if ownerNetName == "" {
		logger.Error("Owner network name env var is not set", "msg", errs.ErrNilUnrecoverable)
		os.Exit(1)
	}

	vid := os.Getenv("VLAN_ID")
	if vid == "" {
		logger.Error("Error getting vlan", "msg", &errs.UnrecoverableError{Context: "VLAN_ID env variable is empty", Err: errs.ErrNilUnrecoverable})
		os.Exit(1)
	}
	vlanID64, err := strconv.ParseInt(vid, 10, 64)
	if err != nil {
		logger.Error("Error parsing vlan", "msg", errs.NewParsingError("VLAN_ID env variable couldn't be parsed to int", err))
		os.Exit(1)
	}
	if vlanID64 > math.MaxInt || vlanID64 < math.MinInt {
		msg := fmt.Sprintf("VLAN_ID is outside of int range [%d, %d]", math.MinInt, math.MaxInt)
		logger.Error("Error parsing vlan", "msg", errs.NewParsingError(msg, err))
		os.Exit(1)
	}
	vlanID = int(vlanID64)

	namespace := os.Getenv("NAMESPACE")
	lockName := os.Getenv("LOCK_NAME")
	localGatewayIP := os.Getenv("LOCAL_GATEWAY_IP")
	isLocalGatewayOn := localGatewayIP != ""

	return Envs{
		ownerNetName:     ownerNetName,
		namespace:        namespace,
		lockName:         lockName,
		localGatewayIP:   localGatewayIP,
		isLocalGatewayOn: isLocalGatewayOn,
		vlanID:           vlanID,
	}
}

func getValues() (int64, *time.Time) {
	cnt := leaderChanges.Load()

	val := lastLeaderChange.Load()
	change, ok := val.(time.Time)
	if !ok {
		return cnt, nil
		// panic("Last leader change metric is not of type time.Time")
	}
	return cnt, &change
}

func interfaceSetup(ctx context.Context, e Envs) {
	err := vlanWatcher.Watch()
	if err != nil {
		logger.Error("Error creating vlan watcher", "msg", &errs.UnrecoverableError{Context: "Couldn't create a vlan watcher", Err: err})
		os.Exit(1)
	}

	if !e.isLocalGatewayOn {
		logger.Info("Skipping leader election, gateway is off")
		setupRoutes(os.Getenv("REMOTE_ROUTES"), os.Getenv("LOCAL_ROUTES"))
		return
	}

	gatewaySubnet := os.Getenv("LOCAL_GATEWAY_SUBNET")
	gwInt, err := strconv.ParseInt(gatewaySubnet, 10, 64)
	if err != nil {
		gwInt = 32
	}
	gatewayIPNet = net.IPNet{
		IP:   net.ParseIP(e.localGatewayIP),
		Mask: net.CIDRMask(int(gwInt), 32),
	}
	attrs := ip.NewLinkAttrs()
	attrs.Name = "macvlangw" + strconv.FormatInt(int64(e.vlanID), 10)
	attrs.ParentIndex = vlanWatcher.Link.Attrs().Index
	macvlan := ip.Macvlan{
		LinkAttrs: attrs,
		Mode:      ip.MACVLAN_MODE_BRIDGE,
	}
	err = ip.LinkAdd(&macvlan)
	if err != nil {
		alreadyExists, err := ip.LinkByName(attrs.Name)
		if err != nil {
			logger.Error("Failed to create macvlan and there is no existing link with that name", "msg", err)
			os.Exit(1)
		}
		mvlan, ok := alreadyExists.(*ip.Macvlan)
		if !ok {
			logger.Error("Existing macvlan is of wrong type", "macvlan", mvlan)
			os.Exit(1)
		}

		macvlan = *mvlan
	}
	err = ip.LinkSetUp(&macvlan)
	if err != nil {
		logger.Error("Failed to set macvlan link up", "msg", err)
		os.Exit(1)
	}
	gatewayLink = &macvlan

	cfg := ctrl.GetConfigOrDie()
	hostname, err := os.Hostname()
	if err != nil {
		logger.Error("failed to find hostname", "msg", err)
		os.Exit(1)
	}
	l, err := rl.NewFromKubeconfig(
		rl.LeasesResourceLock,
		e.namespace,
		e.lockName,
		rl.ResourceLockConfig{
			Identity: hostname,
		},
		cfg,
		time.Second*2,
	)
	if err != nil {
		logger.Error("Failed to create a resource locker", "msg", err)
		os.Exit(1)
	}
	el, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          l,
		LeaseDuration: time.Second * 5,
		RenewDeadline: time.Second * 3,
		RetryPeriod:   time.Second * 2,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: addIPAddress,
			OnStoppedLeading: removeIPAddress,
			OnNewLeader:      logNewLeader,
		},
	})
	if err != nil {
		logger.Error("failed to create a leader election", "msg", err)
		os.Exit(1)
	}
	for {
		el.Run(ctx)
	}
}

var envs Envs

type PrometheusLogger struct {
	actual slog.Logger
}

func (pl *PrometheusLogger) Println(args ...any) {
	msg := fmt.Sprintln(args...)
	pl.actual.Error("Prometheus collector logs", "msg", msg)
}

var _ promhttp.Logger = &PrometheusLogger{}

func main() {
	logger = *slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := begin()

	envs = getEnvs()
	vlanWatcher = NewWatcher(envs.vlanID)

	go interfaceSetup(ctx, envs)

	mux := http.NewServeMux()
	mux.HandleFunc("/pid", pid)
	mux.HandleFunc("/ready", ready)
	mux.HandleFunc("/macvlan", macvlan)

	// TODO: enable and disable from helm values
	isMonitoringOn := true

	if isMonitoringOn {
		err := testProcFS()
		if err != nil {
			logger.Error("/proc FS is not accessible", "error", err)
			os.Exit(1)
		}
		// Make sure can read info from /proc
		_ = NewManagerCollector(envs.ownerNetName, envs.vlanID)
		lg := PrometheusLogger{
			actual: logger,
		}
		mux.Handle("/metrics", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
			ErrorLog: &lg,
		}))
	}
	server := http.Server{
		Handler: mux,
		Addr:    ":61410",
	}
	if err := server.ListenAndServe(); err != nil {
		logger.Error("Error listening", "msg", err, "addr", server.Addr)
	}
}

func testProcFS() error {
	fs, err := procfs.NewDefaultFS()
	if err != nil {
		return err
	}
	_, err = fs.NetDev()
	if err != nil {
		return err
	}
	return nil
}
