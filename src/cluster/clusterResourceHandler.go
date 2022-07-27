package cluster

import (
	"errors"

	"github.com/accuknox/auto-policy-discovery/src/config"
	"github.com/accuknox/auto-policy-discovery/src/types"
)

func GetPods(clusterName string) []types.Pod {
	var pods []types.Pod

	if config.GetCfgClusterInfoFrom() == "k8sclient" { // get from k8s client api
		pods = GetPodsFromK8sClient()
	} else {
		clusterInstance := GetClusterFromClusterName(clusterName)
		if clusterInstance.ClusterID == 0 { // cluster not onboarded
			pods = nil
		} else {
			pods = GetPodsFromCluster(clusterInstance)
		}
	}

	// Append VM pod type to pods
	pods = append(pods, types.Pod{
		Namespace: types.PolicyDiscoveryVMNamespace,
		PodName:   types.PolicyDiscoveryVMPodName,
	})

	return pods
}

func GetAllClusterResources(cluster string) ([]string, []types.Service, []types.Endpoint, []types.Pod, error) {
	if config.GetCfgClusterInfoFrom() == "k8sclient" { // get from k8s client api
		namespaces := GetNamespacesFromK8sClient()
		services := GetServicesFromK8sClient()
		endpoints := GetEndpointsFromK8sClient()
		pods := GetPodsFromK8sClient()

		return namespaces, services, endpoints, pods, nil
	} else {
		clusterInstance := GetClusterFromClusterName(cluster)
		if clusterInstance.ClusterID == 0 { // cluster not onboarded
			return nil, nil, nil, nil, errors.New("Cluster " + cluster + " not onboarded")
		}

		namespaces := GetNamespacesFromCluster(clusterInstance)
		services := GetServicesFromCluster(clusterInstance)
		endpoints := GetEndpointsFromCluster(clusterInstance)
		pods := GetPodsFromCluster(clusterInstance)

		/*
			fmt.Println("ClusterMgmt Data")
			fmt.Println("----------------")
			fmt.Printf("Namespaces --> %v\n\n", namespaces)
			fmt.Printf("Endpoints --> %#v\n\n", endpoints)
			fmt.Printf("Pods --> %#v\n\n", pods)
		*/

		return namespaces, services, endpoints, pods, nil
	}
}
