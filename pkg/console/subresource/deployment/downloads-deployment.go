package deployment

// kube
// openshift

const (
	ConsoleDownloadsReplicas = 2
)

const (
	ibmCloudManagedAnnotation     = "include.release.openshift.io/ibm-cloud-managed"
	SelfManagedHighAnnotation     = "include.release.openshift.io/self-managed-high-availability"
	SingleNodeDeveloperAnnotation = "include.release.openshift.io/single-node-developer"
)
