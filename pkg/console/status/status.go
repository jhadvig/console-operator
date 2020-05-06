package status

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	operatorsv1 "github.com/openshift/api/operator/v1"
	v1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	"github.com/openshift/console-operator/pkg/console/errors"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

// Operator status is set on the Operator Config, which is:
//   group: console.operator.openshift.io
//   kind:  Console
//   name:  console
// And is replicated out onto the the clusteroperator by a separate sync loop:
//   group: config.openshift.io
//   kind:  ClusterOperator
//   name:  console
//

// handleDegraded(), handleProgressing(), handleAvailable() each take a typePrefix string representing "category"
// and a reason string, representing the actual problem.
// the provided err will be used as the detailed message body if it is not nil
// note that available status is desired to be true, where degraded & progressing are desired to be false
// example:
//   c.handleDegraded(operatorConfig, "RouteStatus", "FailedHost", error.New("route is not available at canonical host..."))
// generates:
//   - Type RouteStatusDegraded
//     Status: true
//     Reason: Failedhost
//     Message: error string value is used as message
// all degraded suffix conditions will be aggregated into a final "Degraded" status that will be set on the console ClusterOperator
func HandleDegraded(typePrefix string, reason string, err error) v1helpers.UpdateStatusFunc {
	conditionType := typePrefix + operatorsv1.OperatorStatusTypeDegraded
	condition := handleCondition(conditionType, reason, err)
	return v1helpers.UpdateConditionFn(condition)
}

func HandleProgressing(typePrefix string, reason string, err error) v1helpers.UpdateStatusFunc {
	conditionType := typePrefix + operatorsv1.OperatorStatusTypeProgressing
	condition := handleCondition(conditionType, reason, err)
	return v1helpers.UpdateConditionFn(condition)
}

func HandleAvailable(typePrefix string, reason string, err error) v1helpers.UpdateStatusFunc {
	conditionType := typePrefix + operatorsv1.OperatorStatusTypeAvailable
	condition := handleCondition(conditionType, reason, err)
	return v1helpers.UpdateConditionFn(condition)
}

// HandleProgressingOrDegraded exists until we remove type SyncError
// If isSyncError
// - Type suffix will be set to Progressing
// if it is any other kind of error
// - Type suffix will be set to Degraded
// TODO: when we eliminate the special case SyncError, this helper can go away.
// When we do that, however, we must make sure to register deprecated conditions with NewRemoveStaleConditions()
func HandleProgressingOrDegraded(typePrefix string, reason string, err error) []v1helpers.UpdateStatusFunc {
	updateStatusFuncs := []v1helpers.UpdateStatusFunc{}
	if errors.IsSyncError(err) {
		updateStatusFuncs = append(updateStatusFuncs, HandleDegraded(typePrefix, reason, nil))
		updateStatusFuncs = append(updateStatusFuncs, HandleProgressing(typePrefix, reason, err))
	} else {
		updateStatusFuncs = append(updateStatusFuncs, HandleDegraded(typePrefix, reason, err))
		updateStatusFuncs = append(updateStatusFuncs, HandleProgressing(typePrefix, reason, nil))
	}
	return updateStatusFuncs
}

func handleCondition(conditionTypeWithSuffix string, reason string, err error) operatorsv1.OperatorCondition {
	if err != nil {
		klog.Errorln(conditionTypeWithSuffix, reason, err.Error())
		return operatorsv1.OperatorCondition{
			Type:    conditionTypeWithSuffix,
			Status:  setConditionValue(conditionTypeWithSuffix, err),
			Reason:  reason,
			Message: err.Error(),
		}
	}
	return operatorsv1.OperatorCondition{
		Type:   conditionTypeWithSuffix,
		Status: setConditionValue(conditionTypeWithSuffix, err),
	}
}

// Available is an inversion of the other conditions
func setConditionValue(conditionType string, err error) operatorsv1.ConditionStatus {
	if strings.HasSuffix(conditionType, operatorsv1.OperatorStatusTypeAvailable) {
		if err != nil {
			return operatorsv1.ConditionFalse
		}
		return operatorsv1.ConditionTrue
	}
	if err != nil {
		return operatorsv1.ConditionTrue
	}
	return operatorsv1.ConditionFalse
}

// func IsDegraded(operatorConfig *operatorsv1.Authentication) bool {
// 	for _, condition := range operatorConfig.Status.Conditions {
// 		if strings.HasSuffix(condition.Type, operatorsv1.OperatorStatusTypeDegraded) &&
// 			condition.Status == operatorsv1.ConditionTrue {
// 			return true
// 		}
// 	}
// 	return false
// }

// Lets transition to using this, and get the repetition out of all of the above.
func SyncStatus(ctx context.Context, operatorConfigClient v1.ConsoleInterface, operatorConfig *operatorsv1.Console) (*operatorsv1.Console, error) {
	logConditions(operatorConfig.Status.Conditions)
	updatedConfig, err := operatorConfigClient.UpdateStatus(ctx, operatorConfig, metav1.UpdateOptions{})
	if err != nil {
		errMsg := fmt.Errorf("status update error: %v", err)
		klog.Error(errMsg)
		return nil, errMsg
	}
	return updatedConfig, nil
}

// Outputs the condition as a log message based on the detail of the condition in the form of:
//   Status.Condition.<Condition>: <Bool>
//   Status.Condition.<Condition>: <Bool> (<Reason>)
//   Status.Condition.<Condition>: <Bool> (<Reason>) <Message>
//   Status.Condition.<Condition>: <Bool> <Message>
func logConditions(conditions []operatorsv1.OperatorCondition) {
	klog.V(4).Infoln("Operator.Status.Conditions")

	for _, condition := range conditions {
		buf := bytes.Buffer{}
		buf.WriteString(fmt.Sprintf("Status.Condition.%s: %s", condition.Type, condition.Status))
		hasMessage := condition.Message != ""
		hasReason := condition.Reason != ""
		if hasMessage && hasReason {
			buf.WriteString(" |")
			if hasReason {
				buf.WriteString(fmt.Sprintf(" (%s)", condition.Reason))
			}
			if hasMessage {
				buf.WriteString(fmt.Sprintf(" %s", condition.Message))
			}
		}
		klog.V(4).Infoln(buf.String())
	}
}
