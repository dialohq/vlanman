package controller

import (
	vlanmanv1 "dialo.ai/vlanman/api/v1"
)

type VlanNetworkState struct {
	Status      map[string]vlanmanv1.ConnectionState
	VlanId      int
	NetworkName string
	Mappings    []vlanmanv1.IPMapping
}
