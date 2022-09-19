package libs

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/accuknox/auto-policy-discovery/src/types"

	_ "github.com/go-sql-driver/mysql"
)

const WorkloadProcessFileSet_TableName = "workload_process_fileset"
const TableNetworkPolicy_TableName = "network_policy"
const TableSystemPolicy_TableName = "system_policy"
const TableSystemLogs_TableName = "system_logs"
const TableNetworkLogs_TableName = "network_logs"
const PolicyYamlTable_TableName = "policy_yaml"

// ================ //
// == Connection == //
// ================ //

var MockSql sqlmock.Sqlmock = nil
var MockDB *sql.DB = nil

func NewMock() (*sql.DB, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	if err != nil {
		log.Error().Msgf("an error '%s' was not expected when opening a stub database connection", err)
	}

	MockSql = mock
	MockDB = db

	return db, mock
}

func waitForDB(db *sql.DB) {
	for {
		err := db.Ping()
		if err != nil {
			time.Sleep(time.Second * 1)
			log.Error().Msgf("db.Ping() failed. Will retry. err=%s", err.Error())
		} else {
			break
		}
	}
}

func connectMySQL(cfg types.ConfigDB) (db *sql.DB) {
	if MockDB != nil {
		return MockDB
	}

	dbconn := cfg.DBUser + ":" + cfg.DBPass + "@tcp(" + cfg.DBHost + ":" + cfg.DBPort + ")/" + cfg.DBName
	db, err := sql.Open(cfg.DBDriver, dbconn)
	for err != nil {
		log.Error().Msgf("mysql driver:%s, user:%s, host:%s, port:%s, dbname:%s conn-error:%s",
			cfg.DBDriver, cfg.DBUser, cfg.DBHost, cfg.DBPort, cfg.DBName, err.Error())
		time.Sleep(time.Second * 1)
		db, err = sql.Open(cfg.DBDriver, dbconn)
	}

	db.SetMaxIdleConns(0)

	waitForDB(db)

	return db
}

// ==================== //
// == Network Policy == //
// ==================== //

func GetNetworkPoliciesFromMySQL(cfg types.ConfigDB, cluster, namespace, status, nwtype, rule string) ([]types.KnoxNetworkPolicy, error) {
	db := connectMySQL(cfg)
	defer db.Close()

	policies := []types.KnoxNetworkPolicy{}
	var results *sql.Rows
	var err error

	query := "SELECT apiVersion,kind,flow_ids,name,cluster_name,namespace,type,rule,status,outdated,spec,generatedTime,updatedTime FROM " + TableNetworkPolicy_TableName

	var whereClause string
	var args []interface{}

	if cluster != "" {
		concatWhereClause(&whereClause, "cluster_name")
		args = append(args, cluster)
	}
	if namespace != "" {
		concatWhereClause(&whereClause, "namespace")
		args = append(args, namespace)
	}
	if status != "" {
		concatWhereClause(&whereClause, "status")
		args = append(args, status)
	}
	if nwtype != "" {
		concatWhereClause(&whereClause, "type")
		args = append(args, nwtype)
	}
	if rule != "" {
		concatWhereClause(&whereClause, "rule")
		args = append(args, rule)
	}

	results, err = db.Query(query+whereClause, args...)
	defer results.Close()

	if err != nil {
		log.Error().Msg(err.Error())
		return nil, err
	}

	for results.Next() {
		policy := types.KnoxNetworkPolicy{}

		var name, clusterName, namespace, policyType, rule, status string
		specByte := []byte{}
		spec := types.Spec{}

		flowIDsByte := []byte{}
		flowIDs := []int{}

		if err := results.Scan(
			&policy.APIVersion,
			&policy.Kind,
			&flowIDsByte,
			&name,
			&clusterName,
			&namespace,
			&policyType,
			&rule,
			&status,
			&policy.Outdated,
			&specByte,
			&policy.GeneratedTime,
			&policy.UpdatedTime,
		); err != nil {
			return nil, err
		}

		if err := json.Unmarshal(specByte, &spec); err != nil {
			return nil, err
		}

		if err := json.Unmarshal(flowIDsByte, &flowIDs); err != nil {
			return nil, err
		}

		policy.Metadata = map[string]string{
			"name":         name,
			"cluster_name": clusterName,
			"namespace":    namespace,
			"type":         policyType,
			"rule":         rule,
			"status":       status,
		}

		policy.FlowIDs = flowIDs
		policy.Spec = spec

		policies = append(policies, policy)
	}

	return policies, nil
}

