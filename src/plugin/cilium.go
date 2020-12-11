package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/accuknox/knoxAutoPolicy/src/libs"
	"github.com/accuknox/knoxAutoPolicy/src/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	flow "github.com/cilium/cilium/api/v1/flow"
	"github.com/cilium/cilium/api/v1/observer"
)

// cidrEanbeld config
var cidrEanbeld bool

func init() {
	env := libs.GetEnv("CIDR_ENABLED", "true")
	if env == "false" {
		cidrEanbeld = false
	} else {
		cidrEanbeld = true
	}
}

// ============================ //
// == Traffic Flow Convertor == //
// ============================ //

// isSynFlagOnly function
func isSynFlagOnly(tcp *flow.TCP) bool {
	if tcp.Flags != nil && tcp.Flags.SYN && !tcp.Flags.ACK {
		return true
	}
	return false
}

// getL4Ports function
func getL4Ports(l4 *flow.Layer4) (int, int) {
	if l4.GetTCP() != nil {
		return int(l4.GetTCP().SourcePort), int(l4.GetTCP().DestinationPort)
	} else if l4.GetUDP() != nil {
		return int(l4.GetUDP().SourcePort), int(l4.GetUDP().DestinationPort)
	} else if l4.GetICMPv4() != nil {
		return int(l4.GetICMPv4().Type), int(l4.GetICMPv4().Code)
	} else {
		return -1, -1
	}
}

// getProtocol function
func getProtocol(l4 *flow.Layer4) int {
	if l4.GetTCP() != nil {
		return 6
	} else if l4.GetUDP() != nil {
		return 17
	} else if l4.GetICMPv4() != nil {
		return 1
	} else {
		return 0 // unknown?
	}
}

// getReservedLabelIfExist function
func getReservedLabelIfExist(labels []string) string {
	for _, label := range labels {
		if strings.HasPrefix(label, "reserved:") {
			return label
		}
	}

	return ""
}

// getHTTP function
func getHTTP(flow *flow.Flow) (string, string) {
	if flow.L7 != nil && flow.L7.GetHttp() != nil {
		if flow.L7.GetType() == 1 { // REQUEST only
			method := flow.L7.GetHttp().GetMethod()
			u, _ := url.Parse(flow.L7.GetHttp().GetUrl())
			path := u.Path
			return method, path
		}
	}

	return "", ""
}

func isFromDNSQuery(log types.KnoxNetworkLog, dnsToIPs map[string][]string) string {
	for domain, v := range dnsToIPs {
		if libs.ContainsElement(v, log.DstPodName) {
			return domain
		}
	}

	return ""
}

// ConvertCiliumFlowToKnoxLog function
func ConvertCiliumFlowToKnoxLog(flow *flow.Flow, dnsToIPs map[string][]string) (types.KnoxNetworkLog, bool) {
	log := types.KnoxNetworkLog{}

	// set action
	if flow.Verdict == 2 {
		log.Action = "deny"
	} else {
		log.Action = "allow"
	}

	// set egress / ingress
	log.Direction = flow.GetTrafficDirection().String()

	// set namespace
	if flow.Source.Namespace == "" {
		log.SrcNamespace = getReservedLabelIfExist(flow.Source.Labels)
	} else {
		log.SrcNamespace = flow.Source.Namespace
	}

	if flow.Destination.Namespace == "" {
		log.DstNamespace = getReservedLabelIfExist(flow.Destination.Labels)
	} else {
		log.DstNamespace = flow.Destination.Namespace
	}

	// set pod
	if flow.Source.PodName == "" {
		log.SrcPodName = flow.IP.Source
	} else {
		log.SrcPodName = flow.Source.GetPodName()
	}

	if flow.Destination.PodName == "" {
		log.DstPodName = flow.IP.Destination
	} else {
		log.DstPodName = flow.Destination.GetPodName()
	}

	// get L3
	if flow.IP != nil {
		log.SrcIP = flow.IP.Source
		log.DstIP = flow.IP.Destination
	} else {
		return log, false
	}

	// get L4
	if flow.L4 != nil {
		log.Protocol = getProtocol(flow.L4)
		if log.Protocol == 6 && flow.L4.GetTCP() != nil { // if tcp,
			log.SynFlag = isSynFlagOnly(flow.L4.GetTCP())
		}

		log.SrcPort, log.DstPort = getL4Ports(flow.L4)
	} else {
		return log, false
	}

	// traffic go to the outside of the cluster,
	if log.DstNamespace == "reserved:world" {
		// filter if the ip is from the DNS query
		dns := isFromDNSQuery(log, dnsToIPs)
		if dns != "" {
			log.DNSQuery = dns
		}
	}

	// get L7 DNS
	if flow.GetL7() != nil && flow.L7.GetDns() != nil {
		if flow.L7.GetType() == 1 { // if DNS REQUEST,
			query := strings.TrimSuffix(flow.L7.GetDns().GetQuery(), ".")

			// if query is in the map,
			if _, ok := dnsToIPs[query]; ok {
				log.DNSQuery = query

				return log, true
			}
		}

		return log, false
	}

	// get L7 HTTP
	if flow.GetL7() != nil && flow.L7.GetHttp() != nil {
		log.HTTPMethod, log.HTTPPath = getHTTP(flow)
		if log.HTTPMethod == "" && log.HTTPPath == "" {
			return log, false
		}
	}

	return log, true
}

