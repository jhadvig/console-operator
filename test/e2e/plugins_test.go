package e2e

import (
	"context"
	"reflect"
	"testing"
	"time"

	consolev1 "github.com/openshift/api/console/v1"
	operatorsv1 "github.com/openshift/api/operator/v1"
	yaml "gopkg.in/yaml.v2"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	consoleapi "github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/console-operator/pkg/console/subresource/consoleserver"
	"github.com/openshift/console-operator/test/e2e/framework"
)

const (
	pluginName = "test-plugin"
)

func setupPluginsTestCase(t *testing.T) (*framework.ClientSet, *operatorsv1.Console) {
	return framework.StandardSetup(t)
}

func cleanupPluginsTestCase(t *testing.T, client *framework.ClientSet) {
	framework.StandardCleanup(t, client)

	err := client.ConsolePlugin.Delete(context.TODO(), pluginName, metav1.DeleteOptions{})
	if err != nil && !apiErrors.IsNotFound(err) {
		t.Fatalf("could not delete cleanup %q plugin, %v", pluginName, err)
	}
	framework.StandardCleanup(t, client)
}

func TestCreatePlugin(t *testing.T) {
	expectedPlugins := []string{pluginName}
	currentPlugins := []string{}
	client, _ := setupPluginsTestCase(t)
	defer cleanupPluginsTestCase(t, client)

	plugin := &consolev1.ConsolePlugin{
		ObjectMeta: v1.ObjectMeta{
			Name: pluginName,
		},
		Spec: consolev1.ConsolePluginSpec{
			DisplayName: "TestPlugin",
			Service: consolev1.ConsolePluginService{
				Name:      "test-plugin-service-name",
				Namespace: "test-plugin-service-namespace",
				Port:      8443,
				Manifest:  "/manifest",
			},
		},
	}
	setOperatorConfigPlugin(t, client)

	_, err := client.ConsolePlugin.Create(context.TODO(), plugin, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("could not create ConsolePlugin custom resource: %s", err)
	}

	err = wait.Poll(1*time.Second, pollTimeout, func() (stop bool, err error) {
		currentPlugins = getConsolePluginsField(t, client)
		if reflect.DeepEqual(expectedPlugins, currentPlugins) {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Errorf("error: expected '%v' plugins, got '%v': '%v'", expectedPlugins, currentPlugins, err)
	}
}

func setOperatorConfigPlugin(t *testing.T, client *framework.ClientSet) {
	operatorConfig, err := client.Operator.Consoles().Get(context.TODO(), consoleapi.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("could not get operator config, %v", err)
	}
	t.Logf("setting plugins to '%s'", pluginName)
	operatorConfig.Spec = operatorsv1.ConsoleSpec{
		OperatorSpec: operatorsv1.OperatorSpec{
			ManagementState: "Managed",
		},
		Plugins: []string{pluginName},
	}

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		_, err = client.Operator.Consoles().Update(context.TODO(), operatorConfig, metav1.UpdateOptions{})
		return err
	})

	if err != nil {
		t.Fatalf("could not update operator config plugins: %v", err)
	}
}

func getConsolePluginsField(t *testing.T, client *framework.ClientSet) []string {
	cm, err := framework.GetConsoleConfigMap(client)
	if err != nil {
		t.Fatalf("error: %s", err)
	}
	consoleConfig := consoleserver.Config{}
	err = yaml.Unmarshal([]byte(cm.Data["console-config.yaml"]), &consoleConfig)
	if err != nil {
		t.Fatalf("could not unmarshal console-config.yaml: %v", err)
	}

	return consoleConfig.Plugins
}
