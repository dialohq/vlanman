package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"dialo.ai/vlanman/pkg/comms"
	"github.com/alecthomas/repr"
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
	if watcher != nil {
		if watcher.UP.Load() {
			w.WriteHeader(200)
			return
		}
	}
	w.WriteHeader(500)
}

var watcher *VlanWatcher = nil

func main() {
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

	VlanID, err := strconv.ParseInt(os.Getenv("VLAN_ID"), 10, 64)
	if err != nil {
		log.Error("Couldn't parse vlan id", "id", os.Getenv("VLAN_ID"))
		os.Exit(1)
	}

	watcher = NewWatcher(int(VlanID))
	go func() {
		err = watcher.Watch()
		if err != nil {
			log.Error("Error watching for vlan creation", "error", err)
			os.Exit(1)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/pid", pid)
	mux.HandleFunc("/ready", ready)
	server := http.Server{
		Handler: mux,
		Addr:    ":61410",
	}
	if err := server.ListenAndServe(); err != nil {
		log.Error("Error listening", "addr", server.Addr, "error", err)
	}
}
