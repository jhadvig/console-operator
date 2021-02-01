package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/util/retry"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorsv1 "github.com/openshift/api/operator/v1"
	consoleapi "github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/console-operator/test/e2e/framework"
)

const (
	statuspageIDField = "statuspageID"
	providersField    = "providers"
)

func setupProvidersTestCase(t *testing.T) (*framework.ClientSet, *operatorsv1.Console) {
	return framework.StandardSetup(t)
}

func cleanupProvidersTestCase(t *testing.T, client *framework.ClientSet) {
	framework.StandardCleanup(t, client)
}

func TestProvidersSetStatuspageID(t *testing.T) {
	expectedStatuspageID := "id-1234"
	currentStatuspageID := ""
	client, _ := setupProvidersTestCase(t)
	defer cleanupProvidersTestCase(t, client)
	setOperatorConfigStatuspageIDProvider(t, client, expectedStatuspageID)

	err := wait.Poll(1*time.Second, pollTimeout, func() (stop bool, err error) {
		currentStatuspageID = getConsoleProviderField(t, client, statuspageIDField)
		if expectedStatuspageID == currentStatuspageID {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Errorf("error: expected '%s' statuspageID, got '%s': '%v'", expectedStatuspageID, currentStatuspageID, err)
	}
}

func TestProvidersSetStatuspageIDFlag(t *testing.T) {
	client, _ := setupProvidersTestCase(t)
	defer cleanupProvidersTestCase(t, client)
	expectedStatuspageID := "id-1234"
	expectedStatuspageFlag := fmt.Sprintf("--statuspage-id=%s", expectedStatuspageID)
	currentStatuspageFlag := ""
	setOperatorConfigStatuspageIDProvider(t, client, expectedStatuspageID)

	err := wait.Poll(1*time.Second, pollTimeout, func() (stop bool, err error) {
		currentStatuspageFlag = getConsoleDeploymentCommand(t, client)
		if expectedStatuspageFlag == currentStatuspageFlag {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Errorf("error: expected '%s' statuspage-id flag, got '%s': '%v'", expectedStatuspageFlag, currentStatuspageFlag, err)
	}
}

func TestProvidersSetStatuspageIDEmpty(t *testing.T) {
	statuspageID := ""
	currentProviders := ""
	expectedProviders := "{}"
	client, _ := setupProvidersTestCase(t)
	defer cleanupProvidersTestCase(t, client)
	setOperatorConfigStatuspageIDProvider(t, client, statuspageID)

	err := wait.Poll(1*time.Second, pollTimeout, func() (stop bool, err error) {
		currentProviders = getConsoleProviderField(t, client, providersField)
		if currentProviders == expectedProviders {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Errorf("error: expected '%s' statuspageID, got '%s': '%v'", expectedProviders, currentProviders, err)
	}
}

func getConsoleDeploymentCommand(t *testing.T, client *framework.ClientSet) string {
	deployment, err := framework.GetConsoleDeployment(client)
	if err != nil {
		t.Fatalf("error: %s", err)
	}

	flag := ""
	consolePodTemplateCommand := deployment.Spec.Template.Spec.Containers[0].Command
	for _, cmdArg := range consolePodTemplateCommand {
		if strings.Contains(cmdArg, "--statuspage-id") {
			flag = cmdArg
			break
		}
	}
	return flag
}

func getConsoleProviderField(t *testing.T, client *framework.ClientSet, providerField string) string {
	cm, err := framework.GetConsoleConfigMap(client)
	if err != nil {
		t.Fatalf("error: %s", err)
	}

	data := cm.Data["console-config.yaml"]
	field := ""
	temp := strings.Split(data, "\n")
	for _, item := range temp {
		if strings.Contains(item, providerField) {
			field = strings.TrimSpace(strings.Split(item, ":")[1])
			break
		}
	}
	return field
}

func setOperatorConfigStatuspageIDProvider(t *testing.T, client *framework.ClientSet, statuspageID string) {
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		operatorConfig, err := client.Operator.Consoles().Get(context.TODO(), consoleapi.ConfigResourceName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("could not get operator config, %v", err)
		}
		t.Logf("setting statuspageID to '%s'", statuspageID)
		operatorConfig.Spec = operatorsv1.ConsoleSpec{
			OperatorSpec: operatorsv1.OperatorSpec{
				ManagementState: "Managed",
			},
			Providers: operatorsv1.ConsoleProviders{
				Statuspage: &operatorsv1.StatuspageProvider{
					PageID: statuspageID,
				},
			},
		}

		_, err = client.Operator.Consoles().Update(context.TODO(), operatorConfig, metav1.UpdateOptions{})
		return err
	})

	if err != nil {
		t.Fatalf("could not update operator config providers statupageID: %v", err)
	}
}
