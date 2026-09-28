package usage

import "context"

type requestPluginKey struct{}

// WithRequestPlugin installs a synchronous request-scoped observer. Implementations
// must avoid retaining handler contexts or request/response bodies.
func WithRequestPlugin(ctx context.Context, plugin Plugin) context.Context {
	return context.WithValue(ctx, requestPluginKey{}, plugin)
}

// CopyRequestPlugin carries an observer into a separately derived handler context.
func CopyRequestPlugin(dst, src context.Context) context.Context {
	if src != nil {
		if plugin, ok := src.Value(requestPluginKey{}).(Plugin); ok {
			return WithRequestPlugin(dst, plugin)
		}
	}
	return dst
}
