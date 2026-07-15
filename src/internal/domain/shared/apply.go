package shared

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Apply creates or updates obj with owner reference and mutation hook (ADR-006).
// Retries on resource-version conflicts from concurrent status/owner updates.
func Apply(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, obj client.Object, mutate func() error) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, c, obj, func() error {
			if err := controllerutil.SetControllerReference(owner, obj, scheme); err != nil {
				return err
			}
			return mutate()
		})
		return err
	})
}