// ConvertDocsToCiliumFlows function
func ConvertDocsToCiliumFlows(docs []map[string]interface{}) []*flow.Flow {
	if libs.DBDriver == "mysql" {
		return ConvertMySQLDocsToCiliumFlows(docs)
	} else {
		return ConvertMongoDocsToCiliumFlows(docs)
	}
}

// ConvertMongoDocsToCiliumFlows function
func ConvertMongoDocsToCiliumFlows(docs []map[string]interface{}) []*flow.Flow {
	flows := []*flow.Flow{}

	for _, doc := range docs {
		flow := &flow.Flow{}
		flowByte, _ := json.Marshal(doc)
		json.Unmarshal(flowByte, flow)

		flows = append(flows, flow)
	}

	return flows
}

// ConvertMySQLDocsToCiliumFlows function
func ConvertMySQLDocsToCiliumFlows(docs []map[string]interface{}) []*flow.Flow {
	flows := []*flow.Flow{}

	for _, doc := range docs {
		ciliumFlow := &flow.Flow{}
		var err error

		primitiveDoc := map[string]interface{}{
			"traffic_direction": doc["traffic_direction"],
			"verdict":           doc["verdict"],
			"policy_match_type": doc["policy_match_type"],
			"drop_reason":       doc["drop_reason"],
		}

		flowByte, err := json.Marshal(primitiveDoc)
		if err != nil {
			log.Error().Msg("Error while unmarshing primitives :" + err.Error())
			continue
		}

		err = json.Unmarshal(flowByte, ciliumFlow)
		if err != nil {
			log.Error().Msg("Error while unmarshing primitives :" + err.Error())
			continue
		}

		if doc["event_type"] != nil {
			err = json.Unmarshal(doc["event_type"].([]byte), &ciliumFlow.EventType)
			if err != nil {
				log.Error().Msg("Error while unmarshing event type :" + err.Error())
				continue
			}
		}

		if doc["source"] != nil {
			err = json.Unmarshal(doc["source"].([]byte), &ciliumFlow.Source)
			if err != nil {
				log.Error().Msg("Error while unmarshing source :" + err.Error())
				continue
			}
		}

		if doc["destination"] != nil {
			err = json.Unmarshal(doc["destination"].([]byte), &ciliumFlow.Destination)
			if err != nil {
				log.Error().Msg("Error while unmarshing destination :" + err.Error())
				continue
			}
		}

		if doc["ip"] != nil {
			err = json.Unmarshal(doc["ip"].([]byte), &ciliumFlow.IP)
			if err != nil {
				log.Error().Msg("Error while unmarshing ip :" + err.Error())
				continue
			}
		}

		if doc["l4"] != nil {
			err = json.Unmarshal(doc["l4"].([]byte), &ciliumFlow.L4)
			if err != nil {
				log.Error().Msg("Error while unmarshing l4 :" + err.Error())
				continue
			}
		}

		if doc["l7"] != nil {
			l7Byte := doc["l7"].([]byte)
			if len(l7Byte) != 0 {
				err = json.Unmarshal(l7Byte, &ciliumFlow.L7)
				if err != nil {
					log.Error().Msg("Error while unmarshing l7 :" + err.Error())
					continue
				}
			}
		}

		flows = append(flows, ciliumFlow)
	}

	return flows
}

