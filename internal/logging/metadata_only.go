package logging

import "context"

type metadataOnlyKey struct{}

// WithMetadataOnly prevents upstream diagnostic bodies from entering request logs.
func WithMetadataOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, metadataOnlyKey{}, true)
}

func MetadataOnly(ctx context.Context) bool {
	return ctx != nil && ctx.Value(metadataOnlyKey{}) == true
}
