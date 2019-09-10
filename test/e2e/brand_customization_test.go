package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	yaml "gopkg.in/yaml.v2"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	configv1 "github.com/openshift/api/config/v1"
	operatorsv1 "github.com/openshift/api/operator/v1"
	consoleapi "github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/console-operator/pkg/console/subresource/consoleserver"
	"github.com/openshift/console-operator/test/e2e/framework"
)

const (
	customLogoVolumeName = "custom-logo"
	customLogoMountPath  = "/var/logo/"
)

func setupCustomBrandTest(t *testing.T, cmName string, fileName string) (*framework.ClientSet, *operatorsv1.Console) {
	clientSet, operatorConfig := framework.StandardSetup(t)
	// ensure it doesn't exist already for some reason
	err := deleteCustomLogoConfigMap(clientSet, cmName)
	if err != nil && !apiErrors.IsNotFound(err) {
		t.Fatalf("could not cleanup previous custom logo configmap, %v", err)
	}
	_, err = createCustomLogoConfigMap(clientSet, cmName, fileName)
	if err != nil && !apiErrors.IsAlreadyExists(err) {
		t.Fatalf("could not create custom logo configmap, %v", err)
	}

	return clientSet, operatorConfig
}
func cleanupCustomBrandTest(t *testing.T, client *framework.ClientSet, cmName string) {
	err := deleteCustomLogoConfigMap(client, cmName)
	if err != nil {
		t.Fatalf("could not delete custom logo configmap, %v", err)
	}
	framework.StandardCleanup(t, client)
}

