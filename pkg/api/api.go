package api

const (
	TargetNamespace    = "openshift-console"
	ConfigResourceName = "cluster"
)

// consts to maintain existing names of various sub-resources
const (
	ClusterOperatorName                 = "console"
	OpenShiftConsoleName                = "console"
	OpenShiftConsoleNamespace           = TargetNamespace
	OpenShiftConsoleOperatorNamespace   = "openshift-console-operator"
	OpenShiftConsoleOperator            = "console-operator"
	OpenShiftConsoleConfigMapName       = "console-config"
	OpenShiftConsolePublicConfigMapName = "console-public"
	OpenshiftConsoleCustomRouteName     = "console-custom"
	ServiceCAConfigMapName              = "service-ca"
	DefaultIngressCertConfigMapName     = "default-ingress-cert"
	OpenShiftConsoleDeploymentName      = OpenShiftConsoleName
	OpenShiftConsoleServiceName         = OpenShiftConsoleName
	OpenShiftConsoleRouteName           = OpenShiftConsoleName
	OpenShiftConsoleDownloadsRouteName  = "downloads"
	OAuthClientName                     = OpenShiftConsoleName
	OpenShiftConfigManagedNamespace     = "openshift-config-managed"
	OpenShiftConfigNamespace            = "openshift-config"
	OpenShiftMonitoringConfigMapName    = "monitoring-shared-config"
	OpenShiftCustomLogoConfigMapName    = "custom-logo"
	TrustedCAConfigMapName              = "trusted-ca-bundle"
	TrustedCABundleKey                  = "ca-bundle.crt"
	TrustedCABundleMountDir             = "/etc/pki/ca-trust/extracted/pem"
	TrustedCABundleMountFile            = "tls-ca-bundle.pem"
	OCCLIDownloadsCustomResourceName    = "oc-cli-downloads"
	ODOCLIDownloadsCustomResourceName   = "odo-cli-downloads"

	ConsoleContainerPortName    = "https"
	ConsoleContainerPort        = 443
	ConsoleContainerTargetPort  = 8443
	RedirectContainerPortName   = "redirect"
	RedirectContainerPort       = 8080
	RedirectContainerTargetPort = 8080
	ConsoleServingCertName      = "console-serving-cert"
)
