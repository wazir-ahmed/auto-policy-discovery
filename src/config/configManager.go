package config

import (
	types "github.com/accuknox/auto-policy-discovery/src/types"
	"github.com/spf13/viper"
)

// operation mode: 		 cronjob: 1
//                 		 onetime job: 2

// network policy types: egress only   : 1
//                       ingress only  : 2
//                       all           : 3

// network rule types:   matchLabels: 1
//                       toPorts    : 2
//                       toHTTPs    : 4
//                       toCIDRs    : 8
//                       toEntities : 16
//                       toServices : 32
//                       toFQDNs    : 64
//                       fromCIDRs  : 128
//                       fromEntities : 256
//                       all        : 511

// system policy types: process     : 1
//                      file        : 2
//                      network     : 4
//                      all		    : 7

// ====================== //
// == Global Variables == //
// ====================== //

var CurrentCfg types.Configuration

var NetworkPlugIn string
var IgnoringNetworkNamespaces []string
var HTTPUrlThreshold int

func init() {
	IgnoringNetworkNamespaces = []string{"kube-system"}
	HTTPUrlThreshold = 5
	NetworkPlugIn = "cilium" // for now, cilium only supported
}

// =========================== //
// == Configuration Loading == //
// =========================== //

func LoadConfigDB() types.ConfigDB {
	cfgDB := types.ConfigDB{}

	cfgDB.DBDriver = viper.GetString("database.driver")
	cfgDB.DBUser = viper.GetString("database.user")
	cfgDB.DBPass = viper.GetString("database.password")
	cfgDB.DBName = viper.GetString("database.dbname")

	cfgDB.DBHost = viper.GetString("database.host")
	cfgDB.SQLiteDBPath = viper.GetString("database.sqlite-db-path")
	/*
		fix for #405
		dbAddr, err := net.LookupIP(cfgDB.DBHost)
		if err == nil {
			cfgDB.DBHost = dbAddr[0].String()
		} else {
			cfgDB.DBHost = libs.GetExternalIPAddr()
		}
	*/
	cfgDB.DBPort = viper.GetString("database.port")

	return cfgDB
}

func LoadConfigCiliumHubble() types.ConfigCiliumHubble {
	cfgHubble := types.ConfigCiliumHubble{}

	cfgHubble.HubbleURL = viper.GetString("cilium-hubble.url")
	/*
		commented for fixing #405
		addr, err := net.LookupIP(cfgHubble.HubbleURL)
		if err == nil {
			cfgHubble.HubbleURL = addr[0].String()
		} else {
			cfgHubble.HubbleURL = libs.GetExternalIPAddr()
		}
	*/

	cfgHubble.HubblePort = viper.GetString("cilium-hubble.port")

	return cfgHubble
}

func LoadConfigKubeArmor() types.ConfigKubeArmorRelay {
	cfgKubeArmor := types.ConfigKubeArmorRelay{}

	cfgKubeArmor.KubeArmorRelayURL = viper.GetString("kubearmor.url")
	/*
		addr, err := net.LookupIP(cfgKubeArmor.KubeArmorRelayURL)
		if err == nil {
			cfgKubeArmor.KubeArmorRelayURL = addr[0].String()
		} else {
			cfgKubeArmor.KubeArmorRelayURL = libs.GetExternalIPAddr()
		}
	*/

	cfgKubeArmor.KubeArmorRelayPort = viper.GetString("kubearmor.port")

	return cfgKubeArmor
}

