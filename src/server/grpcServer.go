package server

import (
	"context"
	"errors"

	"github.com/rs/zerolog"

	analyzer "github.com/accuknox/auto-policy-discovery/src/analyzer"
	cfg "github.com/accuknox/auto-policy-discovery/src/config"
	core "github.com/accuknox/auto-policy-discovery/src/config"
	fc "github.com/accuknox/auto-policy-discovery/src/feedconsumer"
	logger "github.com/accuknox/auto-policy-discovery/src/logging"
	network "github.com/accuknox/auto-policy-discovery/src/networkpolicy"
	obs "github.com/accuknox/auto-policy-discovery/src/observability"
	system "github.com/accuknox/auto-policy-discovery/src/systempolicy"

	"github.com/accuknox/auto-policy-discovery/src/insight"
	"github.com/accuknox/auto-policy-discovery/src/libs"
	apb "github.com/accuknox/auto-policy-discovery/src/protobuf/v1/analyzer"
	fpb "github.com/accuknox/auto-policy-discovery/src/protobuf/v1/consumer"
	dpb "github.com/accuknox/auto-policy-discovery/src/protobuf/v1/discovery"
	ipb "github.com/accuknox/auto-policy-discovery/src/protobuf/v1/insight"
	opb "github.com/accuknox/auto-policy-discovery/src/protobuf/v1/observability"
	wpb "github.com/accuknox/auto-policy-discovery/src/protobuf/v1/worker"
	"github.com/accuknox/auto-policy-discovery/src/types"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

const PortNumber = "9089"

var log *zerolog.Logger

func init() {
	log = logger.GetInstance()
}

// ==================== //
// == Worker Service == //
// ==================== //

type workerServer struct {
	wpb.WorkerServer
}

func (s *workerServer) Start(ctx context.Context, in *wpb.WorkerRequest) (*wpb.WorkerResponse, error) {
	log.Info().Msg("Start worker called")

	response := ""

	if in.GetReq() == "dbclear" {
		libs.ClearDBTables(core.CurrentCfg.ConfigDB)
		response += "Cleared DB."
	}

	if in.GetLogfile() != "" {
		core.SetLogFile(in.GetLogfile())
		response += "Log File Set ,"
	}

	if in.GetPolicytype() != "" {
		if in.GetPolicytype() == "network" {
			network.StartNetworkWorker()
		} else if in.GetPolicytype() == "system" {
			system.StartSystemWorker()
		}
		response += "Starting " + in.GetPolicytype() + " policy discovery"
	}

	return &wpb.WorkerResponse{Res: response}, nil
}

func (s *workerServer) Stop(ctx context.Context, in *wpb.WorkerRequest) (*wpb.WorkerResponse, error) {
	log.Info().Msg("Stop worker called")

	if in.GetPolicytype() == "network" {
		network.StopNetworkWorker()
	} else if in.GetPolicytype() == "system" {
		system.StopSystemWorker()
	} else {
		return &wpb.WorkerResponse{Res: "No policy type, choose 'network' or 'system', not [" + in.GetPolicytype() + "]"}, nil
	}

	return &wpb.WorkerResponse{Res: "ok stopping " + in.GetPolicytype() + " policy discovery"}, nil
}

func (s *workerServer) GetWorkerStatus(ctx context.Context, in *wpb.WorkerRequest) (*wpb.WorkerResponse, error) {
	log.Info().Msg("Get worker status called")

	status := ""

	if in.GetPolicytype() == "network" {
		status = network.NetworkWorkerStatus
	} else if in.GetPolicytype() == "system" {
		status = system.SystemWorkerStatus
	} else {
		return &wpb.WorkerResponse{Res: "No policy type, choose 'network' or 'system', not [" + in.GetPolicytype() + "]"}, nil
	}

	return &wpb.WorkerResponse{Res: status}, nil
}

func (s *workerServer) Convert(ctx context.Context, in *wpb.WorkerRequest) (*wpb.WorkerResponse, error) {

	if in.GetPolicytype() == "network" {
		log.Info().Msg("Convert network policy called")
		network.InitNetPolicyDiscoveryConfiguration()
		network.WriteNetworkPoliciesToFile(in.GetClustername(), in.GetNamespace())
		return network.GetNetPolicy(in.Clustername, in.Namespace), nil
	} else if in.GetPolicytype() == "system" {
		log.Info().Msg("Convert system policy called")
		system.InitSysPolicyDiscoveryConfiguration()
		system.WriteSystemPoliciesToFile(in.GetNamespace(), in.GetClustername(), in.GetLabels(), in.GetFromsource())
		return system.GetSysPolicy(in.Namespace, in.Clustername, in.Labels, in.Fromsource), nil
	} else {
		log.Info().Msg("Convert policy called, but no policy type")
	}

	return &wpb.WorkerResponse{Res: "ok"}, nil
}

// ====================== //
// == Discovery Service == //
// ====================== //
type discoveryServer struct {
	dpb.UnimplementedDiscoveryServer
}

func (ds *discoveryServer) GetPolicy(req *dpb.GetPolicyRequest, srv dpb.Discovery_GetPolicyServer) error {
	consumer := &libs.PolicyConsumer{
		Kind:   req.GetKind(),
		Filter: libs.ConvertGrpcRequestToPolicyFilter(req),
		Events: make(chan *types.PolicyYaml, 64),
	}

	consumer.PolicyType = libs.GetPolicyTypeFromKind(consumer.Kind)
	if len(consumer.PolicyType) == 0 {
		return errors.New("Invalid Request")
	}

	var yamlFromDB []*types.PolicyYaml
	if consumer.IsTypeSystem() {
		yamlFromDB = append(yamlFromDB, system.GetPolicyYamlFromDB(consumer)...)
	}
	if consumer.IsTypeNetwork() {
		yamlFromDB = append(yamlFromDB, network.GetPolicyYamlFromDB(consumer)...)
	}

	for _, yaml := range yamlFromDB {
		err := libs.SendPolicyYamlInGrpcStream(srv, yaml)
		if err != nil {
			return nil
		}
	}

	if !req.GetFollow() {
		// client only needs the discovered policy in DB.
		// Not policy update events.
		return nil
	}

	if consumer.IsTypeSystem() {
		system.PolicyStore.AddConsumer(consumer)
		defer system.PolicyStore.RemoveConsumer(consumer)
	}

	if consumer.IsTypeNetwork() {
		network.PolicyStore.AddConsumer(consumer)
		defer network.PolicyStore.RemoveConsumer(consumer)
	}

	// consume policy update events
	libs.RelayPolicyEventToGrpStream(srv, consumer)
	return nil
}

// ====================== //
// == Consumer Service == //
// ====================== //

type consumerServer struct {
	fpb.ConsumerServer
}

func (s *consumerServer) Start(ctx context.Context, in *fpb.ConsumerRequest) (*fpb.ConsumerResponse, error) {
	log.Info().Msg("Start consumer called")
	fc.ConsumerMutex.Lock()
	fc.StartConsumer()
	fc.ConsumerMutex.Unlock()
	return &fpb.ConsumerResponse{Res: "ok"}, nil
}

func (s *consumerServer) Stop(ctx context.Context, in *fpb.ConsumerRequest) (*fpb.ConsumerResponse, error) {
	log.Info().Msg("Stop consumer called")
	fc.ConsumerMutex.Lock()
	fc.StopConsumer()
	fc.ConsumerMutex.Unlock()
	return &fpb.ConsumerResponse{Res: "ok"}, nil
}

func (s *consumerServer) GetWorkerStatus(ctx context.Context, in *fpb.ConsumerRequest) (*fpb.ConsumerResponse, error) {
	log.Info().Msg("Get consumer status called")
	return &fpb.ConsumerResponse{Res: fc.Status}, nil
}

// ====================== //
// == Analyzer Service == //
// ====================== //

type analyzerServer struct {
	apb.AnalyzerServer
}

func (s *analyzerServer) GetNetworkPolicies(ctx context.Context, in *apb.NetworkLogs) (*apb.NetworkPolicies, error) {
	pbNetworkPolicies := apb.NetworkPolicies{}
	pbNetworkPolicies.NwPolicies = analyzer.GetNetworkPolicies(in.GetNwLog())
	return &pbNetworkPolicies, nil
}

func (s *analyzerServer) GetSystemPolicies(ctx context.Context, in *apb.SystemLogs) (*apb.SystemPolicies, error) {
	pbSystemPolicies := apb.SystemPolicies{}
	pbSystemPolicies.SysPolicies = analyzer.GetSystemPolicies(in.GetSysLog())
	return &pbSystemPolicies, nil
}

// ============= //
// == Insight == //
// ============= //

type insightServer struct {
	ipb.InsightServer
}

func (s *insightServer) GetInsightData(ctx context.Context, in *ipb.Request) (*ipb.Response, error) {
	resp, err := insight.GetInsightData(types.InsightRequest{
		Request:       in.Request,
		Source:        in.Source,
		ClusterName:   in.ClusterName,
		Namespace:     in.Namespace,
		ContainerName: in.ContainerName,
		Labels:        in.Labels,
		FromSource:    in.FromSource,
		Duration:      in.Duration,
		Type:          in.Type,
		Rule:          in.Rule,
	})
	return &resp, err
}

// =================== //
// == Observability == //
// =================== //
type observabilityServer struct {
	opb.ObservabilityServer
}

//Service to fetch summary data
func (s *observabilityServer) Summary(ctx context.Context, in *opb.Request) (*opb.Response, error) {
	resp, err := obs.GetSummaryData(in)
	return resp, err
}

func (s *observabilityServer) GetPodNames(ctx context.Context, in *opb.Request) (*opb.PodNameResponse, error) {
	resp, err := obs.GetPodNames(in)
	return &resp, err
}

// ================= //
// == gRPC server == //
// ================= //

func GetNewServer() *grpc.Server {
	s := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(s, health.NewServer())

	reflection.Register(s)

	// create server instances
	workerServer := &workerServer{}
	consumerServer := &consumerServer{}
	analyzerServer := &analyzerServer{}
	insightServer := &insightServer{}
	observabilityServer := &observabilityServer{}
	discoveryServer := &discoveryServer{}

	// register gRPC servers
	wpb.RegisterWorkerServer(s, workerServer)
	fpb.RegisterConsumerServer(s, consumerServer)
	apb.RegisterAnalyzerServer(s, analyzerServer)
	ipb.RegisterInsightServer(s, insightServer)
	opb.RegisterObservabilityServer(s, observabilityServer)
	dpb.RegisterDiscoveryServer(s, discoveryServer)

	if cfg.GetCurrentCfg().ConfigClusterMgmt.ClusterInfoFrom != "k8sclient" {
		// start consumer automatically
		fc.ConsumerMutex.Lock()
		fc.StartConsumer()
		fc.ConsumerMutex.Unlock()
	}

	// start net worker automatically
	network.StartNetworkWorker()

	// start sys worker automatically
	system.StartSystemWorker()

	// start observability
	obs.InitObservability()

	return s
}
