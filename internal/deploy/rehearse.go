package deploy

import "context"

func RehearseRollback(ctx context.Context, adapter Adapter, product, currentRef, previousRef string) error {
	if _, err := adapter.Rollback(ctx, product, previousRef); err != nil {
		return err
	}
	_, err := adapter.DeployPreview(ctx, product, currentRef)
	return err
}
