package types

type ConfigDB struct {
	DBDriver     string `json:"db_driver,omitempty" bson:"db_driver,omitempty"`
	DBHost       string `json:"db_host,omitempty" bson:"db_host,omitempty"`
	DBPort       string `json:"db_port,omitempty" bson:"db_port,omitempty"`
	DBUser       string `json:"db_user,omitempty" bson:"db_user,omitempty"`
	DBPass       string `json:"db_pass,omitempty" bson:"db_pass,omitempty"`
	DBName       string `json:"db_name,omitempty" bson:"db_name,omitempty"`
	SQLiteDBPath string `json:"sqlite_db_path,omitempty" bson:"sqlite_db_path,omitempty"`
}

type ConfigCiliumHubble struct {
	HubbleURL  string `json:"hubble_url,omitempty" bson:"hubble_url,omitempty"`
	HubblePort string `json:"hubble_port,omitempty" bson:"hubble_port,omitempty"`
}

type ConfigKubeArmorRelay struct {
	KubeArmorRelayURL  string `json:"kubearmor_url,omitempty" bson:"kubearmor_url,omitempty"`
	KubeArmorRelayPort string `json:"kubearmor_port,omitempty" bson:"kubearmor_port,omitempty"`
}

type NetworkLogFilter struct {
	SourceNamespace      string   `json:"source_namespace,omitempty" bson:"source_namespace,omitempty"`
	SourceLabels         []string `json:"source_labels,omitempty" bson:"source_labels,omitempty"`
	DestinationNamespace string   `json:"destination_namespace,omitempty" bson:"destination_namespace,omitempty"`
	DestinationLabels    []string `json:"destination_labels,omitempty" bson:"destination_labels,omitempty"`
	Protocol             string   `json:"protocol,omitempty" bson:"protocol,omitempty"`
	PortNumber           string   `json:"port_number,omitempty" bson:"port_number,omitempty"`
}

type ConfigNetworkPolicy struct {
	OperationMode           int `json:"operation_mode,omitempty" bson:"operation_mode,omitempty"`
	OperationTrigger        int
	CronJobTimeInterval     string `json:"cronjob_time_interval,omitempty" bson:"cronjob_time_interval,omitempty"`
	OneTimeJobTimeSelection string `json:"one_time_job_time_selection,omitempty" bson:"one_time_job_time_selection,omitempty"`

	NetworkLogLimit  int
	NetworkLogFrom   string `json:"network_log_from,omitempty" bson:"network_log_from,omitempty"`
	NetworkLogFile   string `json:"network_log_file,omitempty" bson:"network_log_file,omitempty"`
	NetworkPolicyTo  string `json:"network_policy_to,omitempty" bson:"network_policy_to,omitempty"`
	NetworkPolicyDir string `json:"network_policy_dir,omitempty" bson:"network_policy_dir,omitempty"`

	NsFilter    []string `json:"network_policy_ns_filter,omitempty" bson:"network_policy_ns_filter,omitempty"`
	NsNotFilter []string `json:"network_policy_ns_not_filter,omitempty" bson:"network_policy_ns_not_filter,omitempty"`

	NetPolicyTypes     int `json:"network_policy_types,omitempty" bson:"network_policy_types,omitempty"`
	NetPolicyRuleTypes int `json:"network_policy_rule_types,omitempty" bson:"network_policy_rule_types,omitempty"`
	NetPolicyCIDRBits  int `json:"network_policy_cidrbits,omitempty" bson:"network_policy_cidrbits,omitempty"`

	NetLogFilters []NetworkLogFilter `json:"network_policy_log_filters,omitempty" bson:"network_policy_log_filters,omitempty"`

	NetPolicyL3Level int `json:"network_policy_l3_level,omitempty" bson:"network_policy_l3_level,omitempty"`
	NetPolicyL4Level int `json:"network_policy_l4_level,omitempty" bson:"network_policy_l4_level,omitempty"`
	NetPolicyL7Level int `json:"network_policy_l7_level,omitempty" bson:"network_policy_l7_level,omitempty"`

	NetSkipCertVerification bool `json:"skip_cert_verification,omitempty" bson:"skip_cert_verification,omitempty"`

	DebugFlowCluster string   `json:"debug_flow_cluster,omitempty" bson:"debug_flow_cluster,omitempty"`
	DebugFlowLabels  []string `json:"debug_flow_labels,omitempty" bson:"debug_flow_labels,omitempty"`
}