// TODO: consider break this into several different tests.
// - setup should be same setup across them
// - check for just one thing
// - call cleanup
// - too many things in series here, probably
//
//
// TestBrandCustomization() tests that changing the customization values on the operator-config
// will result in the customization being set on the console-config in openshift-console.
// Implicitly it ensures that the operator-config customization overrides customization set on
// console-config in openshift-config-managed, if the managed configmap exists.
func TestCustomBrand(t *testing.T) {

	// t.Skip("Custom Brand Test is flaky, needs a refactor to make it more reliable.")

	// create a configmap with the new logo
	customProductName := "custom name"
	customLogoConfigMapName := "custom-logo"
	customLogoFileName := "pic.png"
	pollFrequency := 1 * time.Second
	pollStandardMax := 20 * time.Second // TODO: maybe longer is all that was needed.
	pollLongMax := 40 * time.Second

	client, operatorConfig := setupCustomBrandTest(t, customLogoConfigMapName, customLogoFileName)
	// cleanup, defer deletion of the configmap to ensure it happens even if another part of the test fails
	defer cleanupCustomBrandTest(t, client, customLogoConfigMapName)

	operatorConfigWithoutCustomLogo := operatorConfig.DeepCopy()

	// update the operator config with custom branding
	operatorConfigWithCustomLogo := withCustomBrand(operatorConfig, customProductName, customLogoConfigMapName, customLogoFileName)
	fmt.Printf("\n----> %#v \n", operatorConfigWithoutCustomLogo)
	fmt.Printf("\n----SPEC> %#v \n", operatorConfigWithoutCustomLogo.Spec)
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		fmt.Printf("\n----SKIP> %#v \n", operatorConfigWithCustomLogo)
		// updatedConfig, err := client.Operator.Consoles().Update(operatorConfigWithCustomLogo)
		testJSON := []byte(`{"spec":{"customization":{"customLogoFile":{"name":"custom-logo","key":"pic.png"},"customProductName":"custom name"},"managementState":"Managed"}}`)
		updatedConfig, err := client.Operator.Consoles().Patch(consoleapi.ConfigResourceName, types.MergePatchType, testJSON)
		fmt.Printf("\n----UPDATE-ERR> %#v \n", err)
		operatorConfigWithCustomLogo = updatedConfig // shadowing
		return err
	})
	if err != nil {
		t.Fatalf("could not update operator config with custom product name %v and logo %v via configmap %v (%v)", customProductName, customLogoFileName, customLogoConfigMapName, err)
	}
	// fmt.Println("\nSLEEEP")
	// time.Sleep(15 * time.Second)
	// check console-config in openshift-console and verify the config has made it through
	err = wait.Poll(pollFrequency, pollStandardMax, func() (stop bool, err error) {
		cm, err := framework.GetConsoleConfigMap(client)
		if hasCustomBranding(cm, customProductName, customLogoFileName) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		t.Fatalf("custom branding not found in console-config in openshift-console namespace")
	}

	// ensure that custom-logo in openshift-console has been created
	err = wait.Poll(pollFrequency, pollStandardMax, func() (stop bool, err error) {
		_, err = framework.GetCustomLogoConfigMap(client)
		if apiErrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("configmap custom-logo not found in openshift-console")
	}

	// ensure the volume mounts have been added to the deployment
	err = wait.Poll(pollFrequency, pollLongMax, func() (stop bool, err error) {
		deployment, err := framework.GetConsoleDeployment(client)
		fmt.Printf("\n---Spec.Template.Spec.Volumes: \n %#v\n", deployment.Spec.Template.Spec.Volumes)
		fmt.Printf("\n---Spec.Template.Spec.Containers[0].VolumeMounts: \n %#v\n", deployment.Spec.Template.Spec.Containers[0].VolumeMounts)
		volume := findCustomLogoVolume(deployment)
		fmt.Printf("\n---volume: %v\n", volume)
		volumeMount := findCustomLogoVolumeMount(deployment)
		fmt.Printf("\n---volumeMount: %v\n", volumeMount)
		fmt.Println("\n-----------------------------------------")
		return volume && volumeMount, nil
	})
	if err != nil {
		t.Fatalf("error: customization values not on deployment, %v", err)
	}

	// remove the custom logo from the operator config so we can verify that the configmap is cleaned up
	// operatorConfigWithoutCustomLogo := operatorConfigWithCustomLogo.DeepCopy()

	// TODO: errors here
	// operatorConfigWithoutCustomLogo.Spec.Customization = operatorsv1.ConsoleCustomization{
	// 	// CustomLogoFile: nil,
	// }

	// TODO: delete this extra logging

	toLog, _ := json.Marshal(operatorConfigWithoutCustomLogo)
	t.Logf("before: %v", string(toLog))

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		operatorConfigWithoutCustomLogo, err = client.Operator.Consoles().Patch(consoleapi.ConfigResourceName, types.MergePatchType, []byte(`{"spec": {"customization": null}}`))
		// operatorConfigWithoutCustomLogo, err = client.Operator.Consoles().Update(operatorConfigWithoutCustomLogo)
		return err
	})
	if err != nil {
		// TODO: delete this extra logging
		toLog, _ := json.Marshal(operatorConfigWithoutCustomLogo)
		t.Logf("after: %v", string(toLog))

		t.Fatalf("could not clear customizations from operator config: %v", err)
	}

	// ensure that the custom-logo configmap in openshift-console has been removed
	err = client.Core.ConfigMaps(consoleapi.OpenShiftConsoleNamespace).Delete(consoleapi.OpenShiftConsoleConfigMapName, &metav1.DeleteOptions{})
	fmt.Printf("\n---DELETE-ERR->> %v", err)
	err = wait.Poll(pollFrequency, pollStandardMax, func() (stop bool, err error) {
		cm, err := framework.GetCustomLogoConfigMap(client)
		fmt.Printf("\n---CM-->> %v", cm)
		fmt.Printf("\n---ERR->> %v", err)
		if apiErrors.IsNotFound(err) {
			fmt.Println("\n111")
			return true, nil
		}
		if err != nil {
			fmt.Println("\n222")
			return false, err
		}
		fmt.Println("\n333")
		return true, errors.New("configmap custom-logo was not removed")
		// return true, nil
	})
	if err != nil {
		t.Fatalf("configmap custom-logo not found in openshift-console")
	}
}

func hasCustomBranding(cm *v1.ConfigMap, desiredProductName string, desiredLogoFileName string) bool {
	consoleConfig := consoleserver.Config{}
	yaml.Unmarshal([]byte(cm.Data["console-config.yaml"]), &consoleConfig)
	actualProductName := consoleConfig.Customization.CustomProductName
	actualLogoFile := consoleConfig.Customization.CustomLogoFile
	return (desiredProductName == actualProductName) && ("/var/logo/"+desiredLogoFileName == actualLogoFile)
}

func findCustomLogoVolume(deployment *appsv1.Deployment) bool {
	volumes := deployment.Spec.Template.Spec.Volumes
	for _, volume := range volumes {
		if volume.Name == customLogoVolumeName {
			return true
		}
	}
	return false
}

