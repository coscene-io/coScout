package model

type DeviceStatusResponse struct {
	Authorized bool `json:"authorized"`
	Exist      bool `json:"exist"`
}
