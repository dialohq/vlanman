package controller

import (
	vlanmanv1 "dialo.ai/vlanman/api/v1"
)

type VlanNetworkState struct {
	Status   map[string]vlanmanv1.ConnectionState
	Mappings []vlanmanv1.IPMapping
	VlanId   int
	Name     string
}