func findCustomLogoVolumeMount(deployment *appsv1.Deployment) bool {
	mounts := deployment.Spec.Template.Spec.Containers[0].VolumeMounts
	for _, mount := range mounts {
		if (mount.Name == customLogoVolumeName) && (mount.MountPath == customLogoMountPath) {
			return true
		}
	}
	return false
}

// dont pass a pointer, we want a copy
func withCustomBrand(operatorConfig *operatorsv1.Console, productName string, configMapName string, fileName string) *operatorsv1.Console {
	operatorConfig.Spec.Customization = operatorsv1.ConsoleCustomization{
		CustomProductName: productName,
		CustomLogoFile: configv1.ConfigMapFileReference{
			Name: configMapName,
			Key:  fileName,
		},
	}
	fmt.Printf("\n=====SPEC> %#v \n", operatorConfig.Spec)
	return operatorConfig
}

func customLogoConfigmap(configMapName string, imageKey string) *v1.ConfigMap {
	var data = make(map[string][]byte)
	data[imageKey] = []byte("iVBORw0KGgoAAAANSUhEUgAAAGQAAABkCAYAAABw4pVUAAADmklEQVR4Xu2bv0tyURzGv1KLDoUgLoIShEODDbaHf0JbuNfSkIlR2NAf4NTgoJtQkoODq4OWlVu0NUWzoERQpmDhywm6vJh6L3bu5SmeM8bx3Od+Pvfx/jLX9vb2UDhgCLgoBMbFZxAKwfJBIWA+KIRC0AiA5eE5hELACIDFYUMoBIwAWBw2hELACIDFYUMoBIwAWBw2hELACIDFYUMoBIwAWBw2hELACIDFYUMoBIwAWBw25C8I8Xg8srCwYOxKp9OR9/d3LbvmdrtlcXHRlrW1BLR5kZkacnR0JMFg0IhWq9WkVCppiXp4eChLS0vGWpeXl1IsFrWs/RsW0SKkXq/L+fm5lv0dFXJ1dSWnp6da1v4Ni1AImCUKoZDpBKLRqKysrBiTbm5u5PHxEQybfXHgGmLfrv6OlSkEzJN2IaFQSFZXVyUcDovX65XX11d5enoSdbV0f39vuvt+v18CgYAx7+HhQV5eXkw/91cmaBNyfX0tqVRK1I3dpPH29ibZbFYU5EmDl70z/MPO6I1hu90Wn88nLpfL9EAdDoeSy+Xk7u5u7FwK0SBkHNmPjw+Zm5sbC30wGEgikRj7uIVCNApRR//FxYVUKhXp9XoyPz8vsVhMNjY2vskpFArSbDa/CaMQTUKmfRWp517qa+7/oU7wJycnFDJCQMtJXa2pHi6qh4yTRjqdFnUF9jVarZYcHx9TiB1C+v2+7O7uTj2hb25ufn59fY3n52c5ODigEDuEqMvYTCYzVcj6+rrE43FjTrfblWQySSF2CLm9vZV8Pj9VyNrammxtbRlz1D3J3t4ehdghpNFoyNnZGYWY3oWZT9ByUrfygooNMZehZlCINU6OzaIQx1Bb2xCFWOPk2CwKcQy1tQ1RiDVOjs2iEMdQW9sQhVjj5NgsCnEMtbUNUYg1To7NmknI6EukarUq5XJ5auhIJCI7OzvGHPXDBfUOfnTs7+/L8vKy8Wedvxt2jOoPNjSTkB9sjx81IUAhYIcIhVAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhlAIGAGwOGwIhYARAIvDhoAJ+QeTS82niTWiVwAAAABJRU5ErkJggg==")

	cm := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              configMapName,
			Namespace:         consoleapi.OpenShiftConfigNamespace,
			CreationTimestamp: metav1.Time{},
		},
		BinaryData: data,
	}
	return cm
}

func createCustomLogoConfigMap(client *framework.ClientSet, configMapName string, imageKey string) (*v1.ConfigMap, error) {
	fmt.Println("\nCREATE CONFIG_MAP")
	return client.Core.ConfigMaps(consoleapi.OpenShiftConfigNamespace).Create(customLogoConfigmap(configMapName, imageKey))
}

func deleteCustomLogoConfigMap(client *framework.ClientSet, configMapName string) error {
	return client.Core.ConfigMaps(consoleapi.OpenShiftConfigNamespace).Delete(configMapName, &metav1.DeleteOptions{})
}
