package controller

type ClusterState struct {
	Nodes map[string]Node
}

type Node struct {
	Managers map[string]ManagerPod // per network name
}
