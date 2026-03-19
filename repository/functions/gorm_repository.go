package functions

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
)

// GormRepository 是基于 GORM 的统一仓储实现。
type GormRepository struct {
	tx                         bool
	db                         *gorm.DB
	certificateDesiredNotifier func(context.Context, string)
}

// NewGormRepository 创建一个基于 GORM 的仓储实现。
func NewGormRepository(db *gorm.DB) Repository {
	return &GormRepository{db: db, tx: false}
}

func (r *GormRepository) Begin(ctx context.Context) (Repository, error) {
	tx := r.db.WithContext(ctx).Begin()
	return &GormRepository{db: tx, tx: true}, nil
}

func (r *GormRepository) Commit(ctx context.Context) error {
	if !r.tx {
		return errors.New("THIS GormRepository IS NOT IN A TX")
	}
	return r.db.WithContext(ctx).Commit().Error
}

func (r *GormRepository) Rollback(ctx context.Context) error {
	if !r.tx {
		return errors.New("THIS GormRepository IS NOT IN A TX")
	}
	return r.db.WithContext(ctx).Rollback().Error
}

func (r *GormRepository) SetCertificateDesiredNotifier(fn func(context.Context, string)) {
	r.certificateDesiredNotifier = fn
}

func (r *GormRepository) MarkCertificateDesired(ctx context.Context, hostname string) {
	if r == nil || r.certificateDesiredNotifier == nil {
		return
	}
	hostname = strings.TrimSpace(strings.ToLower(hostname))
	if hostname == "" {
		return
	}
	r.certificateDesiredNotifier(ctx, hostname)
}

