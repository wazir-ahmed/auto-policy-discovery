package systempolicy

import (
	"sync"

	"github.com/clarketm/json"

	"github.com/accuknox/auto-policy-discovery/src/libs"
	types "github.com/accuknox/auto-policy-discovery/src/types"
	"sigs.k8s.io/yaml"
)

var PolicyStore libs.PolicyStore

type PubSysPolicy struct {
	data types.KubeArmorPolicy
}

func (p *PubSysPolicy) GetType() string {
	return types.PolicyTypeSystem
}

func (p *PubSysPolicy) GetName() string {
	return p.data.Metadata["name"]
}

func (p *PubSysPolicy) GetNamespace() string {
	return p.data.Metadata["namespace"]
}

func (p *PubSysPolicy) GetCluster() string {
	return p.data.Metadata["clusterName"]
}

func (p *PubSysPolicy) GetLabels() types.LabelMap {
	return p.data.Spec.Selector.MatchLabels
}

func (p *PubSysPolicy) IsKind(kind string) bool {
	if kind == p.data.Kind {
		return true
	}

	return false
}

func (p *PubSysPolicy) ToYaml(kind string) []byte {
	delete(p.data.Metadata, "clusterName")
	delete(p.data.Metadata, "containername")

	jsonBytes, err := json.Marshal(&p.data)
	if err != nil {
		log.Error().Msgf("KubeArmorPolicy json marshal failed err=%v", err.Error())
		return nil
	}

	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		log.Error().Msgf("KubeArmorPolicy json to yaml conversion failed err=%v", err.Error())
		return nil
	}

	return yamlBytes
}

func init() {
	PolicyStore = libs.PolicyStore{
		Consumers: make(map[*libs.PolicyConsumer]struct{}),
		Mutex:     sync.Mutex{},
	}
}

func GetPolicyYamlFromDB(consumer *libs.PolicyConsumer) []*types.PolicyYaml {
	policyYamls, err := libs.GetPolicyYamls(CfgDB, types.PolicyTypeSystem)
	if err != nil {
		log.Error().Msgf("fetching policy yaml from DB failed err=%v", err.Error())
		return nil
	}
	return libs.FilterPolicyYaml(policyYamls, consumer)
}
