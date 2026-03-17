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
	{name: "certificate_revisions", summary: "manage certificate revision rows"},
	{name: "dns_records", summary: "manage DNS record rows"},
	{name: "domain_endpoints", summary: "manage domain endpoint rows"},
	{name: "http_routes", summary: "manage HTTP route rows"},
	{name: "service_backend_refs", summary: "manage service backend reference rows"},
	{name: "snapshot_records", summary: "manage snapshot record rows"},
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