// ConvertCiliumFlowsToKnoxLogs function
func ConvertCiliumFlowsToKnoxLogs(targetNamespace string, flows []*flow.Flow, dnsToIPs map[string][]string) []types.KnoxNetworkLog {
	logMap := map[types.KnoxNetworkLog]bool{}
	networkLogs := []types.KnoxNetworkLog{}

	for _, flow := range flows {
		if flow.Source.Namespace != targetNamespace && flow.Destination.Namespace != targetNamespace {
			continue
		}

		/*
			// packet is dropped (flow.Verdict == 2) and drop reason == 181 (Policy denied by denylist) ?
			if flow.Verdict == 2 && flow.DropReason == 181 {
				continue
			}
		*/

		if log, valid := ConvertCiliumFlowToKnoxLog(flow, dnsToIPs); valid {
			// networkLogs = append(networkLogs, log)

			if _, ok := logMap[log]; !ok {
				logMap[log] = true
			}
		}
	}

	for k := range logMap {
		networkLogs = append(networkLogs, k)
	}

	return networkLogs
}

// ===================================== //
// == Cilium Network Policy Convertor == //
// ===================================== //

// buildNewCiliumNetworkPolicy function
func buildNewCiliumNetworkPolicy(inPolicy types.KnoxNetworkPolicy) types.CiliumNetworkPolicy {
	ciliumPolicy := types.CiliumNetworkPolicy{}

	ciliumPolicy.APIVersion = "cilium.io/v2"
	ciliumPolicy.Kind = "CiliumNetworkPolicy"
	ciliumPolicy.Metadata = map[string]string{}
	for k, v := range inPolicy.Metadata {
		if k == "name" || k == "namespace" {
			ciliumPolicy.Metadata[k] = v
		}
	}

	// update selector matchLabels
	ciliumPolicy.Spec.Selector.MatchLabels = inPolicy.Spec.Selector.MatchLabels

	return ciliumPolicy
}

// TODO: search core-dns? or statically return dns pod
// getCoreDNSEndpoint function
func getCoreDNSEndpoint(services []types.Service) ([]types.CiliumEndpoint, []types.CiliumPortList) {
	matchLabel := map[string]string{
		"k8s:io.kubernetes.pod.namespace": "kube-system",
		"k8s-app":                         "kube-dns",
	}

	coreDNS := []types.CiliumEndpoint{{matchLabel}}

	ciliumPort := types.CiliumPortList{}
	ciliumPort.Ports = []types.CiliumPort{}
	for _, svc := range services {
		if svc.Namespace == "kube-system" && svc.ServiceName == "kube-dns" {
			ciliumPort.Ports = append(ciliumPort.Ports, types.CiliumPort{
				Port: strconv.Itoa(svc.ServicePort), Protocol: strings.ToUpper(svc.Protocol)},
			)
		}
	}

	toPorts := []types.CiliumPortList{ciliumPort}

	// matchPattern
	dnsRules := []types.SubRule{map[string]string{"matchPattern": "*"}}
	toPorts[0].Rules = map[string][]types.SubRule{"dns": dnsRules}

	return coreDNS, toPorts
}

