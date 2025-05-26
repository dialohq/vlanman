package v1

const (
	// Annotation in pod
	PodVlanmanNetworkAnnotation = "vlanman.dialo.ai/network"
	// Prefix for leasing
	LeasePrefix = "vlanman"
	// Postfix for leasing
	LeasePostfix = "lease-lock"
	// WebhookServerPort is the port used by the webhook server
	WebhookServerPort = 8443
	// WebhookServerCertDir is the directory path for webhook server certificates
	WebhookServerCertDir = "/etc/webhook/certs"
)
