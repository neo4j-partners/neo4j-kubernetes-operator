package shared

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// Apply creates or updates obj with owner reference and mutation hook (ADR-006).
// Retries on resource-version conflicts from concurrent status/owner updates.
// Logs create/update at Info and no-ops at V(1) (ADR-014).
//
// NEO-002: refuses to adopt an existing object that is not already controlled by
// this owner (no silent SetControllerReference on foreign / unowned resources).
func Apply(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, obj client.Object, mutate func() error) error {
	log := ctrllog.FromContext(ctx)
	var result controllerutil.OperationResult
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		key := client.ObjectKeyFromObject(obj)
		existing := obj.DeepCopyObject().(client.Object)
		getErr := c.Get(ctx, key, existing)
		switch {
		case getErr == nil:
			if !metav1.IsControlledBy(existing, owner) {
				return fmt.Errorf("refusing to adopt %s %s/%s: exists and is not controlled by %s/%s (NEO-002)",
					objectKind(obj), key.Namespace, key.Name, owner.GetNamespace(), owner.GetName())
			}
		case apierrors.IsNotFound(getErr):
			// create path
		default:
			return getErr
		}

		var applyErr error
		result, applyErr = controllerutil.CreateOrUpdate(ctx, c, obj, func() error {
			if err := controllerutil.SetControllerReference(owner, obj, scheme); err != nil {
				return err
			}
			return mutate()
		})
		return applyErr
	})

	keys := []any{
		"namespace", obj.GetNamespace(),
		"name", obj.GetName(),
		"kind", objectKind(obj),
	}
	if err != nil {
		log.Error(err, "domain apply failed", keys...)
		return err
	}
	switch result {
	case controllerutil.OperationResultCreated, controllerutil.OperationResultUpdated:
		log.Info("domain apply", append(keys, "result", string(result))...)
	default:
		log.V(1).Info("domain apply unchanged", keys...)
	}
	return nil
}

func objectKind(obj client.Object) string {
	if kind := obj.GetObjectKind().GroupVersionKind().Kind; kind != "" {
		return kind
	}
	t := reflect.TypeOf(obj)
	if t == nil {
		return "unknown"
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}