// ConvertKnoxPolicyToCiliumPolicy function
func ConvertKnoxPolicyToCiliumPolicy(services []types.Service, inPolicy types.KnoxNetworkPolicy) types.CiliumNetworkPolicy {
	ciliumPolicy := buildNewCiliumNetworkPolicy(inPolicy)

	// ====== //
	// Egress //
	// ====== //
	if len(inPolicy.Spec.Egress) > 0 {
		ciliumPolicy.Spec.Egress = []types.CiliumEgress{}

		for _, knoxEgress := range inPolicy.Spec.Egress {
			ciliumEgress := types.CiliumEgress{}

			// ====================== //
			// build label-based rule //
			// ====================== //
			if knoxEgress.MatchLabels != nil {
				ciliumEgress.ToEndpoints = []types.CiliumEndpoint{{knoxEgress.MatchLabels}}

				// ================ //
				// build L4 toPorts //
				// ================ //
				for _, toPort := range knoxEgress.ToPorts {
					if toPort.Port == "" { // if port number is none, skip
						continue
					}

					if ciliumEgress.ToPorts == nil {
						ciliumEgress.ToPorts = []types.CiliumPortList{}
						ciliumPort := types.CiliumPortList{}
						ciliumPort.Ports = []types.CiliumPort{}
						ciliumEgress.ToPorts = append(ciliumEgress.ToPorts, ciliumPort)

						// =============== //
						// build HTTP rule //
						// =============== //
						if len(knoxEgress.ToHTTPs) > 0 {
							ciliumEgress.ToPorts[0].Rules = map[string][]types.SubRule{}

							httpRules := []types.SubRule{}
							for _, http := range knoxEgress.ToHTTPs {
								// matchPattern
								httpRules = append(httpRules, map[string]string{"method": http.Method,
									"path": http.Path})
							}
							ciliumEgress.ToPorts[0].Rules = map[string][]types.SubRule{"http": httpRules}
						}
					}

					port := types.CiliumPort{Port: toPort.Port, Protocol: strings.ToUpper(toPort.Protocol)}
					ciliumEgress.ToPorts[0].Ports = append(ciliumEgress.ToPorts[0].Ports, port)
				}
			} else if len(knoxEgress.ToCIDRs) > 0 {
				// =============== //
				// build CIDR rule //
				// =============== //
				for _, toCIDR := range knoxEgress.ToCIDRs {
					cidrs := []string{}
					for _, cidr := range toCIDR.CIDRs {
						cidrs = append(cidrs, cidr)
					}
					ciliumEgress.ToCIDRs = cidrs

					// update toPorts if exist
					for _, toPort := range knoxEgress.ToPorts {
						if toPort.Port == "" { // if port number is none, skip
							continue
						}

						if ciliumEgress.ToPorts == nil {
							ciliumEgress.ToPorts = []types.CiliumPortList{}
							ciliumPort := types.CiliumPortList{}
							ciliumPort.Ports = []types.CiliumPort{}
							ciliumEgress.ToPorts = append(ciliumEgress.ToPorts, ciliumPort)
						}

						port := types.CiliumPort{Port: toPort.Port, Protocol: strings.ToUpper(toPort.Protocol)}
						ciliumEgress.ToPorts[0].Ports = append(ciliumEgress.ToPorts[0].Ports, port)
					}
				}
			} else if len(knoxEgress.ToEndtities) > 0 {
				// ================= //
				// build Entity rule //
				// ================= //
				for _, entity := range knoxEgress.ToEndtities {
					if ciliumEgress.ToEndtities == nil {
						ciliumEgress.ToEndtities = []string{}
					}

					ciliumEgress.ToEndtities = append(ciliumEgress.ToEndtities, entity)
				}
			} else if len(knoxEgress.ToServices) > 0 {
				// ================== //
				// build Service rule //
				// ================== //
				for _, service := range knoxEgress.ToServices {
					if ciliumEgress.ToServices == nil {
						ciliumEgress.ToServices = []types.CiliumService{}
					}

					ciliumService := types.CiliumService{
						K8sService: types.CiliumK8sService{
							ServiceName: service.ServiceName,
							Namespace:   service.Namespace,
						},
					}

					ciliumEgress.ToServices = append(ciliumEgress.ToServices, ciliumService)
				}
			} else if len(knoxEgress.ToFQDNs) > 0 {
				// =============== //
				// build FQDN rule //
				// =============== //
				for _, fqdn := range knoxEgress.ToFQDNs {
					// TODO: static core-dns
					ciliumEgress.ToEndpoints, ciliumEgress.ToPorts = getCoreDNSEndpoint(services)

					egressFqdn := types.CiliumEgress{}

					if egressFqdn.ToFQDNs == nil {
						egressFqdn.ToFQDNs = []types.CiliumFQDN{}
					}

					// FQDN (+ToPorts)
					for _, matchName := range fqdn.MatchNames {
						egressFqdn.ToFQDNs = append(egressFqdn.ToFQDNs, map[string]string{"matchName": matchName})
					}

					for _, port := range knoxEgress.ToPorts {
						if egressFqdn.ToPorts == nil {
							egressFqdn.ToPorts = []types.CiliumPortList{}
							ciliumPort := types.CiliumPortList{}
							ciliumPort.Ports = []types.CiliumPort{}
							egressFqdn.ToPorts = append(egressFqdn.ToPorts, ciliumPort)
						}

						ciliumPort := types.CiliumPort{Port: port.Port, Protocol: strings.ToUpper(port.Protocol)}
						egressFqdn.ToPorts[0].Ports = append(egressFqdn.ToPorts[0].Ports, ciliumPort)
					}

					ciliumPolicy.Spec.Egress = append(ciliumPolicy.Spec.Egress, egressFqdn)
				}
			}

			ciliumPolicy.Spec.Egress = append(ciliumPolicy.Spec.Egress, ciliumEgress)
		}
	}

	// ======= //
	// Ingress //
	// ======= //
	if len(inPolicy.Spec.Ingress) > 0 {
		ciliumPolicy.Spec.Ingress = []types.CiliumIngress{}

		for _, knoxIngress := range inPolicy.Spec.Ingress {
			ciliumIngress := types.CiliumIngress{}

			// ================= //
			// build label-based //
			// ================= //
			if knoxIngress.MatchLabels != nil {
				ciliumIngress.FromEndpoints = []types.CiliumEndpoint{{knoxIngress.MatchLabels}}

				// ================ //
				// build L4 toPorts //
				// ================ //
				for _, fromPort := range knoxIngress.ToPorts {
					if ciliumIngress.ToPorts == nil {
						ciliumIngress.ToPorts = []types.CiliumPortList{}
						ciliumPort := types.CiliumPortList{}
						ciliumPort.Ports = []types.CiliumPort{}
						ciliumIngress.ToPorts = append(ciliumIngress.ToPorts, ciliumPort)
					}

					port := types.CiliumPort{Port: fromPort.Port, Protocol: strings.ToUpper(fromPort.Protocol)}
					ciliumIngress.ToPorts[0].Ports = append(ciliumIngress.ToPorts[0].Ports, port)
				}
			}

			// =============== //
			// build CIDR rule //
			// =============== //
			for _, fromCIDR := range knoxIngress.FromCIDRs {
				for _, cidr := range fromCIDR.CIDRs {
					ciliumIngress.FromCIDRs = append(ciliumIngress.FromCIDRs, cidr)
				}
			}

			// ================= //
			// build Entity rule //
			// ================= //
			for _, entity := range knoxIngress.FromEntities {
				if ciliumIngress.FromEntities == nil {
					ciliumIngress.FromEntities = []string{}
				}
				ciliumIngress.FromEntities = append(ciliumIngress.FromEntities, entity)
			}

			ciliumPolicy.Spec.Ingress = append(ciliumPolicy.Spec.Ingress, ciliumIngress)
		}

	}

	return ciliumPolicy
}

