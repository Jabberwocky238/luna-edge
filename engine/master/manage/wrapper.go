package manage

import (
	"context"
	"encoding/json"
	"fmt"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository/functions"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

// Wrapper 承担 manage 层的统一 CRUD 和自动广播。
type Wrapper struct {
	repo      functions.Repository
	builder   enginepkg.ProjectionBuilder
	publisher enginepkg.Publisher
}

// NewWrapper 创建 wrapper。
func NewWrapper(repo functions.Repository, builder enginepkg.ProjectionBuilder, publisher enginepkg.Publisher) *Wrapper {
	return &Wrapper{repo: repo, builder: builder, publisher: publisher}
}

type genericResourceRepository interface {
	functions.GenericRepository[any]
}

type genericResourceAdapter[M any] struct {
	repo functions.GenericRepository[M]
	cast func(any) (M, error)
}

func (a genericResourceAdapter[M]) ListResource(ctx context.Context, out any, orderBy string) error {
	return a.repo.ListResource(ctx, out, orderBy)
}

func (a genericResourceAdapter[M]) GetResourceByField(ctx context.Context, out any, field string, value any) error {
	model, err := a.cast(out)
	if err != nil {
		return err
	}
	return a.repo.GetResourceByField(ctx, model, field, value)
}

func (a genericResourceAdapter[M]) UpsertResource(ctx context.Context, model any) error {
	typedModel, err := a.cast(model)
	if err != nil {
		return err
	}
	return a.repo.UpsertResource(ctx, typedModel)
}

func (a genericResourceAdapter[M]) DeleteResourceByField(ctx context.Context, model any, field string, value any) error {
	typedModel, err := a.cast(model)
	if err != nil {
		return err
	}
	return a.repo.DeleteResourceByField(ctx, typedModel, field, value)
}

func castModel[M any](model any) (M, error) {
	typedModel, ok := model.(M)
	if !ok {
		var zero M
		return zero, fmt.Errorf("unexpected model type %T", model)
	}
	return typedModel, nil
}

func (w *Wrapper) resourceRepo(model any) (genericResourceRepository, error) {
	switch model.(type) {
	case *metadata.Zone:
		return genericResourceAdapter[*metadata.Zone]{repo: w.repo.Zones(), cast: castModel[*metadata.Zone]}, nil
	case *metadata.DomainEndpoint:
		return genericResourceAdapter[*metadata.DomainEndpoint]{repo: w.repo.DomainEndpoints(), cast: castModel[*metadata.DomainEndpoint]}, nil
	case *metadata.DomainEndpointStatus:
		return genericResourceAdapter[*metadata.DomainEndpointStatus]{repo: w.repo.DomainEndpointStatuses(), cast: castModel[*metadata.DomainEndpointStatus]}, nil
	case *metadata.ServiceBinding:
		return genericResourceAdapter[*metadata.ServiceBinding]{repo: w.repo.ServiceBindings(), cast: castModel[*metadata.ServiceBinding]}, nil
	case *metadata.HTTPRoute:
		return genericResourceAdapter[*metadata.HTTPRoute]{repo: w.repo.HTTPRoutes(), cast: castModel[*metadata.HTTPRoute]}, nil
	case *metadata.DNSRecord:
		return genericResourceAdapter[*metadata.DNSRecord]{repo: w.repo.DNSRecords(), cast: castModel[*metadata.DNSRecord]}, nil
	case *metadata.CertificateRevision:
		return genericResourceAdapter[*metadata.CertificateRevision]{repo: w.repo.CertificateRevisions(), cast: castModel[*metadata.CertificateRevision]}, nil
	case *metadata.ACMEOrder:
		return genericResourceAdapter[*metadata.ACMEOrder]{repo: w.repo.ACMEOrders(), cast: castModel[*metadata.ACMEOrder]}, nil
	case *metadata.ACMEChallenge:
		return genericResourceAdapter[*metadata.ACMEChallenge]{repo: w.repo.ACMEChallenges(), cast: castModel[*metadata.ACMEChallenge]}, nil
	case *metadata.Node:
		return genericResourceAdapter[*metadata.Node]{repo: w.repo.Nodes(), cast: castModel[*metadata.Node]}, nil
	case *metadata.Attachment:
		return genericResourceAdapter[*metadata.Attachment]{repo: w.repo.Attachments(), cast: castModel[*metadata.Attachment]}, nil
	default:
		return nil, fmt.Errorf("unsupported model type %T", model)
	}
}

// List 返回某资源全量。
func (w *Wrapper) List(ctx context.Context, resource string) (any, error) {
	desc, err := lookupDescriptor(resource)
	if err != nil {
		return nil, err
	}
	resourceRepo, err := w.resourceRepo(desc.newModel())
	if err != nil {
		return nil, err
	}
	slicePtr := newSlicePtr(desc.newModel)
	if err := resourceRepo.ListResource(ctx, slicePtr, desc.idField+" asc"); err != nil {
		return nil, err
	}
	return derefValue(slicePtr), nil
}

// Get 返回单个资源。
func (w *Wrapper) Get(ctx context.Context, resource, id string) (any, error) {
	desc, err := lookupDescriptor(resource)
	if err != nil {
		return nil, err
	}
	model := desc.newModel()
	resourceRepo, err := w.resourceRepo(model)
	if err != nil {
		return nil, err
	}
	if err := resourceRepo.GetResourceByField(ctx, model, desc.idField, id); err != nil {
		return nil, err
	}
	return model, nil
}

// UpsertJSON 执行 upsert 并自动广播。
func (w *Wrapper) UpsertJSON(ctx context.Context, resource string, body []byte) (any, error) {
	desc, err := lookupDescriptor(resource)
	if err != nil {
		return nil, err
	}
	model := desc.newModel()
	if err := json.Unmarshal(body, model); err != nil {
		return nil, err
	}
	resourceRepo, err := w.resourceRepo(model)
	if err != nil {
		return nil, err
	}
	if err := resourceRepo.UpsertResource(ctx, model); err != nil {
		return nil, err
	}
	if err := desc.afterUpsert(ctx, w, model); err != nil {
		return nil, err
	}
	return model, nil
}

// Delete 执行删除并自动广播。
func (w *Wrapper) Delete(ctx context.Context, resource, id string) error {
	desc, err := lookupDescriptor(resource)
	if err != nil {
		return err
	}
	model := desc.newModel()
	resourceRepo, err := w.resourceRepo(model)
	if err != nil {
		return err
	}
	if err := resourceRepo.GetResourceByField(ctx, model, desc.idField, id); err != nil {
		return err
	}
	if err := resourceRepo.DeleteResourceByField(ctx, model, desc.idField, id); err != nil {
		return err
	}
	return desc.afterDelete(ctx, w, model)
}
