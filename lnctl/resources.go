package lnctl

type ResourceInfo struct {
	Name    string
	Summary string
}

type resourceInfo struct {
	name    string
	summary string
}

var supportedResources = []resourceInfo{
	{name: "acme_challenges", summary: "manage ACME challenge rows"},
	{name: "acme_orders", summary: "manage ACME order rows"},
	{name: "attachments", summary: "manage domain-to-node attachment rows"},
	{name: "certificate_revisions", summary: "manage certificate revision rows"},
	{name: "dns_projections", summary: "manage DNS projection rows"},
	{name: "dns_records", summary: "manage DNS record rows"},
	{name: "domain_endpoint_status", summary: "manage domain endpoint status rows"},
	{name: "domain_endpoints", summary: "manage domain endpoint rows"},
	{name: "nodes", summary: "manage node rows"},
	{name: "route_projections", summary: "manage route projection rows"},
	{name: "service_bindings", summary: "manage service binding rows"},
	{name: "zones", summary: "manage zone rows"},
}

func isSupportedResource(name string) bool {
	_, ok := lookupSupportedResource(name)
	return ok
}

func lookupSupportedResource(name string) (resourceInfo, bool) {
	for _, res := range supportedResources {
		if res.name == name {
			return res, true
		}
	}
	return resourceInfo{}, false
}

func SupportedResources() []ResourceInfo {
	out := make([]ResourceInfo, 0, len(supportedResources))
	for _, res := range supportedResources {
		out = append(out, ResourceInfo{Name: res.name, Summary: res.summary})
	}
	return out
}
