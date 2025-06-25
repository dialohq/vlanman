package v1

const (
	// Annotation in pod
	PodVlanmanNetworkAnnotation = "vlanman.dialo.ai/network"
	// Label identifying a manager pod
	ManagerPodLabelKey = "vlanman.dialo.ai/manager"
	// Manager pod name prefix
	ManagerPodNamePrefix = "vlan-manager"
	// Manager container name
	ManagerContainerName = "vlan-manager"
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
)
