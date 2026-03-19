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
	var deletedDomainEntries []metadata.DomainEntryProjection
	var deletedDNSRecords []metadata.DNSRecord

	for _, change := range plan.HTTPRoutes {
		switch change.Action {
		case lnctl.PlanActionCreate, lnctl.PlanActionUpdate:
			if change.Desired != nil {
				if err := a.engine.Repo.HTTPRoutes().UpsertResource(ctx, change.Desired); err != nil {
					return err
				}
			}
		case lnctl.PlanActionDelete:
			if change.Current != nil {
				if err := a.engine.Repo.HTTPRoutes().DeleteResourceByField(ctx, &metadata.HTTPRoute{}, "id", change.Current.ID); err != nil {
					return err
				}
			}
		}
	}

	for _, change := range plan.ServiceBackendRefs {
		switch change.Action {
		case lnctl.PlanActionCreate, lnctl.PlanActionUpdate:
			if change.Desired != nil {
				if err := a.engine.Repo.ServiceBindingRefs().UpsertResource(ctx, change.Desired); err != nil {
					return err
				}
			}
		case lnctl.PlanActionDelete:
			if change.Current != nil {
				if err := a.engine.Repo.ServiceBindingRefs().DeleteResourceByField(ctx, &metadata.ServiceBackendRef{}, "id", change.Current.ID); err != nil {
					return err
				}
			}
		}
	}

	for _, change := range plan.DomainEndpoints {
		switch change.Action {
		case lnctl.PlanActionCreate, lnctl.PlanActionUpdate:
			if change.Desired != nil {
				if err := a.engine.Repo.DomainEndpoints().UpsertResource(ctx, change.Desired); err != nil {
					return err
				}
			}
		case lnctl.PlanActionDelete:
			if change.Current != nil {
				if current, err := a.engine.Repo.GetDomainEntryProjectionByDomain(ctx, change.Current.Hostname); err == nil && current != nil {
					deletedDomainEntries = append(deletedDomainEntries, *current)
				}
				if err := a.engine.Repo.DomainEndpoints().DeleteResourceByField(ctx, &metadata.DomainEndpoint{}, "id", change.Current.ID); err != nil {
					return err
				}
			}
		}
	}

	for _, change := range plan.DNSRecords {
		switch change.Action {
		case lnctl.PlanActionCreate, lnctl.PlanActionUpdate:
			if change.Desired != nil {
				if err := a.engine.Repo.DNSRecords().UpsertResource(ctx, change.Desired); err != nil {
					return err
				}
			}
		case lnctl.PlanActionDelete:
			if change.Current != nil {
				deletedDNSRecords = append(deletedDNSRecords, *change.Current)
				if err := a.engine.Repo.DNSRecords().DeleteResourceByField(ctx, &metadata.DNSRecord{}, "id", change.Current.ID); err != nil {
					return err
				}
			}
		}
	}

	for _, entry := range deletedDomainEntries {
		entry.Deleted = true
		if err := a.broadcastDeletedDomainEntry(ctx, entry.Hostname, &entry); err != nil {
			return err
		}
	}
	for _, record := range deletedDNSRecords {
		record.Deleted = true
		if err := a.broadcastDeletedDNSRecord(ctx, &record); err != nil {
			return err
		}
	}

	if strings.TrimSpace(plan.Hostname) != "" {
		if err := a.engine.BoardcastDomainEndpointProjection(ctx, plan.Hostname); err != nil {
			return err
		}
	}
	for _, change := range plan.DNSRecords {
		if change.Action == lnctl.PlanActionDelete {
			continue
		}
		if change.Desired != nil {
			if err := a.engine.BoardcastDNSRecord(ctx, change.Desired.ID); err != nil {
				return err
			}
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
		item.ID = id
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
