package resourcesyncdestination

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"

	operatorsv1 "github.com/openshift/api/operator/v1"
	operatorclientv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	operatorinformersv1 "github.com/openshift/client-go/operator/informers/externalversions/operator/v1"
	"github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/console-operator/pkg/console/controllers/util"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
)

const (
	controllerWorkQueueKey = "resource-sync-destination-work-queue-key"
	controllerName         = "ConsoleResourceSyncDestinationController"
)

type ResourceSyncDestinationController struct {
	// operatorconfig
	operatorConfigClient operatorclientv1.ConsoleInterface
	configMapClient      coreclientv1.ConfigMapsGetter
	// events
	resourceSyncer resourcesynccontroller.ResourceSyncer
}

func NewResourceSyncDestinationController(
	// operatorconfig
	operatorConfigClient operatorclientv1.ConsoleInterface,
	operatorConfigInformer operatorinformersv1.ConsoleInformer,
	// configmap
	corev1Client coreclientv1.CoreV1Interface,
	// events
	recorder events.Recorder,
	resourceSyncer resourcesynccontroller.ResourceSyncer,
) factory.Controller {
	corev1Client.ConfigMaps(api.OpenShiftConsoleNamespace)

	ctrl := &ResourceSyncDestinationController{
		operatorConfigClient: operatorConfigClient,
		configMapClient:      corev1Client,
		// events
		resourceSyncer: resourceSyncer,
	}

	configNameFilter := util.NamesFilter(api.ConfigResourceName)

	return factory.New().
		WithFilteredEventsInformers( // configs
			configNameFilter,
			operatorConfigInformer.Informer(),
		).WithSync(ctrl.Sync).
		ToController("ConsoleResourceSyncDestinationController", recorder.WithComponentSuffix("console-resource-sync-destination-controller"))
}

func (c *ResourceSyncDestinationController) Sync(ctx context.Context, controllerContext factory.SyncContext) error {
	operatorConfig, err := c.operatorConfigClient.Get(ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	switch operatorConfig.Spec.ManagementState {
	case operatorsv1.Managed:
		klog.V(4).Infoln("console is in a managed state: syncing configmaps")
	case operatorsv1.Unmanaged:
		klog.V(4).Infoln("console is in an unmanaged state: skipping configmaps sync")
		return nil
	case operatorsv1.Removed:
		klog.V(4).Infoln("console is in an removed state: removing synced configmaps")
	default:
		return fmt.Errorf("unknown state: %v", operatorConfig.Spec.ManagementState)
	}

	err = c.syncIngressCert(ctx)

	return err
}

// func (c *ResourceSyncDestinationController) removeDefaultIngressCertConfigMap(ctx context.Context) error {
// 	klog.V(2).Info("deleting default-ingress-cert configmap")
// 	defer klog.V(2).Info("finished deleting default-ingress-cert configmap")
// 	err := c.configMapClient.ConfigMaps(api.OpenShiftConsoleNamespace).Delete(ctx, api.DefaultIngressCertConfigMapName, metav1.DeleteOptions{})
// 	if apierrors.IsNotFound(err) {
// 		return nil
// 	}
// 	return err
// }

func (c *ResourceSyncDestinationController) syncCustomLogo(operatorConfig operatorsv1.Console) error {
	source := resourcesynccontroller.ResourceLocation{}
	logoConfigMapName := operatorConfig.Spec.Customization.CustomLogoFile.Name

	if logoConfigMapName != "" {
		source.Name = logoConfigMapName
		source.Namespace = api.OpenShiftConfigNamespace
	}
	// if no custom logo provided, sync an empty source to delete
	return c.resourceSyncer.SyncConfigMap(
		resourcesynccontroller.ResourceLocation{Namespace: api.OpenShiftConsoleNamespace, Name: api.OpenShiftCustomLogoConfigMapName},
		source,
	)
}

func (c *ResourceSyncDestinationController) syncIngressCert(ctx context.Context) error {
	_, err := c.configMapClient.ConfigMaps(api.OpenShiftConfigManagedNamespace).Get(ctx, api.DefaultIngressCertConfigMapName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	// sync: 'default-ingress-cert' configmap
	// from: 'openshift-config-managed' namespace
	// to:   'openshift-console' namespace
	return c.resourceSyncer.SyncConfigMap(
		resourcesynccontroller.ResourceLocation{Name: api.DefaultIngressCertConfigMapName, Namespace: api.OpenShiftConsoleNamespace},
		resourcesynccontroller.ResourceLocation{Name: api.DefaultIngressCertConfigMapName, Namespace: api.OpenShiftConfigManagedNamespace},
	)
}
