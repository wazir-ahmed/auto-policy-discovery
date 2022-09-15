package networkpolicy

import (
	"sync"

	"github.com/clarketm/json"

	"github.com/accuknox/auto-policy-discovery/src/libs"
	"github.com/accuknox/auto-policy-discovery/src/plugin"
	types "github.com/accuknox/auto-policy-discovery/src/types"
	"sigs.k8s.io/yaml"
)

var PolicyStore libs.PolicyStore

type PubNetPolicy struct {
	data types.KnoxNetworkPolicy
}

func (p *PubNetPolicy) GetType() string {
	return types.PolicyTypeNetwork
}

func (p *PubNetPolicy) GetName() string {
	return p.data.Metadata["name"]
}

func (p *PubNetPolicy) GetNamespace() string {
	return p.data.Metadata["namespace"]
}

func (p *PubNetPolicy) GetCluster() string {
	return p.data.Metadata["cluster_name"]
}

func (p *PubNetPolicy) GetLabels() types.LabelMap {
	return p.data.Spec.Selector.MatchLabels
}

func (p *PubNetPolicy) IsKind(kind string) bool {
	if p.data.Kind == types.KindKnoxNetworkPolicy &&
		(kind == types.KindCiliumNetworkPolicy || kind == types.KindK8sNetworkPolicy) {
		return true
	}

	if p.data.Kind == types.KindKnoxHostNetworkPolicy &&
		kind == types.KindCiliumClusterwideNetworkPolicy {
		return true
	}

	return false
}

func (p *PubNetPolicy) ToYaml(kind string) []byte {
	var policy interface{}

	if kind == types.KindCiliumNetworkPolicy ||
		kind == types.KindCiliumClusterwideNetworkPolicy {
		policy = plugin.ConvertKnoxNetworkPolicyToCiliumPolicy(p.data)
	} else if kind == types.KindK8sNetworkPolicy {
		// TODO: Handle this after k8s network policy support is added
		return nil
	} else {
		return nil
	}

	jsonBytes, err := json.Marshal(&policy)
	if err != nil {
		log.Error().Msgf("NetworkPolicy json marshal failed err=%v", err.Error())
		return nil
	}

	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		log.Error().Msgf("NetworkPolicy json to yaml conversion failed err=%v", err.Error())
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
	policyYamls, err := libs.GetPolicyYamls(CfgDB, types.PolicyTypeNetwork)
	if err != nil {
		log.Error().Msgf("fetching policy yaml from DB failed err=%v", err.Error())
		return nil
	}
	return libs.FilterPolicyYaml(policyYamls, consumer)
}
