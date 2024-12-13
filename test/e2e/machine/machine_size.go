package machine

// DigitalOceanSize represents the available sizes in DigitalOcean
type DigitalOceanSize struct {
	S1VCPU1GB    string
	S4VCPU8GB    string
	S8VCPU16GB   string
	G16VCPU64GB  string
	C216VCPU32GB string
}

// ScalewaySize represents the available sizes in Scaleway
type ScalewaySize struct {
	ENT1M string
}

// CloudSize represents the available sizes in various cloud providers
type CloudSize struct {
	DigitalOcean DigitalOceanSize
	Scaleway     ScalewaySize
}

var Sizes = CloudSize{
	DigitalOcean: DigitalOceanSize{
		S1VCPU1GB:    "s-1vcpu-1gb",
		S4VCPU8GB:    "s-4vcpu-8gb",
		S8VCPU16GB:   "s-8vcpu-16gb",
		G16VCPU64GB:  "g-16vcpu-64gb",
		C216VCPU32GB: "c2-16vcpu-32gb",
	},
	Scaleway: ScalewaySize{
		ENT1M: "ent-1m",
	},
}
