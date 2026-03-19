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

type ClientInterface interface {
	QueryDomainEntryProjection(hostname string) (*metadata.DomainEntryProjection, error)
	QueryDNSRecords(fqdn, recordType string) ([]metadata.DNSRecord, error)
	ApplyPlan(plan *Plan) (*Plan, error)
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) ClientInterface {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    http.DefaultClient,
	}
}

func (c *Client) QueryDomainEntryProjection(hostname string) (*metadata.DomainEntryProjection, error) {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return nil, fmt.Errorf("hostname is required")
	}
	body, err := rawQuery(c, "/manage/query/domain-entry-projection?hostname="+hostname)
	if err != nil {
		return nil, err
	}
	var out metadata.DomainEntryProjection
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode domain entry projection response: %w", err)
	}
	return &out, nil
}

func (c *Client) QueryDNSRecords(fqdn, recordType string) ([]metadata.DNSRecord, error) {
	fqdn = strings.TrimSpace(fqdn)
	recordType = strings.TrimSpace(recordType)
	if fqdn == "" || recordType == "" {
		return nil, fmt.Errorf("fqdn and recordType are required")
	}
	body, err := rawQuery(c, "/manage/query/dns-records?fqdn="+fqdn+"&record_type="+recordType)
	if err != nil {
		return nil, err
	}
	var out []metadata.DNSRecord
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode dns records response: %w", err)
	}
	return out, nil
}

func (c *Client) ApplyPlan(plan *Plan) (*Plan, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan is required")
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("marshal plan request: %w", err)
	}
	respBody, err := rawAbsoluteRequest(c, http.MethodPost, "/manage/plan", body)
	if err != nil {
		return nil, err
	}
	var out Plan
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode plan response: %w", err)
	}
	return &out, nil
}

func rawQuery(c *Client, path string) ([]byte, error) {
	return rawAbsoluteRequest(c, http.MethodGet, path, nil)
}

func rawAbsoluteRequest(c *Client, method, path string, body []byte) ([]byte, error) {
	url := c.baseURL + path
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