type SystemLogFilter struct {
	Namespace      string   `json:"namespace,omitempty" bson:"namespace,omitempty"`
	Labels         []string `json:"labels,omitempty" bson:"labels,omitempty"`
	FileFormats    []string `json:"file_formats,omitempty" bson:"file_formats,omitempty"`
	ProcessFormats []string `json:"process_formats,omitempty" bson:"process_formats,omitempty"`
	FileDirs       []string `json:"file_dirs,omitempty" bson:"file_dirs,omitempty"`
	ProcessDirs    []string `json:"process_dirs,omitempty" bson:"process_dirs,omitempty"`
}

type ConfigSystemPolicy struct {
	OperationMode           int `json:"operation_mode,omitempty" bson:"operation_mode,omitempty"`
	OperationTrigger        int
	CronJobTimeInterval     string `json:"cronjob_time_interval,omitempty" bson:"cronjob_time_interval,omitempty"`
	OneTimeJobTimeSelection string `json:"one_time_job_time_selection,omitempty" bson:"one_time_job_time_selection,omitempty"`

	SystemLogLimit  int
	SystemLogFrom   string `json:"system_log_from,omitempty" bson:"system_log_from,omitempty"`
	SystemLogFile   string `json:"system_log_file,omitempty" bson:"system_log_file,omitempty"`
	SystemPolicyTo  string `json:"system_policy_to,omitempty" bson:"system_policy_to,omitempty"`
	SystemPolicyDir string `json:"system_policy_dir,omitempty" bson:"system_policy_dir,omitempty"`

	SysPolicyTypes   int  `json:"system_policy_types,omitempty" bson:"system_policy_types,omitempty"`
	DeprecateOldMode bool `json:"deprecate_old_mode,omitempty" bson:"deprecate_old_mode,omitempty"`

	SystemLogFilters []SystemLogFilter `json:"system_policy_log_filters,omitempty" bson:"system_policy_log_filters,omitempty"`

	ProcessFromSource bool `json:"system_policy_proc_fromsource,omitempty" bson:"system_policy_proc_fromsource,omitempty"`
	FileFromSource    bool `json:"system_policy_file_fromsource,omitempty" bson:"system_policy_file_fromsource,omitempty"`
}

type ConfigClusterMgmt struct {
	ClusterInfoFrom string `json:"cluster_info_from,omitempty" bson:"cluster_info_from,omitempty"`
	ClusterMgmtURL  string `json:"cluster_mgmt_url,omitempty" bson:"cluster_mgmt_url,omitempty"`
}

type Configuration struct {
	ConfigName string `json:"config_name,omitempty" bson:"config_name,omitempty"`
	Status     int    `json:"status,omitempty" bson:"status,omitempty"`

	Observability bool `json:"observability,omitempty" bson:"observability,omitempty"`

	ConfigDB             ConfigDB             `json:"config_db,omitempty" bson:"config_db,omitempty"`
	ConfigCiliumHubble   ConfigCiliumHubble   `json:"config_cilium_hubble,omitempty" bson:"config_cilium_hubble,omitempty"`
	ConfigKubeArmorRelay ConfigKubeArmorRelay `json:"config_kubearmor_relay,omitempty" bson:"config_kubearmor_relay,omitempty"`

	ConfigNetPolicy   ConfigNetworkPolicy `json:"config_network_policy,omitempty" bson:"config_network_policy,omitempty"`
	ConfigSysPolicy   ConfigSystemPolicy  `json:"config_system_policy,omitempty" bson:"config_system_policy,omitempty"`
	ConfigClusterMgmt ConfigClusterMgmt   `json:"config_cluster_mgmt,omitempty" bson:"config_cluster_mgmt,omitempty"`
}
