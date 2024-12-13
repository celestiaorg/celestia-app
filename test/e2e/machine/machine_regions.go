package machine

// DigitalOceanRegion represents the available regions in DigitalOcean
type DigitalOceanRegion struct {
	NYC1 string
	NYC2 string
	NYC3 string
	AMS3 string
	SFO2 string
	SFO3 string
	SGP1 string
	LON1 string
	FRA1 string
	TOR1 string
	BLR1 string
	SYD1 string
}

// ScalewayRegion represents the available regions in Scaleway
type ScalewayRegion struct {
	FR_PAR_1 string
	FR_PAR_2 string
	FR_PAR_3 string
	NL_AMS_1 string
	NL_AMS_2 string
	NL_AMS_3 string
	PL_WAW_1 string
	PL_WAW_2 string
	PL_WAW_3 string
}

// CloudRegion represents the available regions in various cloud providers
type CloudRegion struct {
	DigitalOcean DigitalOceanRegion
	Scaleway     ScalewayRegion
}

var Regions = CloudRegion{
	DigitalOcean: DigitalOceanRegion{
		NYC1: "nyc1",
		NYC2: "nyc2",
		NYC3: "nyc3",
		AMS3: "ams3",
		SFO2: "sfo2",
		SFO3: "sfo3",
		SGP1: "sgp1",
		LON1: "lon1",
		FRA1: "fra1",
		TOR1: "tor1",
		BLR1: "blr1",
		SYD1: "syd1",
	},
	Scaleway: ScalewayRegion{
		FR_PAR_1: "fr-par-1",
		FR_PAR_2: "fr-par-2",
		FR_PAR_3: "fr-par-3",
		NL_AMS_1: "nl-ams-1",
		NL_AMS_2: "nl-ams-2",
		NL_AMS_3: "nl-ams-3",
		PL_WAW_1: "pl-waw-1",
		PL_WAW_2: "pl-waw-2",
		PL_WAW_3: "pl-waw-3",
	},
}
