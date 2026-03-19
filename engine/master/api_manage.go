package master

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jabberwocky238/luna-edge/lnctl"
	"github.com/jabberwocky238/luna-edge/replication"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"gorm.io/gorm"
)

type manageAPI struct {
	engine *Engine
}

type planBroadcasts struct {
	deletedDomainEntries []metadata.DomainEntryProjection
	deletedDNSRecords    []metadata.DNSRecord
	domainHosts          map[string]struct{}
	dnsRecordIDs         map[string]struct{}
}

func newManageAPI(engine *Engine) http.Handler {
	api := &manageAPI{engine: engine}
	mux := http.NewServeMux()
	mux.HandleFunc("/manage/plan", api.handlePlan)
	mux.HandleFunc("/manage/query/domain-entry-projection", api.handleDomainEntryProjectionQuery)
	mux.HandleFunc("/manage/query/dns-records", api.handleDNSRecordsQuery)
	mux.HandleFunc("/manage/", api.handleResource)
	return mux
}

func (a *manageAPI) handlePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var plan lnctl.Plan
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.applyPlan(r.Context(), &plan); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (a *manageAPI) handleDomainEntryProjectionQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hostname := strings.TrimSpace(r.URL.Query().Get("hostname"))
	if hostname == "" {
		http.Error(w, "hostname is required", http.StatusBadRequest)
		return
	}
	entry, err := a.engine.Repo.GetDomainEntryProjectionByDomain(r.Context(), hostname)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (a *manageAPI) handleDNSRecordsQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fqdn := strings.TrimSpace(r.URL.Query().Get("fqdn"))
	recordType := strings.TrimSpace(r.URL.Query().Get("record_type"))
	if fqdn == "" || recordType == "" {
		http.Error(w, "fqdn and record_type are required", http.StatusBadRequest)
		return
	}
	records, err := a.engine.Repo.ListDNSRecordsByQuestion(r.Context(), fqdn, recordType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (a *manageAPI) handleResource(w http.ResponseWriter, r *http.Request) {
	resource, id := parseManageResourcePath(r.URL.Path)
	if resource == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if id == "" {
			a.handleListResource(w, r, resource)
			return
		}
		a.handleGetResource(w, r, resource, id)
	case http.MethodPut:
		if id == "" {
			http.Error(w, "resource id is required", http.StatusBadRequest)
			return
		}
		a.handlePutResource(w, r, resource, id)
	case http.MethodDelete:
		if id == "" {
			http.Error(w, "resource id is required", http.StatusBadRequest)
			return
		}
		a.handleDeleteResource(w, r, resource, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *manageAPI) applyPlan(ctx context.Context, plan *lnctl.Plan) error {
	if plan == nil {
		return nil
	}
	txRepo, err := a.engine.Repo.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = txRepo.Rollback(ctx)
		}
	}()

	broadcasts := planBroadcasts{
		domainHosts:  map[string]struct{}{},
		dnsRecordIDs: map[string]struct{}{},
	}

	for _, change := range plan.HTTPRoutes {
		switch change.Action {
		case lnctl.PlanActionCreate, lnctl.PlanActionUpdate:
			if change.Desired != nil {
				if err := txRepo.HTTPRoutes().UpsertResource(ctx, change.Desired); err != nil {
					return err
				}
			}
		case lnctl.PlanActionDelete:
			if change.Current != nil {
				if err := txRepo.HTTPRoutes().DeleteResourceByField(ctx, &metadata.HTTPRoute{}, "id", change.Current.ID); err != nil {
					return err
				}
			}
		}
	}

	for _, change := range plan.ServiceBackendRefs {
		switch change.Action {
		case lnctl.PlanActionCreate, lnctl.PlanActionUpdate:
			if change.Desired != nil {
				if err := txRepo.ServiceBindingRefs().UpsertResource(ctx, change.Desired); err != nil {
					return err
				}
			}
		case lnctl.PlanActionDelete:
			if change.Current != nil {
				if err := txRepo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", change.Current.ID); err != nil {
					return err
				}
			}
		}
	}

	for _, change := range plan.DomainEndpoints {
		switch change.Action {
		case lnctl.PlanActionCreate, lnctl.PlanActionUpdate:
			if change.Desired != nil {
				if err := txRepo.DomainEndpoints().UpsertResource(ctx, change.Desired); err != nil {
					return err
				}
				if hostname := strings.TrimSpace(change.Desired.Hostname); hostname != "" {
					broadcasts.domainHosts[hostname] = struct{}{}
				}
			}
		case lnctl.PlanActionDelete:
			if change.Current != nil {
				if current, err := txRepo.GetDomainEntryProjectionByDomain(ctx, change.Current.Hostname); err == nil && current != nil {
					broadcasts.deletedDomainEntries = append(broadcasts.deletedDomainEntries, *current)
				}
				if err := txRepo.DomainEndpoints().DeleteResourceByField(ctx, &metadata.DomainEndpoint{}, "hostname", change.Current.Hostname); err != nil {
					return err
				}
			}
		}
	}

	for _, change := range plan.DNSRecords {
		switch change.Action {
		case lnctl.PlanActionCreate, lnctl.PlanActionUpdate:
			if change.Desired != nil {
				if err := txRepo.DNSRecords().UpsertResource(ctx, change.Desired); err != nil {
					return err
				}
				if id := strings.TrimSpace(change.Desired.ID); id != "" {
					broadcasts.dnsRecordIDs[id] = struct{}{}
				}
			}
		case lnctl.PlanActionDelete:
			if change.Current != nil {
				broadcasts.deletedDNSRecords = append(broadcasts.deletedDNSRecords, *change.Current)
				if err := txRepo.DNSRecords().DeleteResourceByField(ctx, &metadata.DNSRecord{}, "id", change.Current.ID); err != nil {
					return err
				}
			}
		}
	}

	if hostname := strings.TrimSpace(plan.Hostname); hostname != "" {
		broadcasts.domainHosts[hostname] = struct{}{}
	}

	if err := txRepo.Commit(ctx); err != nil {
		return err
	}
	committed = true

	for _, entry := range broadcasts.deletedDomainEntries {
		entry.Deleted = true
		if err := a.broadcastDeletedDomainEntry(ctx, entry.Hostname, &entry); err != nil {
			return err
		}
	}
	for _, record := range broadcasts.deletedDNSRecords {
		record.Deleted = true
		if err := a.broadcastDeletedDNSRecord(ctx, &record); err != nil {
			return err
		}
	}

	for hostname := range broadcasts.domainHosts {
		if a.engine.Certs != nil {
			domain, err := a.engine.Repo.GetDomainEndpointByHostname(ctx, hostname)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && domain != nil && domain.NeedCert {
				a.engine.Certs.Notify(hostname)
			}
		}
		if err := a.engine.BoardcastDomainEndpointProjection(ctx, hostname); err != nil {
			return err
		}
	}
	for recordID := range broadcasts.dnsRecordIDs {
		if err := a.engine.BoardcastDNSRecord(ctx, recordID); err != nil {
			return err
		}
	}
	return nil
}

