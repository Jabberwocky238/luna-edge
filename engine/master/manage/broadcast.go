package manage

import "context"

func noopBroadcast(context.Context, *Wrapper, any) error { return nil }

func publishDomain(ctx context.Context, w *Wrapper, _ string) error {
	return publishAll(ctx, w)
}

func publishRoute(ctx context.Context, w *Wrapper, _ string, _ string) error {
	return publishAll(ctx, w)
}

func publishCertificate(ctx context.Context, w *Wrapper, _ string, _ uint64) error {
	return publishAll(ctx, w)
}

func publishDelete(ctx context.Context, w *Wrapper, _ string, _ any, _ string) error {
	return publishAll(ctx, w)
}

func publishDeleteForDomain(ctx context.Context, w *Wrapper, _ string, _ any, _ string) error {
	return publishAll(ctx, w)
}

func publishAll(ctx context.Context, w *Wrapper) error {
	if w.publisher == nil {
		return nil
	}
	return w.publisher.PublishNode(ctx, "")
}

func publishAllModel(ctx context.Context, w *Wrapper, _ any) error {
	return publishAll(ctx, w)
}