func LoadConfigFromFile() {
	CurrentCfg = types.Configuration{}

	// default
	CurrentCfg.ConfigName = "default"

	CurrentCfg.Status = 1 // 1: active 0: inactive

	// Observability module
	CurrentCfg.Observability = viper.GetBool("observability")

	// load network policy discovery
	CurrentCfg.ConfigNetPolicy = types.ConfigNetworkPolicy{
		OperationMode:           viper.GetInt("application.network.operation-mode"),
		OperationTrigger:        viper.GetInt("application.network.operation-trigger"),
		CronJobTimeInterval:     "@every " + viper.GetString("application.network.cron-job-time-interval"),
		OneTimeJobTimeSelection: "", // e.g., 2021-01-20 07:00:23|2021-01-20 07:00:25

		NetworkLogLimit:  viper.GetInt("application.network.network-log-limit"),
		NetworkLogFrom:   viper.GetString("application.network.network-log-from"),
		NetworkLogFile:   viper.GetString("application.network.network-log-file"),
		NetworkPolicyTo:  viper.GetString("application.network.network-policy-to"),
		NetworkPolicyDir: viper.GetString("application.network.network-policy-dir"),

		NetPolicyTypes:     3,
		NetPolicyRuleTypes: 1023,
		NetPolicyCIDRBits:  32,

		NetLogFilters: []types.NetworkLogFilter{},

		NetPolicyL3Level: 1,
		NetPolicyL4Level: 1,
		NetPolicyL7Level: 1,

		NetSkipCertVerification: viper.GetBool("application.network.skip-cert-verification"),

		DebugFlowCluster: viper.GetString("application.network.debug-flow-cluster"),
		DebugFlowLabels:  viper.GetStringSlice("application.network.debug-flow-labels"),
	}

	var ns, notNs []string
	namespaces := viper.GetStringSlice("application.network.namespace-filter")
	for _, n := range namespaces {
		if n[0] == '!' {
			notNs = append(notNs, n[1:])
		} else {
			ns = append(ns, n)
		}
	}
	CurrentCfg.ConfigNetPolicy.NsFilter = ns
	CurrentCfg.ConfigNetPolicy.NsNotFilter = notNs

	// load system policy discovery
	CurrentCfg.ConfigSysPolicy = types.ConfigSystemPolicy{
		OperationMode:           viper.GetInt("application.system.operation-mode"),
		OperationTrigger:        viper.GetInt("application.system.operation-trigger"),
		CronJobTimeInterval:     "@every " + viper.GetString("application.system.cron-job-time-interval"),
		OneTimeJobTimeSelection: "", // e.g., 2021-01-20 07:00:23|2021-01-20 07:00:25

		SystemLogLimit:   viper.GetInt("application.system.system-log-limit"),
		SystemLogFrom:    viper.GetString("application.system.system-log-from"),
		SystemLogFile:    viper.GetString("application.system.system-log-file"),
		SystemPolicyTo:   viper.GetString("application.system.system-policy-to"),
		SystemPolicyDir:  viper.GetString("application.system.system-policy-dir"),
		SysPolicyTypes:   viper.GetInt("application.system.system-policy-types"),
		DeprecateOldMode: viper.GetBool("application.system.deprecate-old-mode"),

		SystemLogFilters: []types.SystemLogFilter{},

		ProcessFromSource: true,
		FileFromSource:    true,
	}

	// load cluster resource info
	CurrentCfg.ConfigClusterMgmt = types.ConfigClusterMgmt{
		ClusterInfoFrom: viper.GetString("application.cluster.cluster-info-from"),
		ClusterMgmtURL:  viper.GetString("application.cluster.cluster-mgmt-url"),
	}

	// load database
	CurrentCfg.ConfigDB = LoadConfigDB()

	// load cilium hubble relay
	CurrentCfg.ConfigCiliumHubble = LoadConfigCiliumHubble()

	// load kubearmor relay config
	CurrentCfg.ConfigKubeArmorRelay = LoadConfigKubeArmor()
}

// ============================ //
// == Set Configuration Info == //
// ============================ //

func SetLogFile(file string) {
	CurrentCfg.ConfigNetPolicy.NetworkLogFile = file
}

// ============================ //
// == Get Configuration Info == //
// ============================ //

func GetCurrentCfg() types.Configuration {
	return CurrentCfg
}

func IsObservabilityEnabled() bool {
	return CurrentCfg.Observability
}

func GetCfgDB() types.ConfigDB {
	return CurrentCfg.ConfigDB
}

// ============================= //
// == Get Network Config Info == //
// ============================= //

func GetCfgNet() types.ConfigNetworkPolicy {
	return CurrentCfg.ConfigNetPolicy
}

func GetCfgNetOperationMode() int {
	return CurrentCfg.ConfigNetPolicy.OperationMode
}

func GetCfgNetCronJobTime() string {
	return CurrentCfg.ConfigNetPolicy.CronJobTimeInterval
}

func GetCfgNetOneTime() string {
	return CurrentCfg.ConfigNetPolicy.OneTimeJobTimeSelection
}

func GetCfgNetOperationTrigger() int {
	return CurrentCfg.ConfigNetPolicy.OperationTrigger
}