func (a *manageAPI) broadcastDeletedDomainEntry(ctx context.Context, hostname string, entry *metadata.DomainEntryProjection) error {
	snapshotRecordID, err := a.engine.appendSnapshotRecord(ctx, metadata.SnapshotSyncTypeDomainEntryProjection, hostname, metadata.SnapshotActionDelete)
	if err != nil {
		return err
	}
	a.engine.Hub.Boardcast(&replication.ChangeNotification{
		NodeID:           a.engine.NODE_ID,
		CreatedAt:        time.Now().UTC(),
		SnapshotRecordID: snapshotRecordID,
		DomainEntry:      entry,
	})
	return nil
}

func (a *manageAPI) broadcastDeletedDNSRecord(ctx context.Context, record *metadata.DNSRecord) error {
	snapshotRecordID, err := a.engine.appendSnapshotRecord(ctx, metadata.SnapshotSyncTypeDNSRecord, record.ID, metadata.SnapshotActionDelete)
	if err != nil {
		return err
	}
	a.engine.Hub.Boardcast(&replication.ChangeNotification{
		NodeID:           a.engine.NODE_ID,
		CreatedAt:        time.Now().UTC(),
		SnapshotRecordID: snapshotRecordID,
		DNSRecord:        record,
	})
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (a *manageAPI) handleListResource(w http.ResponseWriter, r *http.Request, resource string) {
	ctx := r.Context()
	switch resource {
	case "domain_endpoints":
		var out []metadata.DomainEndpoint
		if err := a.engine.Repo.DomainEndpoints().ListResource(ctx, &out, "id asc"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case "service_backend_refs":
		var out []metadata.ServiceBackendRef
		if err := a.engine.Repo.ServiceBindingRefs().ListResource(ctx, &out, "id asc"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case "http_routes":
		var out []metadata.HTTPRoute
		if err := a.engine.Repo.HTTPRoutes().ListResource(ctx, &out, "id asc"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case "dns_records":
		var out []metadata.DNSRecord
		if err := a.engine.Repo.DNSRecords().ListResource(ctx, &out, "id asc"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		http.Error(w, fmt.Sprintf("unsupported resource %q", resource), http.StatusBadRequest)
	}
}

func (a *manageAPI) handleGetResource(w http.ResponseWriter, r *http.Request, resource, id string) {
	ctx := r.Context()
	switch resource {
	case "domain_endpoints":
		item := &metadata.DomainEndpoint{}
		if err := a.engine.Repo.DomainEndpoints().GetResourceByField(ctx, item, "id", id); err != nil {
			handleLookupError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case "service_backend_refs":
		item := &metadata.ServiceBackendRef{}
		if err := a.engine.Repo.ServiceBindingRefs().GetResourceByField(ctx, item, "id", id); err != nil {
			handleLookupError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case "http_routes":
		item := &metadata.HTTPRoute{}
		if err := a.engine.Repo.HTTPRoutes().GetResourceByField(ctx, item, "id", id); err != nil {
			handleLookupError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case "dns_records":
		item := &metadata.DNSRecord{}
		if err := a.engine.Repo.DNSRecords().GetResourceByField(ctx, item, "id", id); err != nil {
			handleLookupError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, fmt.Sprintf("unsupported resource %q", resource), http.StatusBadRequest)
	}
}

func (a *manageAPI) handlePutResource(w http.ResponseWriter, r *http.Request, resource, id string) {
	ctx := r.Context()
	switch resource {
	case "domain_endpoints":
		var item metadata.DomainEndpoint
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		item.Hostname = id
		if err := a.engine.Repo.DomainEndpoints().UpsertResource(ctx, &item); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case "service_backend_refs":
		var item metadata.ServiceBackendRef
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		item.ID = id
		if err := a.engine.Repo.ServiceBindingRefs().UpsertResource(ctx, &item); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case "http_routes":
		var item metadata.HTTPRoute
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		item.ID = id
		if err := a.engine.Repo.HTTPRoutes().UpsertResource(ctx, &item); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, item)
	case "dns_records":
		var item metadata.DNSRecord
		if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		item.ID = id
		if err := a.engine.Repo.DNSRecords().UpsertResource(ctx, &item); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		http.Error(w, fmt.Sprintf("unsupported resource %q", resource), http.StatusBadRequest)
	}
}

func (a *manageAPI) handleDeleteResource(w http.ResponseWriter, r *http.Request, resource, id string) {
	ctx := r.Context()
	var err error
	switch resource {
	case "domain_endpoints":
		err = a.engine.Repo.DomainEndpoints().DeleteResourceByField(ctx, &metadata.DomainEndpoint{}, "id", id)
	case "service_backend_refs":
		err = a.engine.Repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", id)
	case "http_routes":
		err = a.engine.Repo.HTTPRoutes().DeleteResourceByField(ctx, &metadata.HTTPRoute{}, "id", id)
	case "dns_records":
		err = a.engine.Repo.DNSRecords().DeleteResourceByField(ctx, &metadata.DNSRecord{}, "id", id)
	default:
		http.Error(w, fmt.Sprintf("unsupported resource %q", resource), http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseManageResourcePath(path string) (resource, id string) {
	path = strings.TrimPrefix(path, "/manage/")
	path = strings.Trim(path, "/")
	if path == "" {
		return "", ""
	}
	parts := strings.Split(path, "/")
	resource = parts[0]
	if len(parts) > 1 {
		id = strings.Join(parts[1:], "/")
	}
	return resource, id
}

func handleLookupError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		http.NotFound(w, r)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