func UpdateNetworkPolicyToMySQL(cfg types.ConfigDB, policy types.KnoxNetworkPolicy) error {
	db := connectMySQL(cfg)
	defer db.Close()

	// set status -> outdated
	stmt, err := db.Prepare("UPDATE " + TableNetworkPolicy_TableName +
		" SET apiVersion=?,kind=?,cluster_name=?,namespace=?,type=?,status=?,outdated=?,spec=?,updatedTime=? WHERE name = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	specPointer := &policy.Spec
	spec, err := json.Marshal(specPointer)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(
		policy.APIVersion,
		policy.Kind,
		policy.Metadata["cluster_name"],
		policy.Metadata["namespace"],
		policy.Metadata["type"],
		policy.Metadata["status"],
		policy.Outdated,
		spec,
		ConvertStrToUnixTime("now"),
		policy.Metadata["name"])
	if err != nil {
		return err
	}

	return nil
}

func UpdateOutdatedNetworkPolicyFromMySQL(cfg types.ConfigDB, outdatedPolicy string, latestPolicy string) error {
	db := connectMySQL(cfg)
	defer db.Close()

	var err error

	// set status -> outdated
	stmt1, err := db.Prepare("UPDATE " + TableNetworkPolicy_TableName + " SET status=? WHERE name=?")
	if err != nil {
		return err
	}
	defer stmt1.Close()

	_, err = stmt1.Exec("outdated", outdatedPolicy)
	if err != nil {
		return err
	}

	// set outdated -> latest' name
	stmt2, err := db.Prepare("UPDATE " + TableNetworkPolicy_TableName + " SET outdated=? WHERE name=?")
	if err != nil {
		return err
	}
	defer stmt2.Close()

	_, err = stmt2.Exec(latestPolicy, outdatedPolicy)
	if err != nil {
		return err
	}

	return nil
}

func insertNetworkPolicy(cfg types.ConfigDB, db *sql.DB, policy types.KnoxNetworkPolicy) error {
	stmt, err := db.Prepare("INSERT INTO " + TableNetworkPolicy_TableName + "(apiVersion,kind,flow_ids,name,cluster_name,namespace,type,rule,status,outdated,spec,generatedTime,updatedTime) values(?,?,?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	flowIDsPointer := &policy.FlowIDs
	flowids, err := json.Marshal(flowIDsPointer)
	if err != nil {
		return err
	}

	specPointer := &policy.Spec
	spec, err := json.Marshal(specPointer)
	if err != nil {
		return err
	}

	currTime := ConvertStrToUnixTime("now")

	_, err = stmt.Exec(policy.APIVersion,
		policy.Kind,
		flowids,
		policy.Metadata["name"],
		policy.Metadata["cluster_name"],
		policy.Metadata["namespace"],
		policy.Metadata["type"],
		policy.Metadata["rule"],
		policy.Metadata["status"],
		policy.Outdated,
		spec,
		currTime,
		currTime)
	if err != nil {
		return err
	}

	return nil
}

func InsertNetworkPoliciesToMySQL(cfg types.ConfigDB, policies []types.KnoxNetworkPolicy) error {
	db := connectMySQL(cfg)
	defer db.Close()

	for _, policy := range policies {
		if err := insertNetworkPolicy(cfg, db, policy); err != nil {
			return err
		}
	}

	return nil
}

// =================== //
// == System Policy == //
// =================== //

func UpdateOutdatedSystemPolicyFromMySQL(cfg types.ConfigDB, outdatedPolicy string, latestPolicy string) error {
	db := connectMySQL(cfg)
	defer db.Close()

	var err error

	// set status -> outdated
	stmt1, err := db.Prepare("UPDATE " + TableSystemPolicy_TableName + " SET status=? WHERE name=?")
	if err != nil {
		return err
	}
	defer stmt1.Close()

	_, err = stmt1.Exec("outdated", outdatedPolicy)
	if err != nil {
		return err
	}

	// set outdated -> latest' name
	stmt2, err := db.Prepare("UPDATE " + TableNetworkPolicy_TableName + " SET outdated=? WHERE name=?")
	if err != nil {
		return err
	}
	defer stmt2.Close()

	_, err = stmt2.Exec(latestPolicy, outdatedPolicy)
	if err != nil {
		return err
	}

	return nil
}

func GetSystemPoliciesFromMySQL(cfg types.ConfigDB, namespace, status string) ([]types.KnoxSystemPolicy, error) {
	db := connectMySQL(cfg)
	defer db.Close()

	policies := []types.KnoxSystemPolicy{}
	var results *sql.Rows
	var err error

	query := "SELECT apiVersion,kind,name,clusterName,namespace,type,status,outdated,spec,generatedTime,updatedTime,latest FROM " + TableSystemPolicy_TableName

	if namespace != "" && status != "" {
		query = query + " WHERE namespace = ? and status = ? "
		results, err = db.Query(query, namespace, status)
	} else if namespace != "" {
		query = query + " WHERE namespace = ? "
		results, err = db.Query(query, namespace)
	} else if status != "" {
		query = query + " WHERE status = ? "
		results, err = db.Query(query, status)
	} else {
		results, err = db.Query(query)
	}

	if err != nil {
		log.Error().Msg(err.Error())
		return nil, err
	}

	defer results.Close()

	for results.Next() {
		policy := types.KnoxSystemPolicy{}

		var name, clusterName, namespace, policyType, status string
		specByte := []byte{}
		spec := types.KnoxSystemSpec{}

		if err := results.Scan(
			&policy.APIVersion,
			&policy.Kind,
			&name,
			&clusterName,
			&namespace,
			&policyType,
			&status,
			&policy.Outdated,
			&specByte,
			&policy.GeneratedTime,
			&policy.UpdatedTime,
			&policy.Latest,
		); err != nil {
			return nil, err
		}

		if err := json.Unmarshal(specByte, &spec); err != nil {
			return nil, err
		}

		policy.Metadata = map[string]string{
			"name":        name,
			"clusterName": clusterName,
			"namespace":   namespace,
			"type":        policyType,
			"status":      status,
		}

		policy.Spec = spec

		policies = append(policies, policy)
	}

	return policies, nil
}

func insertSystemPolicy(cfg types.ConfigDB, db *sql.DB, policy types.KnoxSystemPolicy) error {
	stmt, err := db.Prepare("INSERT INTO " + TableSystemPolicy_TableName + "(apiVersion,kind,name,clusterName,namespace,type,status,outdated,spec,generatedTime,updatedTime,latest) values(?,?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	specPointer := &policy.Spec
	spec, err := json.Marshal(specPointer)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(
		policy.APIVersion,
		policy.Kind,
		policy.Metadata["name"],
		policy.Metadata["clusterName"],
		policy.Metadata["namespace"],
		policy.Metadata["type"],
		policy.Metadata["status"],
		policy.Outdated,
		spec,
		ConvertStrToUnixTime("now"),
		ConvertStrToUnixTime("now"),
		true)
	if err != nil {
		return err
	}

	return nil
}

func InsertSystemPoliciesToMySQL(cfg types.ConfigDB, policies []types.KnoxSystemPolicy) error {
	db := connectMySQL(cfg)
	defer db.Close()

	for _, policy := range policies {
		if err := insertSystemPolicy(cfg, db, policy); err != nil {
			return err
		}
	}

	return nil
}

func UpdateSystemPolicyToMySQL(cfg types.ConfigDB, policy types.KnoxSystemPolicy) error {
	db := connectMySQL(cfg)
	defer db.Close()

	// set status -> outdated
	stmt, err := db.Prepare("UPDATE " + TableSystemPolicy_TableName +
		" SET apiVersion=?,kind=?,clusterName=?,namespace=?,type=?,status=?,outdated=?,spec=?,updatedTime=?,latest=? WHERE name = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	specPointer := &policy.Spec
	spec, err := json.Marshal(specPointer)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(
		policy.APIVersion,
		policy.Kind,
		policy.Metadata["clusterName"],
		policy.Metadata["namespace"],
		policy.Metadata["type"],
		policy.Metadata["status"],
		policy.Outdated,
		spec,
		ConvertStrToUnixTime("now"),
		true,
		policy.Metadata["name"])
	if err != nil {
		return err
	}

	return nil
}

// =========== //
// == Table == //
// =========== //

func ClearNetworkDBTableMySQL(cfg types.ConfigDB) error {
	db := connectMySQL(cfg)
	defer db.Close()

	query := "DELETE FROM " + TableNetworkPolicy_TableName
	if _, err := db.Query(query); err != nil {
		return err
	}

	return nil
}

func ClearDBTablesMySQL(cfg types.ConfigDB) error {
	db := connectMySQL(cfg)
	defer db.Close()

	query := "DELETE FROM " + TableNetworkPolicy_TableName
	if _, err := db.Query(query); err != nil {
		return err
	}

	query = "DELETE FROM " + TableSystemPolicy_TableName
	if _, err := db.Query(query); err != nil {
		return err
	}

	query = "DELETE FROM " + WorkloadProcessFileSet_TableName
	if _, err := db.Query(query); err != nil {
		return err
	}

	return nil
}

func CreateTableNetworkPolicyMySQL(cfg types.ConfigDB) error {
	db := connectMySQL(cfg)
	defer db.Close()

	tableName := TableNetworkPolicy_TableName

	query :=
		"CREATE TABLE IF NOT EXISTS `" + tableName + "` (" +
			"	`id` int NOT NULL AUTO_INCREMENT," +
			"	`apiVersion` varchar(20) DEFAULT NULL," +
			"	`kind` varchar(20) DEFAULT NULL," +
			"	`flow_ids` JSON DEFAULT NULL," +
			"	`name` varchar(50) DEFAULT NULL," +
			"	`cluster_name` varchar(50) DEFAULT NULL," +
			"	`namespace` varchar(50) DEFAULT NULL," +
			"	`type` varchar(10) DEFAULT NULL," +
			"	`rule` varchar(30) DEFAULT NULL," +
			"	`status` varchar(10) DEFAULT NULL," +
			"	`outdated` varchar(50) DEFAULT NULL," +
			"	`spec` JSON DEFAULT NULL," +
			"	`generatedTime` bigint NOT NULL," +
			"	`updatedTime` bigint NOT NULL," +
			"	PRIMARY KEY (`id`)" +
			"  );"

	if _, err := db.Query(query); err != nil {
		return err
	}

	return nil
}

func CreateTableSystemPolicyMySQL(cfg types.ConfigDB) error {
	db := connectMySQL(cfg)
	defer db.Close()

	tableName := TableSystemPolicy_TableName

	query :=
		"CREATE TABLE IF NOT EXISTS `" + tableName + "` (" +
			"	`id` int NOT NULL AUTO_INCREMENT," +
			"	`apiVersion` varchar(40) DEFAULT NULL," +
			"	`kind` varchar(20) DEFAULT NULL," +
			"	`name` varchar(128) DEFAULT NULL," +
			"	`clusterName` varchar(50) DEFAULT NULL," +
			"	`namespace` varchar(50) DEFAULT NULL," +
			"   `type` varchar(20) NOT NULL," +
			"	`status` varchar(10) DEFAULT NULL," +
			"	`outdated` varchar(50) DEFAULT NULL," +
			"	`spec` JSON DEFAULT NULL," +
			"	`generatedTime` bigint NOT NULL," +
			"	`updatedTime` bigint NOT NULL," +
			"	`latest` BOOLEAN," +
			"	PRIMARY KEY (`id`)" +
			"  );"

	if _, err := db.Query(query); err != nil {
		return err
	}

	return nil
}

func CreateTableWorkLoadProcessFileSetMySQL(cfg types.ConfigDB) error {
	db := connectMySQL(cfg)
	defer db.Close()

	tableName := WorkloadProcessFileSet_TableName

	query :=
		"CREATE TABLE IF NOT EXISTS `" + tableName + "` (" +
			"	`id` int NOT NULL AUTO_INCREMENT," +
			"	`policyName` varchar(128) DEFAULT NULL," +
			"	`clusterName` varchar(50) DEFAULT NULL," +
			"	`namespace` varchar(50) DEFAULT NULL," +
			"   `containerName` varchar(100) NOT NULL," +
			"	`labels` varchar(1000) DEFAULT NULL," +
			"	`fromSource` varchar(256) DEFAULT NULL," +
			"	`settype` varchar(16) DEFAULT NULL," + // settype: "file" or "process"
			"	`fileset` text DEFAULT NULL," +
			"	`createdTime` bigint NOT NULL," +
			"	`updatedTime` bigint NOT NULL," +
			"	PRIMARY KEY (`id`)" +
			"  );"

	_, err := db.Query(query)
	return err
}

func CreateTableSystemLogsMySQL(cfg types.ConfigDB) error {
	db := connectMySQL(cfg)
	defer db.Close()

	tableName := TableSystemLogs_TableName

	query :=
		"CREATE TABLE IF NOT EXISTS `" + tableName + "` (" +
			"	`id` integer NOT NULL PRIMARY KEY AUTO_INCREMENT," +
			"	`cluster_name` varchar(50) DEFAULT NULL," +
			"	`host_name` varchar(50) DEFAULT NULL," +
			"	`namespace_name` varchar(50) DEFAULT NULL," +
			"	`pod_name` varchar(50) DEFAULT NULL," +
			"	`container_id` varchar(100) DEFAULT NULL," +
			"	`container_name` varchar(100) DEFAULT NULL," +
			"	`uid` INTEGER," +
			"	`type` varchar(50) DEFAULT NULL," +
			"	`source` varchar(250) DEFAULT NULL," +
			"	`operation` varchar(250) DEFAULT NULL," +
			"	`resource` varchar(250) DEFAULT NULL," +
			"	`labels` varchar(250) DEFAULT NULL," +
			"	`data` varchar(250) DEFAULT NULL," +
			"	`category` varchar(50) DEFAULT NULL," +
			"	`action` varchar(50) DEFAULT NULL," +
			"	`start_time` bigint DEFAULT NULL," +
			"	`updated_time` bigint DEFAULT NULL," +
			"	`result` varchar(100) DEFAULT NULL," +
			"	`total` INTEGER	" +
			"  );"

	_, err := db.Query(query)
	return err
}

func CreateTableNetworkLogsMySQL(cfg types.ConfigDB) error {
	db := connectMySQL(cfg)
	defer db.Close()

	tableName := TableNetworkLogs_TableName

	query :=
		"CREATE TABLE IF NOT EXISTS `" + tableName + "` (" +
			"	`id` integer NOT NULL PRIMARY KEY AUTO_INCREMENT," +
			"	`verdict` varchar(100) DEFAULT NULL," +
			"	`ip_source` varchar(100) DEFAULT NULL," +
			"	`ip_destination` varchar(100) DEFAULT NULL," +
			"	`ip_version` varchar(100) DEFAULT NULL," +
			"	`ip_encrypted` BOOLEAN," +
			"	`l4_tcp_source_port` INTEGER," +
			"	`l4_tcp_destination_port` INTEGER," +
			"	`l4_udp_source_port` INTEGER," +
			"	`l4_udp_destination_port` INTEGER," +
			"	`l4_icmpv4_type` INTEGER," +
			"	`l4_icmpv4_code` INTEGER," +
			"	`l4_icmpv6_type` INTEGER," +
			"	`l4_icmpv6_code` INTEGER," +
			"	`source_namespace` varchar(100) DEFAULT NULL," +
			"	`source_labels` varchar(200) DEFAULT NULL," +
			"	`source_pod_name` varchar(100) DEFAULT NULL," +
			"	`destination_namespace` varchar(100) DEFAULT NULL," +
			"	`destination_labels` varchar(200) DEFAULT NULL," +
			"	`destination_pod_name` varchar(100) DEFAULT NULL," +
			"	`type` varchar(100) DEFAULT NULL," +
			"	`node_name` varchar(100) DEFAULT NULL," +
			"	`l7_type` varchar(100) DEFAULT NULL," +
			"	`l7_dns_cnames` varchar(100) DEFAULT NULL," +
			"	`l7_dns_observation_source` varchar(150) DEFAULT NULL," +
			"	`l7_http_code` INTEGER," +
			"	`l7_http_method` varchar(100) DEFAULT NULL," +
			"	`l7_http_url` varchar(200) DEFAULT NULL," +
			"	`l7_http_protocol` varchar(50) DEFAULT NULL," +
			"	`l7_http_headers` varchar(200) DEFAULT NULL," +
			"	`event_type_type` INTEGER," +
			"	`event_type_sub_type` INTEGER," +
			"	`source_service_name` varchar(150) DEFAULT NULL," +
			"	`source_service_namespace` varchar(100) DEFAULT NULL," +
			"	`destination_service_name` varchar(100) DEFAULT NULL," +
			"	`destination_service_namespace` varchar(100) DEFAULT NULL," +
			"	`traffic_direction` varchar(100) DEFAULT NULL," +
			"	`trace_observation_point` varchar(100) DEFAULT NULL," +
			"	`drop_reason_desc` varchar(100) DEFAULT NULL," +
			"	`is_reply` BOOLEAN," +
			"	`start_time` bigint NOT NULL," +
			"	`updated_time` bigint NOT NULL," +
			"	`total` INTEGER" +
			" 	);"

	_, err := db.Query(query)
	return err
}

func CreatePolicyTableMySQL(cfg types.ConfigDB) error {
	db := connectMySQL(cfg)
	defer db.Close()

	tableName := PolicyYamlTable_TableName

	query :=
		"CREATE TABLE IF NOT EXISTS `" + tableName + "` (" +
			"	`id` INTEGER AUTO_INCREMENT," +
			"	`type` varchar(50) DEFAULT NULL," +
			"	`kind` varchar(50) DEFAULT NULL," +
			"	`cluster_name` varchar(50) DEFAULT NULL," +
			"	`namespace` varchar(50) DEFAULT NULL," +
			"	`labels` text DEFAULT NULL," +
			"	`policy_name` varchar(150) DEFAULT NULL," +
			"	`policy_yaml` text DEFAULT NULL," +
			"	`updated_time` bigint NOT NULL," +
			"	PRIMARY KEY (`id`)" +
			"  );"

	_, err := db.Exec(query)
	return err
}

func concatWhereClause(whereClause *string, field string) {
	if *whereClause == "" {
		*whereClause = " WHERE "
	} else {
		*whereClause = *whereClause + " and "
	}
	*whereClause = *whereClause + field + " = ?"
}

func concatWhereClauseIntRange(whereClause *string, field string, start int64, end int64) {
	if *whereClause == "" {
		*whereClause = " WHERE "
	} else {
		*whereClause = *whereClause + " and "
	}
	*whereClause = *whereClause + field + " between " + strconv.Itoa(int(start)) + " and " + strconv.Itoa(int(end))
}

// GetWorkloadProcessFileSetMySQL Handle File Sets in context to a given fromSource
func GetWorkloadProcessFileSetMySQL(cfg types.ConfigDB, wpfs types.WorkloadProcessFileSet) (map[types.WorkloadProcessFileSet][]string, types.PolicyNameMap, error) {
	db := connectMySQL(cfg)
	defer db.Close()

	var results *sql.Rows
	var err error

	query := "SELECT policyName,clusterName,namespace,containerName,labels,fromSource,settype,fileset FROM " + WorkloadProcessFileSet_TableName

	var whereClause string
	var args []interface{}

	if wpfs.ClusterName != "" {
		concatWhereClause(&whereClause, "clusterName")
		args = append(args, wpfs.ClusterName)
	}
	if wpfs.Namespace != "" {
		concatWhereClause(&whereClause, "namespace")
		args = append(args, wpfs.Namespace)
	}
	if wpfs.ContainerName != "" {
		concatWhereClause(&whereClause, "containerName")
		args = append(args, wpfs.ContainerName)
	}
	if wpfs.Labels != "" {
		concatWhereClause(&whereClause, "labels")
		args = append(args, wpfs.Labels)
	}
	if wpfs.FromSource != "" {
		concatWhereClause(&whereClause, "fromSource")
		args = append(args, wpfs.FromSource)
	}
	if wpfs.SetType != "" {
		concatWhereClause(&whereClause, "settype")
		args = append(args, wpfs.SetType)
	}

	results, err = db.Query(query+whereClause, args...)

	if err != nil {
		log.Error().Msg(err.Error())
		return nil, nil, err
	}
	defer results.Close()

	var loc_wpfs types.WorkloadProcessFileSet
	res := types.ResourceSetMap{}
	pnMap := types.PolicyNameMap{}

	for results.Next() {
		var fscsv string
		var fs []string
		var policyName string

		if err := results.Scan(
			&policyName,
			&loc_wpfs.ClusterName,
			&loc_wpfs.Namespace,
			&loc_wpfs.ContainerName,
			&loc_wpfs.Labels,
			&loc_wpfs.FromSource,
			&loc_wpfs.SetType,
			&fscsv,
		); err != nil {
			return nil, nil, err
		}
		fs = strings.Split(fscsv, types.RecordSeparator)
		res[loc_wpfs] = fs
		pnMap[loc_wpfs] = policyName
	}

	return res, pnMap, nil
}

func InsertWorkloadProcessFileSetMySQL(cfg types.ConfigDB, wpfs types.WorkloadProcessFileSet, fs []string) error {
	db := connectMySQL(cfg)
	defer db.Close()
	policyName := "autopol-" + strings.ToLower(wpfs.SetType) + "-" + RandSeq(15)
	time := ConvertStrToUnixTime("now")

	stmt, err := db.Prepare("INSERT INTO " + WorkloadProcessFileSet_TableName +
		"(policyName,clusterName,namespace,containerName,labels,fromSource,settype,fileset,createdtime,updatedtime) values(?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	fsset := strings.Join(fs[:], types.RecordSeparator)

	_, err = stmt.Exec(
		policyName,
		wpfs.ClusterName,
		wpfs.Namespace,
		wpfs.ContainerName,
		wpfs.Labels,
		wpfs.FromSource,
		wpfs.SetType,
		fsset,
		time,
		time)
	return err
}

// Clears out WPFS DB on full or as per options specified
func ClearWPFSDbMySQL(cfg types.ConfigDB, wpfs types.WorkloadProcessFileSet, duration int64) error {
	db := connectMySQL(cfg)
	defer db.Close()

	var err error

	query := "DELETE FROM " + WorkloadProcessFileSet_TableName

	var whereClause string
	var args []interface{}
	time := ConvertStrToUnixTime("now")

	if wpfs.ClusterName != "" {
		concatWhereClause(&whereClause, "clusterName")
		args = append(args, wpfs.ClusterName)
	}
	if wpfs.Namespace != "" {
		concatWhereClause(&whereClause, "namespace")
		args = append(args, wpfs.Namespace)
	}
	if wpfs.ContainerName != "" {
		concatWhereClause(&whereClause, "containerName")
		args = append(args, wpfs.ContainerName)
	}
	if wpfs.Labels != "" {
		concatWhereClause(&whereClause, "labels")
		args = append(args, wpfs.Labels)
	}
	if wpfs.FromSource != "" {
		concatWhereClause(&whereClause, "fromSource")
		args = append(args, wpfs.FromSource)
	}
	if duration != 0 {
		concatWhereClauseIntRange(&whereClause, "createdtime", time-duration, time)
	}

	_, err = db.Query(query+whereClause, args...)

	if err != nil {
		log.Error().Msg(err.Error())
		return err
	}
	return err
}

func UpdateWorkloadProcessFileSetMySQL(cfg types.ConfigDB, wpfs types.WorkloadProcessFileSet, fs []string) error {
	db := connectMySQL(cfg)
	defer db.Close()

	var err error
	time := ConvertStrToUnixTime("now")

	// set status -> outdated
	stmt, err := db.Prepare("UPDATE " + WorkloadProcessFileSet_TableName +
		" SET fileset=?,updatedtime=? WHERE clusterName = ? and containerName = ? and namespace = ? and labels = ? and fromSource = ? and settype = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	fsset := strings.Join(fs[:], types.RecordSeparator)

	_, err = stmt.Exec(fsset,
		time,
		wpfs.ClusterName,
		wpfs.ContainerName,
		wpfs.Namespace,
		wpfs.Labels,
		wpfs.FromSource,
		wpfs.SetType)

	/*
		a, err := res.RowsAffected()
		if err == nil {
			log.Info().Msgf("UPDATE rows affected:%d", a)
		}
	*/
	return err
}

// UpdateOrInsertKubearmorLogsSQLite -- Update existing log or insert a new log into DB
func UpdateOrInsertKubearmorLogsMySQL(cfg types.ConfigDB, kubearmorlogmap map[types.KubeArmorLog]int) error {
	db := connectMySQL(cfg)
	defer db.Close()

	start := time.Now().UnixMilli()
	log.Info().Msgf("sqlite update or insert %d\n", len(kubearmorlogmap))
	for kubearmorlog, count := range kubearmorlogmap {
		if err := updateOrInsertKubearmorLogsMySQL(db, kubearmorlog, count); err != nil {
			log.Error().Msg(err.Error())
		}
	}
	end := time.Now().UnixMilli()
	log.Info().Msgf("return sqlite update or insert %d time-taken-ms:%d\n", len(kubearmorlogmap), end-start)
	return nil
}

func updateOrInsertKubearmorLogsMySQL(db *sql.DB, kubearmorlog types.KubeArmorLog, count int) error {
	queryString := `cluster_name = ? and namespace_name = ? and pod_name = ? and container_name = ? and operation = ? and labels = ? 
					and data = ? and category = ? and action = ? and result = ? and source = ? and resource = ?`

	query := "UPDATE " + TableSystemLogs_TableName + " SET total=total+?, updated_time=? WHERE " + queryString + " "

	updateStmt, err := db.Prepare(query)
	if err != nil {
		return err
	}
	defer updateStmt.Close()

	result, err := updateStmt.Exec(
		count,
		ConvertStrToUnixTime("now"),
		kubearmorlog.ClusterName,
		kubearmorlog.NamespaceName,
		kubearmorlog.PodName,
		kubearmorlog.ContainerName,
		kubearmorlog.Operation,
		kubearmorlog.Labels,
		kubearmorlog.Data,
		kubearmorlog.Category,
		kubearmorlog.Action,
		kubearmorlog.Result,
		kubearmorlog.Source,
		kubearmorlog.Resource,
	)
	if err != nil {
		log.Error().Msg(err.Error())
		return err
	}

	rowsAffected, err := result.RowsAffected()

	if err == nil && rowsAffected == 0 {

		updateQueryString := `(cluster_name,namespace_name,pod_name,container_name,operation,labels,data,category,action,
		updated_time,result,total,source,resource) values(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

		updateQuery := "INSERT INTO " + TableSystemLogs_TableName + updateQueryString

		insertStmt, err := db.Prepare(updateQuery)
		if err != nil {
			return err
		}
		defer insertStmt.Close()

		_, err = insertStmt.Exec(
			kubearmorlog.ClusterName,
			kubearmorlog.NamespaceName,
			kubearmorlog.PodName,
			kubearmorlog.ContainerName,
			kubearmorlog.Operation,
			kubearmorlog.Labels,
			kubearmorlog.Data,
			kubearmorlog.Category,
			kubearmorlog.Action,
			ConvertStrToUnixTime("now"),
			kubearmorlog.Result,
			count,
			kubearmorlog.Source,
			kubearmorlog.Resource)
		if err != nil {
			log.Error().Msg(err.Error())
			return err
		}
	}

	return nil
}

// GetSystemLogsMySQL
func GetSystemLogsMySQL(cfg types.ConfigDB, filterLog types.KubeArmorLog) ([]types.KubeArmorLog, []uint32, error) {
	db := connectMySQL(cfg)
	defer db.Close()

	resLog := []types.KubeArmorLog{}
	resTotal := []uint32{}

	var results *sql.Rows
	var err error

	queryString := `cluster_name,namespace_name,pod_name,container_name,operation,labels,data,category,action,updated_time,result,total,process_name,parent_process_name,resource`

	query := "SELECT " + queryString + " FROM " + TableSystemLogs_TableName + " "

	var whereClause string
	var args []interface{}

	if filterLog.ClusterName != "" {
		concatWhereClause(&whereClause, "cluster_name")
		args = append(args, filterLog.ClusterName)
	}
	if filterLog.NamespaceName != "" {
		concatWhereClause(&whereClause, "namespace_name")
		args = append(args, filterLog.NamespaceName)
	}
	if filterLog.PodName != "" {
		concatWhereClause(&whereClause, "pod_name")
		args = append(args, filterLog.PodName)
	}
	if filterLog.ContainerName != "" {
		concatWhereClause(&whereClause, "container_name")
		args = append(args, filterLog.ContainerName)
	}
	if filterLog.Operation != "" {
		concatWhereClause(&whereClause, "operation")
		args = append(args, filterLog.Operation)
	}
	if filterLog.Labels != "" {
		concatWhereClause(&whereClause, "labels")
		args = append(args, filterLog.Labels)
	}
	if filterLog.Data != "" {
		concatWhereClause(&whereClause, "data")
		args = append(args, filterLog.Data)
	}
	if filterLog.Category != "" {
		concatWhereClause(&whereClause, "category")
		args = append(args, filterLog.Category)
	}
	if filterLog.Action != "" {
		concatWhereClause(&whereClause, "action")
		args = append(args, filterLog.Action)
	}
	if filterLog.UpdatedTime != 0 {
		concatWhereClause(&whereClause, "updated_time")
		args = append(args, filterLog.UpdatedTime)
	}
	if filterLog.Result != "" {
		concatWhereClause(&whereClause, "result")
		args = append(args, filterLog.Result)
	}
	if filterLog.ProcessName != "" {
		concatWhereClause(&whereClause, "process_name")
		args = append(args, filterLog.ProcessName)
	}
	if filterLog.ParentProcessName != "" {
		concatWhereClause(&whereClause, "parent_process_name")
		args = append(args, filterLog.ParentProcessName)
	}
	if filterLog.Resource != "" {
		concatWhereClause(&whereClause, "resource")
		args = append(args, filterLog.Resource)
	}

	results, err = db.Query(query+whereClause, args...)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil, nil, err
	}
	defer results.Close()

	for results.Next() {
		var loc_log types.KubeArmorLog
		var loc_total uint32
		if err := results.Scan(
			&loc_log.ClusterName,
			&loc_log.NamespaceName,
			&loc_log.PodName,
			&loc_log.ContainerName,
			&loc_log.Operation,
			&loc_log.Labels,
			&loc_log.Data,
			&loc_log.Category,
			&loc_log.Action,
			&loc_log.UpdatedTime,
			&loc_log.Result,
			&loc_total,
			&loc_log.ProcessName,
			&loc_log.ParentProcessName,
			&loc_log.Resource,
		); err != nil {
			return nil, nil, err
		}
		resLog = append(resLog, loc_log)
		resTotal = append(resTotal, loc_total)
	}

	return resLog, resTotal, err
}

// GetNetworkLogsMySQL
func GetCiliumLogsMySQL(cfg types.ConfigDB, filterLog types.CiliumLog) ([]types.CiliumLog, []uint32, error) {
	db := connectMySQL(cfg)
	defer db.Close()

	resLog := []types.CiliumLog{}
	resTotal := []uint32{}

	var results *sql.Rows
	var err error

	queryString := ` verdict,ip_source,ip_destination,ip_version,ip_encrypted,l4_tcp_source_port,l4_tcp_destination_port,
	l4_udp_source_port,l4_udp_destination_port,l4_icmpv4_type,l4_icmpv4_code,l4_icmpv6_type,l4_icmpv6_code,
	source_namespace,source_labels,source_pod_name,destination_namespace,destination_labels,destination_pod_name,
	type,node_name,l7_type,l7_dns_cnames,l7_dns_observation_source,l7_http_code,l7_http_method,l7_http_url,l7_http_protocol,l7_http_headers,
	event_type_type,event_type_sub_type,source_service_name,source_service_namespace,destination_service_name,destination_service_namespace,
	traffic_direction,trace_observation_point,drop_reason_desc,is_reply,start_time,updated_time,total`

	query := "SELECT " + queryString + " FROM " + TableNetworkLogs_TableName + " "

	var whereClause string
	var args []interface{}

	if filterLog.Verdict != "" {
		concatWhereClause(&whereClause, "verdict")
		args = append(args, filterLog.Verdict)
	}
	if filterLog.IpSource != "" {
		concatWhereClause(&whereClause, "ip_source")
		args = append(args, filterLog.IpSource)
	}
	if filterLog.IpDestination != "" {
		concatWhereClause(&whereClause, "ip_destination")
		args = append(args, filterLog.IpDestination)
	}
	if filterLog.IpVersion != "" {
		concatWhereClause(&whereClause, "ip_version")
		args = append(args, filterLog.IpVersion)
	}
	if filterLog.IpEncrypted {
		concatWhereClause(&whereClause, "ip_encrypted")
		args = append(args, filterLog.IpEncrypted)
	}
	if filterLog.L4TCPSourcePort != 0 {
		concatWhereClause(&whereClause, "l4_tcp_source_port")
		args = append(args, filterLog.L4TCPSourcePort)
	}
	if filterLog.L4TCPDestinationPort != 0 {
		concatWhereClause(&whereClause, "l4_tcp_destination_port")
		args = append(args, filterLog.L4TCPDestinationPort)
	}
	if filterLog.L4UDPSourcePort != 0 {
		concatWhereClause(&whereClause, "l4_udp_source_port")
		args = append(args, filterLog.L4UDPSourcePort)
	}
	if filterLog.L4UDPDestinationPort != 0 {
		concatWhereClause(&whereClause, "l4_udp_destination_port")
		args = append(args, filterLog.L4UDPDestinationPort)
	}
	if filterLog.L4ICMPv4Type != 0 {
		concatWhereClause(&whereClause, "l4_icmpv4_type")
		args = append(args, filterLog.L4ICMPv4Type)
	}
	if filterLog.L4ICMPv4Code != 0 {
		concatWhereClause(&whereClause, "l4_icmpv4_code")
		args = append(args, filterLog.L4ICMPv4Code)
	}
	if filterLog.L4ICMPv6Type != 0 {
		concatWhereClause(&whereClause, "l4_icmpv6_type")
		args = append(args, filterLog.L4ICMPv6Type)
	}
	if filterLog.L4ICMPv6Code != 0 {
		concatWhereClause(&whereClause, "l4_icmpv6_code")
		args = append(args, filterLog.L4ICMPv6Code)
	}
	if filterLog.SourceNamespace != "" {
		concatWhereClause(&whereClause, "source_namespace")
		args = append(args, filterLog.SourceNamespace)
	}
	if filterLog.SourceLabels != "" {
		concatWhereClause(&whereClause, "source_labels")
		args = append(args, filterLog.SourceLabels)
	}
	if filterLog.SourcePodName != "" {
		concatWhereClause(&whereClause, "source_pod_name")
		args = append(args, filterLog.SourcePodName)
	}
	if filterLog.DestinationNamespace != "" {
		concatWhereClause(&whereClause, "destination_namespace")
		args = append(args, filterLog.DestinationNamespace)
	}
	if filterLog.DestinationLabels != "" {
		concatWhereClause(&whereClause, "destination_labels")
		args = append(args, filterLog.DestinationLabels)
	}
	if filterLog.Type != "" {
		concatWhereClause(&whereClause, "type")
		args = append(args, filterLog.Type)
	}
	if filterLog.NodeName != "" {
		concatWhereClause(&whereClause, "node_name")
		args = append(args, filterLog.NodeName)
	}
	if filterLog.L7Type != "" {
		concatWhereClause(&whereClause, "l7_type")
		args = append(args, filterLog.L7Type)
	}
	if filterLog.L7DnsCnames != "" {
		concatWhereClause(&whereClause, "l7_dns_cnames")
		args = append(args, filterLog.L7DnsCnames)
	}
	if filterLog.L7DnsObservationsource != "" {
		concatWhereClause(&whereClause, "l7_dns_observation_source")
		args = append(args, filterLog.L7DnsObservationsource)
	}
	if filterLog.L7HttpCode != 0 {
		concatWhereClause(&whereClause, "l7_http_code")
		args = append(args, filterLog.L7HttpCode)
	}
	if filterLog.L7HttpMethod != "" {
		concatWhereClause(&whereClause, "l7_http_method")
		args = append(args, filterLog.L7HttpMethod)
	}
	if filterLog.L7HttpUrl != "" {
		concatWhereClause(&whereClause, "l7_http_url")
		args = append(args, filterLog.L7HttpUrl)
	}
	if filterLog.L7HttpProtocol != "" {
		concatWhereClause(&whereClause, "l7_http_protocol")
		args = append(args, filterLog.L7HttpProtocol)
	}
	if filterLog.L7HttpHeaders != "" {
		concatWhereClause(&whereClause, "l7_http_headers")
		args = append(args, filterLog.L7HttpHeaders)
	}
	if filterLog.EventTypeType != 0 {
		concatWhereClause(&whereClause, "event_type_type")
		args = append(args, filterLog.EventTypeType)
	}
	if filterLog.EventTypeSubType != 0 {
		concatWhereClause(&whereClause, "event_type_sub_type")
		args = append(args, filterLog.EventTypeSubType)
	}
	if filterLog.SourceServiceName != "" {
		concatWhereClause(&whereClause, "source_service_name")
		args = append(args, filterLog.SourceServiceName)
	}
	if filterLog.SourceServiceNamespace != "" {
		concatWhereClause(&whereClause, "source_service_namespace")
		args = append(args, filterLog.SourceServiceNamespace)
	}
	if filterLog.DestinationServiceName != "" {
		concatWhereClause(&whereClause, "destination_service_name")
		args = append(args, filterLog.DestinationServiceName)
	}
	if filterLog.DestinationServiceNamespace != "" {
		concatWhereClause(&whereClause, "destination_service_namespace")
		args = append(args, filterLog.DestinationServiceNamespace)
	}
	if filterLog.TrafficDirection != "" {
		concatWhereClause(&whereClause, "traffic_direction")
		args = append(args, filterLog.TrafficDirection)
	}
	if filterLog.TraceObservationPoint != "" {
		concatWhereClause(&whereClause, "trace_observation_point")
		args = append(args, filterLog.TraceObservationPoint)
	}
	if filterLog.DropReasonDesc != "" {
		concatWhereClause(&whereClause, "drop_reason_desc")
		args = append(args, filterLog.DropReasonDesc)
	}
	if filterLog.IsReply {
		concatWhereClause(&whereClause, "is_reply")
		args = append(args, filterLog.IsReply)
	}
	if filterLog.StartTime != 0 {
		concatWhereClause(&whereClause, "start_time")
		args = append(args, filterLog.StartTime)
	}
	if filterLog.UpdatedTime != 0 {
		concatWhereClause(&whereClause, "updated_time")
		args = append(args, filterLog.UpdatedTime)
	}
	if filterLog.Total != 0 {
		concatWhereClause(&whereClause, "total")
		args = append(args, filterLog.Total)
	}

	results, err = db.Query(query+whereClause, args...)

	if err != nil {
		log.Error().Msg(err.Error())
		return nil, nil, err
	}
	defer results.Close()

	for results.Next() {
		var loc_log types.CiliumLog
		var loc_total uint32
		if err := results.Scan(
			&loc_log.Verdict,
			&loc_log.IpSource,
			&loc_log.IpDestination,
			&loc_log.IpVersion,
			&loc_log.IpEncrypted,
			&loc_log.L4TCPSourcePort,
			&loc_log.L4TCPDestinationPort,
			&loc_log.L4UDPSourcePort,
			&loc_log.L4UDPDestinationPort,
			&loc_log.L4ICMPv4Type,
			&loc_log.L4ICMPv4Code,
			&loc_log.L4ICMPv6Type,
			&loc_log.L4ICMPv6Code,
			&loc_log.SourceNamespace,
			&loc_log.SourceLabels,
			&loc_log.SourcePodName,
			&loc_log.DestinationNamespace,
			&loc_log.DestinationLabels,
			&loc_log.DestinationPodName,
			&loc_log.Type,
			&loc_log.NodeName,
			&loc_log.L7Type,
			&loc_log.L7DnsCnames,
			&loc_log.L7DnsObservationsource,
			&loc_log.L7HttpCode,
			&loc_log.L7HttpMethod,
			&loc_log.L7HttpUrl,
			&loc_log.L7HttpProtocol,
			&loc_log.L7HttpHeaders,
			&loc_log.EventTypeType,
			&loc_log.EventTypeSubType,
			&loc_log.SourceServiceName,
			&loc_log.SourceServiceNamespace,
			&loc_log.DestinationServiceName,
			&loc_log.DestinationServiceNamespace,
			&loc_log.TrafficDirection,
			&loc_log.TraceObservationPoint,
			&loc_log.DropReasonDesc,
			&loc_log.IsReply,
			&loc_log.StartTime,
			&loc_log.UpdatedTime,
			&loc_total,
		); err != nil {
			return nil, nil, err
		}
		resLog = append(resLog, loc_log)
		resTotal = append(resTotal, loc_total)
	}
	return resLog, resTotal, err
}

// UpdateCiliumLogsMySQL -- Update existing log with time and count
func UpdateOrInsertCiliumLogsMySQL(cfg types.ConfigDB, ciliumlogs []types.CiliumLog) error {
	var err error = nil
	db := connectMySQL(cfg)
	defer db.Close()

	for _, ciliumLog := range ciliumlogs {
		if err := updateOrInsertCiliumLogMySQL(db, ciliumLog); err != nil {
			log.Error().Msg(err.Error())
		}
	}
	return err
}

func updateOrInsertCiliumLogMySQL(db *sql.DB, ciliumlog types.CiliumLog) error {
	var err error
	queryString := `verdict = ? and ip_source = ? and ip_destination = ? and ip_version = ? and ip_encrypted = ? and l4_tcp_source_port = ? and 
					l4_tcp_destination_port = ? and l4_udp_source_port = ? and l4_udp_destination_port = ? and l4_icmpv4_type = ? and 
					l4_icmpv4_code = ? and l4_icmpv6_type = ? and l4_icmpv6_code = ? and source_namespace = ? and source_labels = ? and 
					source_pod_name = ? and destination_namespace = ? and destination_labels = ? and destination_pod_name = ? and type = ? and 
					node_name = ? and l7_type = ? and l7_dns_cnames = ? and l7_dns_observation_source = ? and l7_http_code = ? and 
					l7_http_method = ? and l7_http_url = ? and l7_http_protocol = ? and l7_http_headers = ? and event_type_type = ? and 
					event_type_sub_type = ? and source_service_name = ? and source_service_namespace = ? and destination_service_name = ? and 
					destination_service_namespace = ? and traffic_direction = ? and trace_observation_point = ? and drop_reason_desc = ? and is_reply = ? `

	query := "UPDATE " + TableNetworkLogs_TableName + " SET total=total+1, updated_time=? WHERE " + queryString + " "

	updateStmt, err := db.Prepare(query)
	if err != nil {
		return err
	}
	defer updateStmt.Close()

	result, err := updateStmt.Exec(
		ciliumlog.UpdatedTime,
		ciliumlog.Verdict,
		ciliumlog.IpSource,
		ciliumlog.IpDestination,
		ciliumlog.IpVersion,
		ciliumlog.IpEncrypted,
		ciliumlog.L4TCPSourcePort,
		ciliumlog.L4TCPDestinationPort,
		ciliumlog.L4UDPSourcePort,
		ciliumlog.L4UDPDestinationPort,
		ciliumlog.L4ICMPv4Type,
		ciliumlog.L4ICMPv4Code,
		ciliumlog.L4ICMPv6Type,
		ciliumlog.L4ICMPv6Code,
		ciliumlog.SourceNamespace,
		ciliumlog.SourceLabels,
		ciliumlog.SourcePodName,
		ciliumlog.DestinationNamespace,
		ciliumlog.DestinationLabels,
		ciliumlog.DestinationPodName,
		ciliumlog.Type,
		ciliumlog.NodeName,
		ciliumlog.L7Type,
		ciliumlog.L7DnsCnames,
		ciliumlog.L7DnsObservationsource,
		ciliumlog.L7HttpCode,
		ciliumlog.L7HttpMethod,
		ciliumlog.L7HttpUrl,
		ciliumlog.L7HttpProtocol,
		ciliumlog.L7HttpHeaders,
		ciliumlog.EventTypeType,
		ciliumlog.EventTypeSubType,
		ciliumlog.SourceServiceName,
		ciliumlog.SourceServiceNamespace,
		ciliumlog.DestinationServiceName,
		ciliumlog.DestinationServiceNamespace,
		ciliumlog.TrafficDirection,
		ciliumlog.TraceObservationPoint,
		ciliumlog.DropReasonDesc,
		ciliumlog.IsReply,
	)
	if err != nil {
		log.Error().Msg(err.Error())
		return err
	}

	rowsAffected, err := result.RowsAffected()

	if err == nil && rowsAffected == 0 {
		insertQueryString := `(verdict,ip_source,ip_destination,ip_version,ip_encrypted,l4_tcp_source_port,l4_tcp_destination_port,
			l4_udp_source_port,l4_udp_destination_port,l4_icmpv4_type,l4_icmpv4_code,l4_icmpv6_type,l4_icmpv6_code,
			source_namespace,source_labels,source_pod_name,destination_namespace,destination_labels,destination_pod_name,
			type,node_name,l7_type,l7_dns_cnames,l7_dns_observation_source,l7_http_code,l7_http_method,l7_http_url,l7_http_protocol,l7_http_headers,
			event_type_type,event_type_sub_type,source_service_name,source_service_namespace,destination_service_name,destination_service_namespace,
			traffic_direction,trace_observation_point,drop_reason_desc,is_reply,start_time,updated_time,total) 
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

		query := "INSERT INTO " + TableNetworkLogs_TableName + insertQueryString

		insertStmt, err := db.Prepare(query)
		if err != nil {
			return err
		}
		defer insertStmt.Close()

		_, err = insertStmt.Exec(
			ciliumlog.Verdict,
			ciliumlog.IpSource,
			ciliumlog.IpDestination,
			ciliumlog.IpVersion,
			ciliumlog.IpEncrypted,
			ciliumlog.L4TCPSourcePort,
			ciliumlog.L4TCPDestinationPort,
			ciliumlog.L4UDPSourcePort,
			ciliumlog.L4UDPDestinationPort,
			ciliumlog.L4ICMPv4Type,
			ciliumlog.L4ICMPv4Code,
			ciliumlog.L4ICMPv6Type,
			ciliumlog.L4ICMPv6Code,
			ciliumlog.SourceNamespace,
			ciliumlog.SourceLabels,
			ciliumlog.SourcePodName,
			ciliumlog.DestinationNamespace,
			ciliumlog.DestinationLabels,
			ciliumlog.DestinationPodName,
			ciliumlog.Type,
			ciliumlog.NodeName,
			ciliumlog.L7Type,
			ciliumlog.L7DnsCnames,
			ciliumlog.L7DnsObservationsource,
			ciliumlog.L7HttpCode,
			ciliumlog.L7HttpMethod,
			ciliumlog.L7HttpUrl,
			ciliumlog.L7HttpProtocol,
			ciliumlog.L7HttpHeaders,
			ciliumlog.EventTypeType,
			ciliumlog.EventTypeSubType,
			ciliumlog.SourceServiceName,
			ciliumlog.SourceServiceNamespace,
			ciliumlog.DestinationServiceName,
			ciliumlog.DestinationServiceNamespace,
			ciliumlog.TrafficDirection,
			ciliumlog.TraceObservationPoint,
			ciliumlog.DropReasonDesc,
			ciliumlog.IsReply,
			ciliumlog.StartTime,
			ciliumlog.UpdatedTime,
			1)
		if err != nil {
			log.Error().Msg(err.Error())
		}
	}

	return err
}

func GetPodNamesMySQL(cfg types.ConfigDB, filter types.ObsPodDetail) ([]string, error) {
	db := connectMySQL(cfg)
	defer db.Close()

	resPodNames := []string{}

	var results *sql.Rows
	var err error

	// Get podnames from system table
	query := "SELECT pod_name FROM " + TableSystemLogs_TableName + " "

	var whereClause string
	var sysargs []interface{}

	if filter.ClusterName != "" {
		concatWhereClause(&whereClause, "cluster_name")
		sysargs = append(sysargs, filter.ClusterName)
	}
	if filter.Namespace != "" {
		concatWhereClause(&whereClause, "namespace_name")
		sysargs = append(sysargs, filter.Namespace)
	}
	if filter.PodName != "" {
		concatWhereClause(&whereClause, "pod_name")
		sysargs = append(sysargs, filter.PodName)
	}
	if filter.Labels != "" {
		concatWhereClause(&whereClause, "labels")
		sysargs = append(sysargs, filter.Labels)
	}
	if filter.ContainerName != "" {
		concatWhereClause(&whereClause, "container_name")
		sysargs = append(sysargs, filter.ContainerName)
	}

	results, err = db.Query(query+whereClause, sysargs...)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil, err
	}
	defer results.Close()

	for results.Next() {
		var locPodName string
		if err := results.Scan(
			&locPodName,
		); err != nil {
			return nil, err
		}
		resPodNames = append(resPodNames, locPodName)
	}

	// Get podnames from network table
	query = "SELECT source_pod_name FROM " + TableNetworkLogs_TableName + " "

	whereClause = ""
	var nwargs []interface{}

	if filter.Namespace != "" {
		concatWhereClause(&whereClause, "source_namespace")
		nwargs = append(nwargs, filter.Namespace)
	}
	if filter.Labels != "" {
		concatWhereClause(&whereClause, "source_labels")
		nwargs = append(nwargs, filter.Labels)
	}

	results, err = db.Query(query+whereClause, nwargs...)
	if err != nil {
		log.Error().Msg(err.Error())
		return nil, err
	}
	defer results.Close()

	for results.Next() {
		var locPodName string
		if err := results.Scan(
			&locPodName,
		); err != nil {
			return nil, err
		}
		resPodNames = append(resPodNames, locPodName)
	}

	return resPodNames, err
}

// =============== //
// == Policy DB == //
// =============== //

func GetPolicyYamlsMySQL(cfg types.ConfigDB, policyType string) ([]types.PolicyYaml, error) {
	db := connectMySQL(cfg)
	defer db.Close()

	policies := []types.PolicyYaml{}

	var results *sql.Rows
	var err error

	query := "SELECT type,kind,cluster_name,namespace,labels,policy_name,policy_yaml FROM " + PolicyYamlTable_TableName
	query = query + "WHERE type = ?"

	results, err = db.Query(query, policyType)
	defer results.Close()

	if err != nil {
		log.Error().Msg(err.Error())
		return nil, err
	}

	for results.Next() {
		var labels string
		policy := types.PolicyYaml{}

		if err := results.Scan(
			&policy.Type,
			&policy.Kind,
			&policy.Cluster,
			&policy.Namespace,
			&labels,
			&policy.Name,
			&policy.Yaml,
		); err != nil {
			return nil, err
		}

		policy.Labels = LabelMapFromString(labels)
		policies = append(policies, policy)
	}

	return policies, nil
}

func UpdateOrInsertPolicyYamlsMySQL(cfg types.ConfigDB, policies []types.PolicyYaml) error {
	db := connectMySQL(cfg)
	defer db.Close()

	for _, pol := range policies {
		if err := updateOrInsertPolicyYamlMySQL(pol, db); err != nil {
			log.Error().Msg(err.Error())
		}
	}

	return nil
}

func updateOrInsertPolicyYamlMySQL(policy types.PolicyYaml, db *sql.DB) error {
	var err error
	queryString := ` policy_name = ? `

	query := "UPDATE " + PolicyYamlTable_TableName + " SET policy_yaml=?, updated_time=? WHERE " + queryString + " "

	updateStmt, err := db.Prepare(query)
	if err != nil {
		return err
	}
	defer updateStmt.Close()

	result, err := updateStmt.Exec(
		policy.Yaml,
		ConvertStrToUnixTime("now"),
		policy.Name,
	)
	if err != nil {
		log.Error().Msg(err.Error())
		return err
	}

	rowsAffected, err := result.RowsAffected()

	if err == nil && rowsAffected == 0 {

		insertStmt, err := db.Prepare("INSERT INTO " + PolicyYamlTable_TableName +
			"(type,kind,cluster_name,namespace,labels,policy_name,policy_yaml,updated_time) values(?,?,?,?,?,?,?,?)")
		if err != nil {
			return err
		}
		defer insertStmt.Close()

		_, err = insertStmt.Exec(
			policy.Type,
			policy.Kind,
			policy.Cluster,
			policy.Namespace,
			LabelMapToString(policy.Labels),
			policy.Name,
			policy.Yaml,
			ConvertStrToUnixTime("now"),
		)
		if err != nil {
			log.Error().Msg(err.Error())
		}
	}

	return err
}
