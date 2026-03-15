package lnctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type ResourceClient[T any] struct {
	client   *Client
	resource string
	idOf     func(T) string
}

type AnyResourceClient interface {
	ListAny() (any, error)
	GetAny(id string) (any, error)
	PutJSON(body []byte) (any, error)
	Delete(id string) error
}

type managedResourceAdapter[T any] struct {
	resourceClient ResourceClient[T]
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    http.DefaultClient,
	}
}

func (c *Client) Zones() ResourceClient[metadata.Zone] {
	return newResourceClient(c, "zones", func(model metadata.Zone) string { return model.ID })
}

func (c *Client) DomainEndpoints() ResourceClient[metadata.DomainEndpoint] {
	return newResourceClient(c, "domain_endpoints", func(model metadata.DomainEndpoint) string { return model.ID })
}

func (c *Client) DomainEndpointStatuses() ResourceClient[metadata.DomainEndpointStatus] {
	return newResourceClient(c, "domain_endpoint_status", func(model metadata.DomainEndpointStatus) string { return model.DomainEndpointID })
}

func (c *Client) ServiceBindings() ResourceClient[metadata.ServiceBinding] {
	return newResourceClient(c, "service_bindings", func(model metadata.ServiceBinding) string { return model.ID })
}

func (c *Client) DNSRecords() ResourceClient[metadata.DNSRecord] {
	return newResourceClient(c, "dns_records", func(model metadata.DNSRecord) string { return model.ID })
}

func (c *Client) DNSProjections() ResourceClient[metadata.DNSProjection] {
	return newResourceClient(c, "dns_projections", func(model metadata.DNSProjection) string { return model.DomainID })
}

func (c *Client) RouteProjections() ResourceClient[metadata.RouteProjection] {
	return newResourceClient(c, "route_projections", func(model metadata.RouteProjection) string { return model.DomainID })
}

func (c *Client) CertificateRevisions() ResourceClient[metadata.CertificateRevision] {
	return newResourceClient(c, "certificate_revisions", func(model metadata.CertificateRevision) string { return model.ID })
}

func (c *Client) ACMEOrders() ResourceClient[metadata.ACMEOrder] {
	return newResourceClient(c, "acme_orders", func(model metadata.ACMEOrder) string { return model.ID })
}

func (c *Client) ACMEChallenges() ResourceClient[metadata.ACMEChallenge] {
	return newResourceClient(c, "acme_challenges", func(model metadata.ACMEChallenge) string { return model.ID })
}

func (c *Client) Nodes() ResourceClient[metadata.Node] {
	return newResourceClient(c, "nodes", func(model metadata.Node) string { return model.ID })
}

func (c *Client) Attachments() ResourceClient[metadata.Attachment] {
	return newResourceClient(c, "attachments", func(model metadata.Attachment) string { return model.ID })
}

func (c *Client) ManageResource(resource string) (AnyResourceClient, error) {
	switch resource {
	case "zones":
		return managedResourceAdapter[metadata.Zone]{resourceClient: c.Zones()}, nil
	case "domain_endpoints":
		return managedResourceAdapter[metadata.DomainEndpoint]{resourceClient: c.DomainEndpoints()}, nil
	case "domain_endpoint_status":
		return managedResourceAdapter[metadata.DomainEndpointStatus]{resourceClient: c.DomainEndpointStatuses()}, nil
	case "service_bindings":
		return managedResourceAdapter[metadata.ServiceBinding]{resourceClient: c.ServiceBindings()}, nil
	case "dns_records":
		return managedResourceAdapter[metadata.DNSRecord]{resourceClient: c.DNSRecords()}, nil
	case "dns_projections":
		return managedResourceAdapter[metadata.DNSProjection]{resourceClient: c.DNSProjections()}, nil
	case "route_projections":
		return managedResourceAdapter[metadata.RouteProjection]{resourceClient: c.RouteProjections()}, nil
	case "certificate_revisions":
		return managedResourceAdapter[metadata.CertificateRevision]{resourceClient: c.CertificateRevisions()}, nil
	case "acme_orders":
		return managedResourceAdapter[metadata.ACMEOrder]{resourceClient: c.ACMEOrders()}, nil
	case "acme_challenges":
		return managedResourceAdapter[metadata.ACMEChallenge]{resourceClient: c.ACMEChallenges()}, nil
	case "nodes":
		return managedResourceAdapter[metadata.Node]{resourceClient: c.Nodes()}, nil
	case "attachments":
		return managedResourceAdapter[metadata.Attachment]{resourceClient: c.Attachments()}, nil
	default:
		return nil, fmt.Errorf("unsupported resource %q", resource)
	}
}

func newResourceClient[T any](client *Client, resource string, idOf func(T) string) ResourceClient[T] {
	return ResourceClient[T]{client: client, resource: resource, idOf: idOf}
}

func (c ResourceClient[T]) List() ([]T, error) {
	return listResource[T](c.client, c.resource)
}

func (c ResourceClient[T]) Get(id string) (*T, error) {
	return getResource[T](c.client, c.resource, id)
}

func (c ResourceClient[T]) Put(model T) (*T, error) {
	return putResource(c.client, c.resource, c.idOf(model), model)
}

func (c ResourceClient[T]) Delete(id string) error {
	return deleteResource(c.client, c.resource, id)
}

func (a managedResourceAdapter[T]) ListAny() (any, error) {
	return a.resourceClient.List()
}

func (a managedResourceAdapter[T]) GetAny(id string) (any, error) {
	return a.resourceClient.Get(id)
}

func (a managedResourceAdapter[T]) PutJSON(body []byte) (any, error) {
	var model T
	if err := json.Unmarshal(body, &model); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	return a.resourceClient.Put(model)
}

func (a managedResourceAdapter[T]) Delete(id string) error {
	return a.resourceClient.Delete(id)
}

func rawRequest(c *Client, method, resource, id string, body []byte) ([]byte, error) {
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	url := c.baseURL + "/manage/" + strings.Trim(resource, "/")
	if id != "" {
		url += "/" + strings.TrimSpace(id)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("master returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func listResource[T any](c *Client, resource string) ([]T, error) {
	body, err := rawRequest(c, http.MethodGet, resource, "", nil)
	if err != nil {
		return nil, err
	}
	var out []T
	if len(bytes.TrimSpace(body)) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode %s list response: %w", resource, err)
	}
	return out, nil
}

func getResource[T any](c *Client, resource, id string) (*T, error) {
	if err := validateRequiredID(resource, id); err != nil {
		return nil, err
	}
	body, err := rawRequest(c, http.MethodGet, resource, id, nil)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", resource, err)
	}
	return &out, nil
}

func putResource[T any](c *Client, resource, id string, model T) (*T, error) {
	if err := validateRequiredID(resource, id); err != nil {
		return nil, err
	}
	body, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("marshal %s request: %w", resource, err)
	}
	respBody, err := rawRequest(c, http.MethodPut, resource, id, body)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", resource, err)
	}
	return &out, nil
}

func deleteResource(c *Client, resource, id string) error {
	if err := validateRequiredID(resource, id); err != nil {
		return err
	}
	_, err := rawRequest(c, http.MethodDelete, resource, id, nil)
	return err
}

func validateResource(resource string) error {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return fmt.Errorf("resource is required")
	}
	if !isSupportedResource(resource) {
		return fmt.Errorf("unsupported resource %q", resource)
	}
	return nil
}

func validateRequiredID(resource, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%s id is required", resource)
	}
	return nil
}
