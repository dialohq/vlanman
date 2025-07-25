package comms

type AddVlanRequest struct {
	ID int64 `json:"id"`
}
type AddVlanResponse struct {
	Err error `json:"error"`
}

type PIDResponse struct {
	PID int `json:"pid"`
}

type MacvlanRequest struct {
	NsID int64 `json:"ns_id"`
}

type MacvlanResponse struct {
	Id int `json:"vlan_id"`
}
