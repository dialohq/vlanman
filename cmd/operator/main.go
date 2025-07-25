package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	vlanmanv1 "dialo.ai/vlanman/api/v1"
	"dialo.ai/vlanman/internal/controller"
	vlanmanwhv1 "dialo.ai/vlanman/internal/webhook/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "net/http/pprof"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	logger = logf.Log.WithName("Setup")
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vlanmanv1.AddToScheme(scheme))
	utilruntime.Must(promv1.AddToScheme(scheme))
}

func main() {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), manager.Options{
		Scheme: scheme,
	})
	if err != nil || mgr == nil {
		logger.Error(err, "Creating manager failed")
		os.Exit(1)
	}

	podTimeout, err := strconv.ParseInt(os.Getenv("POD_WAIT_TIMEOUT"), 10, 64)
	if err != nil {
		logger.Error(err, "Couldn't parse wait for pod timeout")
		os.Exit(1)
	}

	ttlEnv, err := strconv.ParseInt(os.Getenv("JOB_TTL"), 10, 32)
	var ttl *int32
	if err != nil {
		ttl = nil
	} else {
		bit32 := int32(ttlEnv)
		ttl = &bit32
	}

	e := controller.Envs{
		NamespaceName:            os.Getenv("NAMESPACE_NAME"),
		IsMonitoringEnabled:      os.Getenv("MONITORING_ENABLED") == "true",
		MonitoringScrapeInterval: os.Getenv("MONITORING_SCRAPE_INTERVAL"),
		MonitoringReleaseName:    os.Getenv("MONITORING_RELEASE_NAME"),
		VlanManagerImage:         os.Getenv("MANAGER_POD_IMAGE"),
		VlanManagerPullPolicy:    os.Getenv("MANAGER_PULL_POLICY"),
		InterfacePodImage:        os.Getenv("INTERFACE_POD_IMAGE"),
		InterfacePodPullPolicy:   os.Getenv("INTERFACE_PULL_POLICY"),
		WorkerInitImage:          os.Getenv("WORKER_IMAGE"),
		WorkerInitPullPolicy:     os.Getenv("WORKER_PULL_POLICY"),
		ServiceAccountName:       os.Getenv("SERVICE_ACCOUNT_NAME"),
		WaitForPodTimeoutSeconds: podTimeout,
		TTL:                      ttl,
		IsTest:                   false,
	}
	if e.IsMonitoringEnabled {
		logger.Info("Enabling monitoring", "release", e.MonitoringReleaseName)
	}

	if err != nil {
		logger.Error(err, "Couldn't get informer for a daemonset lister")
		os.Exit(1)
	}

	logger.Info("Creating controller")
	if err = (&controller.VlanmanReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: mgr.GetConfig(),
		Env:    e,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "Creating controller failed")
		os.Exit(1)
	}

	logger.Info("New webhook")
	whServer := webhook.NewServer(webhook.Options{
		Port:    vlanmanv1.WebhookServerPort,
		CertDir: vlanmanv1.WebhookServerCertDir,
	})

	if err = mgr.Add(whServer); err != nil {
		logger.Error(err, "Error registering wh server with the manager")
		os.Exit(1)
	}

	mwh := vlanmanwhv1.MutatingWebhookHandler{
		Client: mgr.GetClient(),
		Config: *mgr.GetConfig(),
		Env:    e,
	}
	vwh := vlanmanwhv1.ValidatingWebhookHandler{
		Client: mgr.GetClient(),
		Config: *mgr.GetConfig(),
		Env:    e,
	}
	whServer.Register("/mutating", &mwh)
	whServer.Register("/validating", &vwh)
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		os.Exit(1)
	}
}
