package server

// Region identifies an Amazon SP-API regional endpoint.
type Region string

const (
	RegionEU Region = "eu"
	RegionNA Region = "na"
	RegionFE Region = "fe"
)

// RegionEndpoints maps each region to its Amazon SP-API hostname.
var RegionEndpoints = map[Region]string{
	RegionEU: "sellingpartnerapi-eu.amazon.com",
	RegionNA: "sellingpartnerapi-na.amazon.com",
	RegionFE: "sellingpartnerapi-fe.amazon.com",
}

// AllRegions returns all supported regions in deterministic order.
func AllRegions() []Region {
	return []Region{RegionEU, RegionNA, RegionFE}
}
