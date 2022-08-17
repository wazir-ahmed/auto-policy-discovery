package types

const (
	// KubeArmor VM
	PolicyDiscoveryVMNamespace = "accuknox-vm-namespace"
	PolicyDiscoveryVMPodName   = "accuknox-vm-podname"

	// KubeArmor container
	PolicyDiscoveryContainerNamespace = "container_namespace"
	PolicyDiscoveryContainerPodName   = "container_podname"

	// RecordSeparator - DB separator flag
	RecordSeparator = "^^"
)

const (
	KindKnoxNetworkPolicy     = "KnoxNetworkPolicy"
	KindKnoxHostNetworkPolicy = "KnoxHostNetworkPolicy"
)
