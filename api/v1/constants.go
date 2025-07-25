package v1

const (
	// Annotation in pod that ties it to a network CR
	PodVlanmanNetworkAnnotation = "vlanman.dialo.ai/network"
	// Annotation in pod that selects IP
	PodVlanmanIPPoolAnnotation = "vlanman.dialo.ai/pool"
	// Label identifying a manager pod
	ManagerSetLabelKey = "vlanman.dialo.ai/manager"
	// Label identifying a worker pod that should have access to vlan
	WorkerPodLabelKey = "vlanman.dialo.ai/worker"
	// Lease name for IPAM
	LeaseName = "vlanman-ipam-lease"
	// Lease name for leader election among manager pods
	LeaderElectionLeaseName = "vlanman-leader-election"
	// Manager pod name prefix
	ManagerSetNamePrefix = "vlan-manager"
	// Wait for daemon timeout
	WaitForDaemonTimeout = 30
	// Service name suffix
	ServiceNameSuffix = "service"
	// Job name prefix
	JobNamePrefix = "create-vlan-job"
	// Manager container name
	ManagerContainerName = "vlan-manager"
	// Worker init container name
	WorkerInitContainerName = "init-vlan"
	// NodeSelector host name label
	NodeSelectorHostName = "kubernetes.io/hostname"
	// Prefix for leasing
	LeasePrefix = "vlanman"
	// Postfix for leasing
	LeasePostfix = "lease-lock"
	// WebhookServerPort is the port used by the webhook server
	WebhookServerPort = 8443
	// WebhookServerCertDir is the directory path for webhook server certificates
	WebhookServerCertDir = "/etc/webhook/certs"
	// ManagerPodAPIPort is the port on which manager pod is listening
	ManagerPodAPIPort = 61410
	// ManagerPodAPIPortName is the port on which manager pod is listening
	ManagerPodAPIPortName = "api"
	// PodMonitorName is the name that will be given to the pod monitor if monitoring is enabled
	PodMonitorName                     = "vlanman-pod-monitor"
	ReconcilerPendingIPsTimeoutSeconds = 35
	UpdateStatusMaxRetries             = 5
)
