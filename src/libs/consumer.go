package libs

import (
	"sync"

	"github.com/accuknox/auto-policy-discovery/src/types"
	"google.golang.org/grpc"

	dpb "github.com/accuknox/auto-policy-discovery/src/protobuf/v1/discovery"
)

// PolicyConsumer stores filter information provided in v1.Discovery.GetFlow RPC request
type PolicyConsumer struct {
	PolicyType []string
	Kind       []string
	Filter     types.PolicyFilter
	Events     chan *types.PolicyYaml
}

func (pc *PolicyConsumer) IsTypeNetwork() bool {
	return ContainsElement(pc.PolicyType, types.PolicyTypeNetwork)
}

func (pc *PolicyConsumer) IsTypeSystem() bool {
	return ContainsElement(pc.PolicyType, types.PolicyTypeSystem)
}

// PolicyStore is used for support v1.Discovery.GetFlow RPC requests
type PolicyStore struct {
	Consumers map[*PolicyConsumer]struct{}
	Mutex     sync.Mutex
}

// AddConsumer adds a new PolicyConsumer to the store
func (pc *PolicyStore) AddConsumer(c *PolicyConsumer) {
	pc.Mutex.Lock()
	defer pc.Mutex.Unlock()

	pc.Consumers[c] = struct{}{}
	return
}

// RemoveConsumer removes a PolicyConsumer from the store
func (pc *PolicyStore) RemoveConsumer(c *PolicyConsumer) {
	pc.Mutex.Lock()
	defer pc.Mutex.Unlock()

	delete(pc.Consumers, c)
}

// Publish converts the given KnoxPolicy to PolicyYaml and pushes to consumer's channels
func (pc *PolicyStore) Publish(knoxPolicy types.KnoxPolicy) {
	pc.Mutex.Lock()
	defer pc.Mutex.Unlock()

	for consumer := range pc.Consumers {
		policyYamls := ConvertKnoxPolicyToPolicyYaml(knoxPolicy, consumer)
		for _, policyYaml := range policyYamls {
			consumer.Events <- policyYaml
		}
	}
}

func ConvertKnoxPolicyToPolicyYaml(policy types.KnoxPolicy, consumer *PolicyConsumer) []*types.PolicyYaml {
	policyYamls := []*types.PolicyYaml{}

	if !MatchesKnoxPolicy(policy, consumer.Filter) {
		return nil
	}

	for _, kind := range consumer.Kind {
		if !policy.IsKind(kind) {
			continue
		}

		yaml := policy.ToYaml(kind)
		if len(yaml) == 0 {
			continue
		}

		policyYaml := &types.PolicyYaml{
			Type:      policy.GetType(),
			Kind:      kind,
			Name:      policy.GetName(),
			Namespace: policy.GetNamespace(),
			Cluster:   policy.GetCluster(),
			Labels:    policy.GetLabels(),
			Yaml:      yaml,
		}

		policyYamls = append(policyYamls, policyYaml)
	}
	return policyYamls
}

func MatchesKnoxPolicy(policy types.KnoxPolicy, filter types.PolicyFilter) bool {
	if filter.Cluster != "" && filter.Cluster != policy.GetCluster() {
		return false
	}

	if filter.Namespace != "" && filter.Cluster != policy.GetNamespace() {
		return false
	}

	if len(filter.Labels) != 0 && !IsLabelMapSubset(policy.GetLabels(), filter.Labels) {
		return false
	}

	return true
}

func FilterPolicyYaml(py []types.PolicyYaml, c *PolicyConsumer) []*types.PolicyYaml {
	res := []*types.PolicyYaml{}
	for _, p := range py {
		// 1. Passing a `PolicyYaml` to ConvertKnoxPolicyToPolicyYaml()
		//    will filter the object based on `PolicyConsumer` filter parameters.
		// 2. This is possible because `PolicyYaml` implements `KnoxPolicy` interface.
		// 3. Passed object will be returned here if the filter parameters are matched.
		filteredPY := ConvertKnoxPolicyToPolicyYaml(&p, c)
		res = append(res, filteredPY...)
	}

	return res
}

func ConvertGrpcRequestToPolicyFilter(req *dpb.GetPolicyRequest) types.PolicyFilter {
	return types.PolicyFilter{
		Cluster:   req.GetCluster(),
		Namespace: req.GetNamespace(),
		Labels:    LabelMapFromLabelArray(req.GetLabel()),
	}
}

func ConvertPolicyYamlToGrpcResponse(p *types.PolicyYaml) *dpb.GetPolicyResponse {
	return &dpb.GetPolicyResponse{
		Kind:      p.Kind,
		Name:      p.Name,
		Cluster:   p.Cluster,
		Namespace: p.Namespace,
		Label:     LabelMapToLabelArray(p.Labels),
		Yaml:      p.Yaml,
	}
}

func SendPolicyYamlInGrpcStream(stream grpc.ServerStream, policy *types.PolicyYaml) error {
	resp := ConvertPolicyYamlToGrpcResponse(policy)
	err := stream.SendMsg(resp)
	if err != nil {
		log.Error().Msgf("sending network policy yaml in grpc stream failed err=%v", err.Error())
		return err
	}
	return nil
}

func RelayPolicyEventToGrpStream(stream grpc.ServerStream, consumer *PolicyConsumer) {
	for {
		select {
		case <-stream.Context().Done():
			// client disconnected
			return
		case policy, ok := <-consumer.Events:
			if !ok {
				// channel closed and all items are consumed
				return
			}
			err := SendPolicyYamlInGrpcStream(stream, policy)
			if err != nil {
				return
			}
		}
	}
}

func GetPolicyTypeFromKind(kind []string) []string {
	isTypeNetwork := false
	isTypeSystem := false

	for _, k := range kind {
		switch k {
		case types.KindCiliumNetworkPolicy:
		case types.KindK8sNetworkPolicy:
		case types.KindCiliumClusterwideNetworkPolicy:
			isTypeNetwork = true
		case types.KindKubeArmorPolicy:
		case types.KindKubeArmorHostPolicy:
			isTypeSystem = true
		}
	}

	var res []string
	if isTypeNetwork {
		res = append(res, types.PolicyTypeNetwork)
	}
	if isTypeSystem {
		res = append(res, types.PolicyTypeSystem)
	}

	return res
}
