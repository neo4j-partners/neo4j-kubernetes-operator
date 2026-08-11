package shared

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// Apply creates or updates obj with owner reference and mutation hook (ADR-006).
// Retries on resource-version conflicts from concurrent status/owner updates.
// Logs create/update at Info and no-ops at V(1) (ADR-014).
func Apply(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, obj client.Object, mutate func() error) error {
	log := ctrllog.FromContext(ctx)
	var result controllerutil.OperationResult
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
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
