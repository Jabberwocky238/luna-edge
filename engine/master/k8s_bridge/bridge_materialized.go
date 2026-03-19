package k8s_bridge

import (
	"context"
	"errors"
	"strings"

	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
)

type domainMaterialized struct {
	domain   metadata.DomainEndpoint
	backends []metadata.ServiceBackendRef
	routes   []metadata.HTTPRoute
}

func syncDomainSet(ctx context.Context, repo repository.Repository, next map[string]domainMaterialized, affectedHosts []string, removedHosts []string) ([]string, []string, error) {
	if repo == nil {
		return nil, nil, nil
	}
	changedAffected, changedRemoved, err := syncDomainSetOnce(ctx, repo, next, affectedHosts, removedHosts)
	if err != nil {
		return nil, nil, err
	}
	return changedAffected, changedRemoved, nil
}

func syncDomainSetOnce(ctx context.Context, repo repository.Repository, next map[string]domainMaterialized, affectedHosts []string, removedHosts []string) ([]string, []string, error) {
	seen := map[string]struct{}{}
	var changedAffected []string
	var changedRemoved []string
	for _, host := range affectedHosts {
		host = normalizeHost(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		item, ok := next[host]
		if !ok {
			changed, err := deleteManagedDomain(ctx, repo, host)
			if err != nil {
				return nil, nil, err
			}
			if changed {
				changedRemoved = append(changedRemoved, host)
			}
			continue
		}
		changed, err := upsertManagedDomain(ctx, repo, item)
		if err != nil {
			return nil, nil, err
		}
		if item.domain.NeedCert {
			repo.MarkCertificateDesired(ctx, item.domain.Hostname)
		}
		if changed {
			changedAffected = append(changedAffected, host)
		}
	}
	for _, host := range removedHosts {
		host = normalizeHost(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		changed, err := deleteManagedDomain(ctx, repo, host)
		if err != nil {
			return nil, nil, err
		}
		if changed {
			changedRemoved = append(changedRemoved, host)
		}
	}
	return changedAffected, changedRemoved, nil
}

func upsertManagedDomain(ctx context.Context, repo repository.Repository, item domainMaterialized) (bool, error) {
	existing, err := repo.GetDomainEndpointByID(ctx, item.domain.ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	currentRoutes, err := repo.ListHTTPRoutesByDomainID(ctx, item.domain.ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	currentBackends, err := repo.ListServiceBindingsByDomainID(ctx, item.domain.ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	if managedDomainUnchanged(existing, currentRoutes, currentBackends, item) {
		return false, nil
	}
	if err := repo.DomainEndpoints().UpsertResource(ctx, &item.domain); err != nil {
		return false, err
	}
	currentRouteIDs := make(map[string]metadata.HTTPRoute, len(currentRoutes))
	for i := range currentRoutes {
		currentRouteIDs[currentRoutes[i].ID] = currentRoutes[i]
	}
	nextRouteIDs := map[string]struct{}{}
	nextBackendIDs := map[string]struct{}{}
	for i := range item.backends {
		nextBackendIDs[item.backends[i].ID] = struct{}{}
		if err := repo.ServiceBindingRefs().UpsertResource(ctx, &item.backends[i]); err != nil {
			return false, err
		}
	}
	for i := range item.routes {
		nextRouteIDs[item.routes[i].ID] = struct{}{}
		if err := repo.HTTPRoutes().UpsertResource(ctx, &item.routes[i]); err != nil {
			return false, err
		}
	}
	for id, route := range currentRouteIDs {
		if _, ok := nextRouteIDs[id]; ok {
			continue
		}
		if err := repo.HTTPRoutes().DeleteResourceByField(ctx, &metadata.HTTPRoute{}, "id", id); err != nil {
			return false, err
		}
		if strings.HasPrefix(route.BackendRefID, "k8s:backend:") {
			if _, keep := nextBackendIDs[route.BackendRefID]; !keep {
				if err := repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", route.BackendRefID); err != nil {
					return false, err
				}
			}
		}
	}
	if existing != nil && existing.BindedServiceID != "" && strings.HasPrefix(existing.BindedServiceID, "k8s:backend:") {
		if _, keep := nextBackendIDs[existing.BindedServiceID]; !keep {
			if err := repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", existing.BindedServiceID); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func deleteManagedDomain(ctx context.Context, repo repository.Repository, hostname string) (bool, error) {
	domain, err := repo.GetDomainEndpointByHostname(ctx, hostname)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if domain == nil || !strings.HasPrefix(domain.ID, "k8s:domain:") {
		return false, nil
	}
	routes, err := repo.ListHTTPRoutesByDomainID(ctx, domain.ID)
	if err == nil {
		for i := range routes {
			if err := repo.HTTPRoutes().DeleteResourceByField(ctx, &metadata.HTTPRoute{}, "id", routes[i].ID); err != nil {
				return false, err
			}
			if strings.HasPrefix(routes[i].BackendRefID, "k8s:backend:") {
				if err := repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", routes[i].BackendRefID); err != nil {
					return false, err
				}
			}
		}
	}
	if domain.BindedServiceID != "" && strings.HasPrefix(domain.BindedServiceID, "k8s:backend:") {
		if err := repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", domain.BindedServiceID); err != nil {
			return false, err
		}
	}
	if err := repo.DomainEndpoints().DeleteResourceByField(ctx, &metadata.DomainEndpoint{}, "id", domain.ID); err != nil {
		return false, err
	}
	return true, nil
}

func managedDomainUnchanged(existing *metadata.DomainEndpoint, currentRoutes []metadata.HTTPRoute, currentBackends []metadata.ServiceBackendRef, item domainMaterialized) bool {
	if existing == nil {
		return false
	}
	if existing.Hostname != item.domain.Hostname || existing.NeedCert != item.domain.NeedCert || existing.BackendType != item.domain.BackendType || existing.BindedServiceID != item.domain.BindedServiceID {
		return false
	}
	if len(currentRoutes) != len(item.routes) || len(currentBackends) != len(item.backends) {
		return false
	}
	currentRouteMap := make(map[string]metadata.HTTPRoute, len(currentRoutes))
	for i := range currentRoutes {
		currentRouteMap[currentRoutes[i].ID] = currentRoutes[i]
	}
	for i := range item.routes {
		current, ok := currentRouteMap[item.routes[i].ID]
		if !ok || current.DomainEndpointID != item.routes[i].DomainEndpointID || current.Path != item.routes[i].Path || current.Priority != item.routes[i].Priority || current.BackendRefID != item.routes[i].BackendRefID {
			return false
		}
	}
	currentBackendMap := make(map[string]metadata.ServiceBackendRef, len(currentBackends))
	for i := range currentBackends {
		currentBackendMap[currentBackends[i].ID] = currentBackends[i]
	}
	for i := range item.backends {
		current, ok := currentBackendMap[item.backends[i].ID]
		if !ok ||
			current.Type != item.backends[i].Type ||
			current.ArbitraryEndpoint != item.backends[i].ArbitraryEndpoint ||
			current.ServiceNamespace != item.backends[i].ServiceNamespace ||
			current.ServiceName != item.backends[i].ServiceName ||
			current.Port != item.backends[i].Port {
			return false
		}
	}
	return true
}