// ConvertKnoxPoliciesToCiliumPolicies function
func ConvertKnoxPoliciesToCiliumPolicies(services []types.Service, policies []types.KnoxNetworkPolicy) []types.CiliumNetworkPolicy {
	ciliumPolicies := []types.CiliumNetworkPolicy{}

	for _, policy := range policies {
		ciliumPolicy := ConvertKnoxPolicyToCiliumPolicy(services, policy)
		ciliumPolicies = append(ciliumPolicies, ciliumPolicy)
	}

	return ciliumPolicies
}

// ========================= //
// == Cilium Hubble Relay == //
// ========================= //

// CiliumFlows list
var CiliumFlows []*flow.Flow

// CiliumFlowsMutex mutext
var CiliumFlowsMutex *sync.Mutex

// ConnectHubbleRelay function.
func ConnectHubbleRelay() *grpc.ClientConn {
	url := libs.GetEnv("HUBBLE_URL", "127.0.0.1")
	port := libs.GetEnv("HUBBLE_PORT", "4245")
	addr := url + ":" + port

	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		log.Error().Err(err)
		return nil
	}

	log.Info().Msg("connected to Hubble Relay")
	return conn
}

// GetCiliumFlowsFromHubble function
func GetCiliumFlowsFromHubble() []*flow.Flow {
	CiliumFlowsMutex.Lock()
	result := CiliumFlows
	CiliumFlows = []*flow.Flow{} // reset
	CiliumFlowsMutex.Unlock()

	return result
}

// StartHubbleRelay function
func StartHubbleRelay(StopChan chan struct{}, wg *sync.WaitGroup) {
	conn := ConnectHubbleRelay()
	defer conn.Close()
	defer wg.Done()

	client := observer.NewObserverClient(conn)

	req := &observer.GetFlowsRequest{
		Follow:    true,
		Whitelist: nil,
		Blacklist: nil,
		Since:     timestamppb.Now(),
		Until:     nil,
	}

	// init mutex
	CiliumFlowsMutex = &sync.Mutex{}

	if stream, err := client.GetFlows(context.Background(), req); err == nil {
		for {
			select {
			case <-StopChan:
				return

			default:
				res, err := stream.Recv()
				if err == io.EOF {
					log.Info().Msg("end of file: " + err.Error())
				}

				if err != nil {
					log.Error().Msg("network flow stream stopped: " + err.Error())
				}

				switch r := res.ResponseTypes.(type) {
				case *observer.GetFlowsResponse_Flow:
					flow := r.Flow

					CiliumFlowsMutex.Lock()
					CiliumFlows = append(CiliumFlows, flow)
					CiliumFlowsMutex.Unlock()
				}
			}
		}
	} else {
		log.Error().Msg("unable to stream network flow: " + err.Error())
	}
}