// GetDomainEntryProjectionByDomain 按域名聚合查询 DomainEntryProjection。
func (r *GormRepository) GetDomainEntryProjectionByDomain(ctx context.Context, domain string) (*metadata.DomainEntryProjection, error) {
	type domainEntryProjectionRow struct {
		DomainID                string                       `gorm:"column:domain_id"`
		DomainHost              string                       `gorm:"column:domain_hostname"`
		DomainNeedCert          bool                         `gorm:"column:domain_need_cert"`
		BackendType             metadata.BackendType         `gorm:"column:backend_type"`
		CertID                  *string                      `gorm:"column:cert_id"`
		CertDomainID            *string                      `gorm:"column:cert_domain_endpoint_id"`
		CertRevision            *uint64                      `gorm:"column:cert_revision"`
		CertProvider            *string                      `gorm:"column:cert_provider"`
		CertType                *metadata.ChallengeType      `gorm:"column:cert_challenge_type"`
		CertBucket              *string                      `gorm:"column:cert_artifact_bucket"`
		CertPrefix              *string                      `gorm:"column:cert_artifact_prefix"`
		CertSHA256Crt           *string                      `gorm:"column:cert_sha256_crt"`
		CertSHA256Key           *string                      `gorm:"column:cert_sha256_key"`
		RouteID                 *string                      `gorm:"column:route_id"`
		RoutePath               *string                      `gorm:"column:route_path"`
		RoutePriority           *int32                       `gorm:"column:route_priority"`
		RouteBackendRefID       *string                      `gorm:"column:route_backend_ref_id"`
		RouteBackendType        *metadata.ServiceBackendType `gorm:"column:route_backend_type"`
		RouteArbitraryEndpoint  *string                      `gorm:"column:route_arbitrary_endpoint"`
		RouteServiceNamespace   *string                      `gorm:"column:route_service_namespace"`
		RouteServiceName        *string                      `gorm:"column:route_service_name"`
		RoutePort               *uint32                      `gorm:"column:route_port"`
		BindedBackendRefID      *string                      `gorm:"column:binded_backend_ref_id"`
		BindedBackendType       *metadata.ServiceBackendType `gorm:"column:binded_backend_type"`
		BindedArbitraryEndpoint *string                      `gorm:"column:binded_arbitrary_endpoint"`
		BindedServiceNamespace  *string                      `gorm:"column:binded_service_namespace"`
		BindedServiceName       *string                      `gorm:"column:binded_service_name"`
		BindedPort              *uint32                      `gorm:"column:binded_port"`
	}

	var rows []domainEntryProjectionRow
	err := r.db.WithContext(ctx).Raw(`
SELECT
	de.id AS domain_id,
	de.hostname AS domain_hostname,
	de.need_cert AS domain_need_cert,
	de.backend_type AS backend_type,
	cr.id AS cert_id,
	cr.domain_endpoint_id AS cert_domain_endpoint_id,
	cr.revision AS cert_revision,
	cr.provider AS cert_provider,
	cr.challenge_type AS cert_challenge_type,
	cr.artifact_bucket AS cert_artifact_bucket,
	cr.artifact_prefix AS cert_artifact_prefix,
	cr.sha256_crt AS cert_sha256_crt,
	cr.sha256_key AS cert_sha256_key,
	hr.id AS route_id,
	hr.path AS route_path,
	hr.priority AS route_priority,
	route_sbr.id AS route_backend_ref_id,
	route_sbr.type AS route_backend_type,
	route_sbr.arbitrary_endpoint AS route_arbitrary_endpoint,
	route_sbr.service_namespace AS route_service_namespace,
	route_sbr.service_name AS route_service_name,
	route_sbr.service_port AS route_port,
	binded_sbr.id AS binded_backend_ref_id,
	binded_sbr.type AS binded_backend_type,
	binded_sbr.arbitrary_endpoint AS binded_arbitrary_endpoint,
	binded_sbr.service_namespace AS binded_service_namespace,
	binded_sbr.service_name AS binded_service_name,
	binded_sbr.service_port AS binded_port
FROM domain_endpoints AS de
LEFT JOIN certificate_revisions AS cr
	ON cr.domain_endpoint_id = de.id
	AND cr.deleted = FALSE
	AND cr.revision = (
		SELECT MAX(cr2.revision)
		FROM certificate_revisions AS cr2
		WHERE cr2.deleted = FALSE
			AND cr2.domain_endpoint_id = de.id
	)
LEFT JOIN http_routes AS hr
	ON hr.domain_endpoint_id = de.id
	AND hr.deleted = FALSE
LEFT JOIN service_backend_refs AS route_sbr
	ON route_sbr.id = hr.backend_ref_id
	AND route_sbr.deleted = FALSE
LEFT JOIN service_backend_refs AS binded_sbr
	ON binded_sbr.id = de.binded_service_ref
	AND binded_sbr.deleted = FALSE
WHERE de.deleted = FALSE
	AND de.hostname = ?
ORDER BY hr.priority DESC, LENGTH(hr.path) DESC, hr.id ASC
`, domain).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	projection := &metadata.DomainEntryProjection{
		ID:          rows[0].DomainID,
		Hostname:    rows[0].DomainHost,
		NeedCert:    rows[0].DomainNeedCert,
		BackendType: rows[0].BackendType,
	}

	first := rows[0]
	if first.CertID != nil && *first.CertID != "" {
		projection.Cert = &metadata.CertificateRevision{
			ID:               *first.CertID,
			DomainEndpointID: derefString(first.CertDomainID),
			Revision:         derefUint64(first.CertRevision),
			Provider:         metadata.ACMEProvider(derefString(first.CertProvider)),
			ChallengeType:    derefChallengeType(first.CertType),
			ArtifactBucket:   derefString(first.CertBucket),
			ArtifactPrefix:   derefString(first.CertPrefix),
			SHA256Crt:        derefString(first.CertSHA256Crt),
			SHA256Key:        derefString(first.CertSHA256Key),
		}
	}
	if projection.Cert != nil {
		log.Printf("repository: domain projection cert resolved hostname=%s domain_id=%s cert_id=%s revision=%d", projection.Hostname, projection.ID, projection.Cert.ID, projection.Cert.Revision)
	} else {
		log.Printf("repository: domain projection cert missing hostname=%s domain_id=%s", projection.Hostname, projection.ID)
	}

	if projection.BackendType == metadata.BackendTypeL4TLSPassthrough || projection.BackendType == metadata.BackendTypeL4TLSTermination {
		if first.BindedBackendRefID != nil && *first.BindedBackendRefID != "" {
			projection.BindedBackendRef = &metadata.ServiceBackendRef{
				ID:                *first.BindedBackendRefID,
				Type:              derefServiceBackendType(first.BindedBackendType),
				ArbitraryEndpoint: derefString(first.BindedArbitraryEndpoint),
				ServiceNamespace:  derefString(first.BindedServiceNamespace),
				ServiceName:       derefString(first.BindedServiceName),
				Port:              derefUint32(first.BindedPort),
			}
		}
		return projection, nil
	}

	seenRoutes := map[string]struct{}{}
	for _, row := range rows {
		if row.RouteID == nil || *row.RouteID == "" {
			continue
		}
		if _, ok := seenRoutes[*row.RouteID]; ok {
			continue
		}
		seenRoutes[*row.RouteID] = struct{}{}

		route := metadata.HTTPRouteProjection{
			ID:       *row.RouteID,
			Path:     derefString(row.RoutePath),
			Priority: derefInt32(row.RoutePriority),
		}
		if row.RouteBackendRefID != nil && *row.RouteBackendRefID != "" {
			route.BackendRef = &metadata.ServiceBackendRef{
				ID:                *row.RouteBackendRefID,
				Type:              derefServiceBackendType(row.RouteBackendType),
				ArbitraryEndpoint: derefString(row.RouteArbitraryEndpoint),
				ServiceNamespace:  derefString(row.RouteServiceNamespace),
				ServiceName:       derefString(row.RouteServiceName),
				Port:              derefUint32(row.RoutePort),
			}
		}
		projection.HTTPRoutes = append(projection.HTTPRoutes, route)
	}

	return projection, nil
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func derefUint32(value *uint32) uint32 {
	if value == nil {
		return 0
	}
	return *value
}

func derefInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func derefChallengeType(value *metadata.ChallengeType) metadata.ChallengeType {
	if value == nil {
		return ""
	}
	return *value
}

func derefServiceBackendType(value *metadata.ServiceBackendType) metadata.ServiceBackendType {
	if value == nil || *value == "" {
		return metadata.ServiceBackendTypeSVC
	}
	return *value
}
