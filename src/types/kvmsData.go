package types

// KvmEndpoint structure
type KvmEndpoint struct {
	VMName    string   `json:"vmName"`
	Labels    []string `json:"labels"`
	Identity  uint32   `json:"identity"`
	Namespace string   `json:"namespace"`
}
