package functions

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (r *GormRepository) ACMEOrders() GenericRepository[*metadata.ACMEOrder] {
	return &gormGenericRepository[*metadata.ACMEOrder]{db: r.db}
}

func (r *GormRepository) ACMEChallenges() GenericRepository[*metadata.ACMEChallenge] {
	return &gormGenericRepository[*metadata.ACMEChallenge]{db: r.db}
}

func (r *GormRepository) UpsertACMEOrder(ctx context.Context, order *metadata.ACMEOrder) error {
	return r.ACMEOrders().UpsertResource(ctx, order)
}

func (r *GormRepository) GetACMEOrder(ctx context.Context, id string) (*metadata.ACMEOrder, error) {
	order := &metadata.ACMEOrder{}
	if err := r.ACMEOrders().GetResourceByField(ctx, order, "id", id); err != nil {
		return nil, err
	}
	return order, nil
}

func (r *GormRepository) UpsertACMEChallenge(ctx context.Context, challenge *metadata.ACMEChallenge) error {
	return r.ACMEChallenges().UpsertResource(ctx, challenge)
}

func (r *GormRepository) ListACMEChallengesByOrderID(ctx context.Context, orderID string) ([]metadata.ACMEChallenge, error) {
	var challenges []metadata.ACMEChallenge
	err := r.db.WithContext(ctx).Order("updated_at asc").Find(&challenges, "acme_order_id = ?", orderID).Error
	return challenges, err
}
