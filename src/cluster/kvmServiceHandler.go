package cluster

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/accuknox/auto-policy-discovery/src/config"
	"github.com/accuknox/auto-policy-discovery/src/libs"
	"github.com/accuknox/auto-policy-discovery/src/types"
)

func GetResourcesFromKvmService() ([]string, []types.Pod) {
	var namespaces []string
	var pods []types.Pod

	url := config.GetCfgClusterMgmtURL() + "/vmlist"

	log.Info().Msgf("http request url: %s", url)
	resp, err := http.Get(url)
	if err != nil {
		log.Error().Msgf("http response error: %s", err.Error())
		return nil, nil
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error().Msgf("http response error: %s", err.Error())
		return nil, nil
	}

	var endpoints []types.KvmEndpoint

	err = json.Unmarshal(data, &endpoints)
	if err != nil {
		log.Error().Msgf("json unmarshall error: %s", err.Error())
		return nil, nil
	}

	for _, vm := range endpoints {
		// add `reserved:vm` label to all VMs in the KVMS cluster
		newLabels := append(vm.Labels, "reserved:vm")

		pods = append(pods, types.Pod{
			Namespace: vm.Namespace,
			PodName:   vm.VMName,
			Labels:    newLabels,
			Identity:  vm.Identity,
		})

		if !libs.ContainsElement(namespaces, vm.Namespace) {
			namespaces = append(namespaces, vm.Namespace)
		}
	}

	return namespaces, pods
}
