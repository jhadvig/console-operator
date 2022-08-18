package converter

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"

	"github.com/openshift/console-operator/pkg/api"
)

func convertCRD(Object *unstructured.Unstructured, toVersion string) (*unstructured.Unstructured, metav1.Status) {
	convertedObject := Object.DeepCopy()
	fromVersion := Object.GetAPIVersion()

	if toVersion == fromVersion {
		return nil, statusErrorWithMessage("conversion from a version to itself should not call the webhook: %s", toVersion)
	}

	switch Object.GetAPIVersion() {
	case "console.openshift.io/v1alpha1":
		switch toVersion {
		case "console.openshift.io/v1":
			klog.Infof("converting %q object from 'console.openshift.io/v1alpha1' into 'console.openshift.io/v1'", convertedObject.GetName())
			convertPlugins(convertedObject)
		default:
			return nil, statusErrorWithMessage("unexpected conversion version %q", toVersion)
		}
	default:
		return nil, statusErrorWithMessage("unexpected conversion version %q", fromVersion)
	}
	return convertedObject, statusSucceed()
}

func convertPlugins(convertedObject *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	// apiVersion
	unstructured.SetNestedField(convertedObject.Object, "console.openshift.io/v1", "apiVersion")

	// i18n
	if v := convertedObject.GetAnnotations()[api.PluginI18nAnnotation]; v == "true" {
		unstructured.SetNestedField(convertedObject.Object, "Preload", "spec", "i18n", "loadType")
	}

	// service -> backend
	service, _, err := unstructured.NestedFieldCopy(convertedObject.Object, "spec", "service")
	if err != nil {
		return nil, err
	}
	unstructured.SetNestedField(convertedObject.Object, "Service", "spec", "backend", "type")
	unstructured.SetNestedField(convertedObject.Object, service, "spec", "backend", "service")
	unstructured.RemoveNestedField(convertedObject.Object, "spec", "service")

	// proxy
	proxySlice, _, err := unstructured.NestedSlice(convertedObject.Object, "spec", "proxy")
	if err != nil {
		return nil, err
	}

	if len(proxySlice) > 0 {
		for _, proxyUncast := range proxySlice {
			proxyObj := proxyUncast.(map[string]interface{})

			// proxy type
			proxyType, _, err := unstructured.NestedFieldCopy(proxyObj, "type")
			if err != nil {
				klog.Info("Failed to get proxy type")
				return nil, err
			}
			unstructured.SetNestedField(proxyObj, proxyType, "endpoint", "type")
			unstructured.RemoveNestedField(proxyObj, "type")

			//proxy service -> endpoint
			proxyService, _, err := unstructured.NestedFieldCopy(proxyObj, "service")
			if err != nil {
				klog.Info("Failed to get service")
				return nil, err
			}
			unstructured.SetNestedField(proxyObj, proxyService, "endpoint", "service")
			unstructured.RemoveNestedField(proxyObj, "service")

			err = unstructured.SetNestedSlice(convertedObject.Object, proxySlice, "spec", "proxy")
			if err != nil {
				klog.Info("Failed to update the proxy array")
				return nil, err
			}
		}
	}

	return convertedObject, nil
}