// == //

func GetCfgNetLimit() int {
	return CurrentCfg.ConfigNetPolicy.NetworkLogLimit
}

func GetCfgNetworkLogFrom() string {
	return CurrentCfg.ConfigNetPolicy.NetworkLogFrom
}

func GetCfgNetworkLogFile() string {
	return CurrentCfg.ConfigNetPolicy.NetworkLogFile
}

func GetCfgCiliumHubble() types.ConfigCiliumHubble {
	return CurrentCfg.ConfigCiliumHubble
}

func GetCfgKubeArmor() types.ConfigKubeArmorRelay {
	return CurrentCfg.ConfigKubeArmorRelay
}

func GetCfgNetworkPolicyTo() string {
	return CurrentCfg.ConfigNetPolicy.NetworkPolicyTo
}

func GetCfgCIDRBits() int {
	return CurrentCfg.ConfigNetPolicy.NetPolicyCIDRBits
}

func GetCfgNetworkPolicyTypes() int {
	return CurrentCfg.ConfigNetPolicy.NetPolicyTypes
}

func GetCfgNetworkRuleTypes() int {
	return CurrentCfg.ConfigNetPolicy.NetPolicyRuleTypes
}

func GetCfgNetworkL3Level() int {
	return CurrentCfg.ConfigNetPolicy.NetPolicyL3Level
}

func GetCfgNetworkL4Level() int {
	return CurrentCfg.ConfigNetPolicy.NetPolicyL4Level
}

func GetCfgNetworkL7Level() int {
	return CurrentCfg.ConfigNetPolicy.NetPolicyL7Level
}

func GetCfgNetworkHTTPThreshold() int {
	return HTTPUrlThreshold
}

func GetCfgNetworkSkipNamespaces() []string {
	return IgnoringNetworkNamespaces
}

func GetCfgNetworkLogFilters() []types.NetworkLogFilter {
	return CurrentCfg.ConfigNetPolicy.NetLogFilters
}

func GetCfgNetworkSkipCertVerification() bool {
	return CurrentCfg.ConfigNetPolicy.NetSkipCertVerification
}

// ============================ //
// == Get System Config Info == //
// ============================ //

func GetCfgSys() types.ConfigSystemPolicy {
	return CurrentCfg.ConfigSysPolicy
}

func GetCfgSysOperationMode() int {
	return CurrentCfg.ConfigSysPolicy.OperationMode
}

func GetCfgSysOperationTrigger() int {
	return CurrentCfg.ConfigSysPolicy.OperationTrigger
}

func GetCfgSysCronJobTime() string {
	return CurrentCfg.ConfigSysPolicy.CronJobTimeInterval
}

func GetCfgSysOneTime() string {
	return CurrentCfg.ConfigSysPolicy.OneTimeJobTimeSelection
}

// == //

func GetCfgSysLimit() int {
	return CurrentCfg.ConfigSysPolicy.SystemLogLimit
}

func GetCfgSystemLogFrom() string {
	return CurrentCfg.ConfigSysPolicy.SystemLogFrom
}

func GetCfgSystemLogFile() string {
	return CurrentCfg.ConfigSysPolicy.SystemLogFile
}

func GetCfgSystemPolicyTo() string {
	return CurrentCfg.ConfigSysPolicy.SystemPolicyTo
}

func GetCfgSystemPolicyDir() string {
	return CurrentCfg.ConfigSysPolicy.SystemPolicyDir
}

func GetCfgSystemkPolicyTypes() int {
	return CurrentCfg.ConfigSysPolicy.SysPolicyTypes
}

func GetCfgSystemLogFilters() []types.SystemLogFilter {
	return CurrentCfg.ConfigSysPolicy.SystemLogFilters
}

func GetCfgSystemProcFromSource() bool {
	return CurrentCfg.ConfigSysPolicy.ProcessFromSource
}

func GetCfgSystemFileFromSource() bool {
	return CurrentCfg.ConfigSysPolicy.FileFromSource
}

// ============================= //
// == Get Cluster Config Info == //
// ============================= //

func GetCfgClusterInfoFrom() string {
	return CurrentCfg.ConfigClusterMgmt.ClusterInfoFrom
}

func GetCfgClusterMgmtURL() string {
	return CurrentCfg.ConfigClusterMgmt.ClusterMgmtURL
}
