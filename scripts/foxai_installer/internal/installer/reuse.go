//go:build linux

package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (i *installer) resolveReusedDataNodesForInstall(originalTargets []string) ([]string, error) {
	section("REUSED DATANODE DETECTION")
	if len(originalTargets) == 0 {
		fmt.Println("  - No DataNodes requested")
		return nil, nil
	}
	clusterID, err := i.readLocalNameNodeClusterID()
	if err != nil {
		return nil, err
	}
	fmt.Printf("  - Local NameNode clusterID: %s\n", clusterID)

	probes := make([]dataNodeReuseProbe, 0, len(originalTargets))
	probeByIP := make(map[string]dataNodeReuseProbe, len(originalTargets))
	actionable := make([]dataNodeReuseProbe, 0)
	for _, ip := range originalTargets {
		probe := i.probeDataNodeReuse(ip, clusterID)
		probes = append(probes, probe)
		probeByIP[ip] = probe
		fmt.Printf("  - %s: %s", ip, probe.State)
		if probe.ClusterID != "" {
			fmt.Printf(" (clusterID=%s)", probe.ClusterID)
		}
		if probe.Details != "" {
			fmt.Printf(" - %s", probe.Details)
		}
		fmt.Println()
		if isActionableReuseState(probe.State) {
			actionable = append(actionable, probe)
		}
	}
	skipped := make(map[string]bool)
	if len(actionable) > 0 {
		choice, err := i.promptChoice(
			fmt.Sprintf("We found %d DataNodes that need a decision before continuing. Partial nodes can usually continue with normal installer sync/setup. Conflicting or unreadable nodes usually need wipe or skip.", len(actionable)),
			[]string{
				"1. Stop installer",
				"2. Apply recommended action for each actionable DataNode",
				"3. Skip all actionable DataNodes for this run",
				"4. Review one by one",
			},
		)
		if err != nil {
			return nil, err
		}
		switch choice {
		case 1:
			return nil, fmt.Errorf("install stopped by user after reused DataNode detection")
		case 2:
			for _, probe := range actionable {
				if err := i.applyRecommendedReuseAction(probe, skipped); err != nil {
					return nil, err
				}
			}
		case 3:
			for _, probe := range actionable {
				skipped[probe.IP] = true
				i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusSkip, fmt.Sprintf("%s node skipped for this install run", probe.State))
			}
		case 4:
			for _, probe := range actionable {
				if err := i.reviewReuseProbe(probe, skipped); err != nil {
					return nil, err
				}
			}
		}
	}

	finalTargets := make([]string, 0, len(originalTargets))
	mutationTargets := make([]string, 0, len(originalTargets))
	for _, ip := range originalTargets {
		if skipped[ip] {
			continue
		}
		finalTargets = append(finalTargets, ip)
		probe := probeByIP[ip]
		if probe.State == dataNodeReuseCompatible {
			i.addSummary(i.nodeLabelForIP(ip), "datanode state", statusOK, "compatible node already converged; skipping sync/setup")
			continue
		}
		mutationTargets = append(mutationTargets, ip)
	}
	if len(finalTargets) == 0 {
		return nil, fmt.Errorf("no DataNodes remain active for this install run after reused-node resolution")
	}
	i.cfg.ExistingNodeIPs = filterSkippedIPs(i.cfg.ExistingNodeIPs, skipped)
	i.cfg.NewNodeIPs = filterSkippedIPs(i.cfg.NewNodeIPs, skipped)
	fmt.Printf("  - Final DataNodes for this run: %s\n", strings.Join(finalTargets, ", "))
	if len(mutationTargets) == 0 {
		fmt.Println("  - All requested DataNodes are already converged with the current NameNode; remote sync/setup will be skipped")
	} else {
		fmt.Printf("  - DataNodes requiring sync/setup: %s\n", strings.Join(mutationTargets, ", "))
	}
	return mutationTargets, nil
}

func filterSkippedIPs(ips []string, skipped map[string]bool) []string {
	filtered := make([]string, 0, len(ips))
	for _, ip := range ips {
		if skipped[ip] {
			continue
		}
		filtered = append(filtered, ip)
	}
	return filtered
}

func isActionableReuseState(state dataNodeReuseState) bool {
	return state == dataNodeReuseConflicting || state == dataNodeReusePartial || state == dataNodeReuseUnreadable
}

func recommendedReuseAction(probe dataNodeReuseProbe) string {
	switch probe.State {
	case dataNodeReusePartial:
		details := strings.ToLower(probe.Details)
		if strings.Contains(details, "version file missing") || strings.Contains(details, "clusterid missing") {
			return "wipe"
		}
		return "continue"
	case dataNodeReuseConflicting, dataNodeReuseUnreadable:
		return "wipe"
	default:
		return "continue"
	}
}

func (i *installer) applyRecommendedReuseAction(probe dataNodeReuseProbe, skipped map[string]bool) error {
	switch recommendedReuseAction(probe) {
	case "wipe":
		if err := i.reinitializeRemoteDataNodeState(probe.IP); err != nil {
			return err
		}
		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusFixed, fmt.Sprintf("%s node reinitialized and reused", probe.State))
	case "continue":
		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusWarn, fmt.Sprintf("%s node accepted for normal installer sync/setup", probe.State))
	default:
		skipped[probe.IP] = true
		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusSkip, fmt.Sprintf("%s node skipped for this install run", probe.State))
	}
	return nil
}

func (i *installer) reviewReuseProbe(probe dataNodeReuseProbe, skipped map[string]bool) error {
	if probe.State == dataNodeReusePartial {
		return i.reviewPartialReuseProbe(probe, skipped)
	}

	resolution, err := i.promptChoice(
		fmt.Sprintf("%s (%s) is %s. %s", i.nodeLabelForIP(probe.IP), probe.IP, probe.State, probe.Details),
		[]string{
			"1. Stop installer",
			"2. Reinitialize old DataNode state and reuse this node (Recommended)",
			"3. Skip this node for this run",
			"4. Keep old storage and force continue (Unsafe)",
		},
	)
	if err != nil {
		return err
	}

	switch resolution {
	case 1:
		return fmt.Errorf("install stopped by user while reviewing reused DataNodes")
	case 2:
		if err := i.reinitializeRemoteDataNodeState(probe.IP); err != nil {
			return err
		}
		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusFixed, fmt.Sprintf("%s node reinitialized and reused", probe.State))
	case 3:
		skipped[probe.IP] = true
		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusSkip, fmt.Sprintf("%s node skipped for this install run", probe.State))
	case 4:
		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusWarn, fmt.Sprintf("%s node forced to continue with old state", probe.State))
	}
	return nil
}

func (i *installer) reviewPartialReuseProbe(probe dataNodeReuseProbe, skipped map[string]bool) error {
	recommendedAction := recommendedReuseAction(probe)
	options := []string{"1. Stop installer"}
	continueChoice := 0
	wipeChoice := 0
	skipChoice := 0

	if recommendedAction == "wipe" {
		options = append(options,
			"2. Reinitialize old DataNode state and reuse this node (Recommended)",
			"3. Continue with normal installer sync/setup",
			"4. Skip this node for this run",
		)
		wipeChoice = 2
		continueChoice = 3
		skipChoice = 4
	} else {
		options = append(options,
			"2. Continue with normal installer sync/setup (Recommended)",
			"3. Reinitialize old DataNode state and reuse this node",
			"4. Skip this node for this run",
		)
		continueChoice = 2
		wipeChoice = 3
		skipChoice = 4
	}

	resolution, err := i.promptChoice(
		fmt.Sprintf("%s (%s) is %s. %s", i.nodeLabelForIP(probe.IP), probe.IP, probe.State, probe.Details),
		options,
	)
	if err != nil {
		return err
	}

	switch resolution {
	case 1:
		return fmt.Errorf("install stopped by user while reviewing partial DataNodes")
	case continueChoice:
		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusWarn, "partial node accepted for normal installer sync/setup")
	case wipeChoice:
		if err := i.reinitializeRemoteDataNodeState(probe.IP); err != nil {
			return err
		}
		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusFixed, "partial node reinitialized and reused")
	case skipChoice:
		skipped[probe.IP] = true
		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusSkip, "partial node skipped for this install run")
	}
	return nil
}

func (i *installer) readLocalNameNodeClusterID() (string, error) {
	versionPath := filepath.Join(i.baseHome, "hadoopdata", "namenode", "current", "VERSION")
	data, err := os.ReadFile(versionPath)
	if err != nil {
		return "", fmt.Errorf("failed to read local NameNode VERSION file %s: %w", versionPath, err)
	}
	clusterID := parseClusterIDFromVersion(string(data))
	if clusterID == "" {
		return "", fmt.Errorf("local NameNode VERSION file %s is missing clusterID", versionPath)
	}
	return clusterID, nil
}

func parseClusterIDFromVersion(text string) string {
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "clusterID=") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "clusterID="))
	}
	return ""
}

func sha256Text(text string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(text, "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (i *installer) probeDataNodeReuse(ip, localClusterID string) dataNodeReuseProbe {
	target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
	dataNodeDir := filepath.Join("/home", i.cfg.DataNodeUser, "hadoopdata", "datanode")
	versionPath := filepath.Join(dataNodeDir, "current", "VERSION")
	hadoopHome := filepath.Join("/home", i.cfg.DataNodeUser, "hadoop")
	sparkHome := i.sparkHome
	bashrcPath := filepath.Join("/home", i.cfg.DataNodeUser, ".bashrc")
	coreSitePath := filepath.Join(i.hadoopHome, "etc", "hadoop", "core-site.xml")
	hdfsSitePath := filepath.Join(i.hadoopHome, "etc", "hadoop", "hdfs-site.xml")
	workersPath := filepath.Join(i.hadoopHome, "etc", "hadoop", "workers")
	mapredSitePath := filepath.Join(i.hadoopHome, "etc", "hadoop", "mapred-site.xml")
	yarnSitePath := filepath.Join(i.hadoopHome, "etc", "hadoop", "yarn-site.xml")
	hadoopEnvPath := filepath.Join(i.hadoopHome, "etc", "hadoop", "hadoop-env.sh")
	coreSiteSHA, err := sha256File(coreSitePath)
	if err != nil {
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseUnreadable, Details: fmt.Sprintf("failed to hash local core-site.xml: %v", err)}
	}
	hdfsSiteSHA, err := sha256File(hdfsSitePath)
	if err != nil {
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseUnreadable, Details: fmt.Sprintf("failed to hash local hdfs-site.xml: %v", err)}
	}
	workersSHA, err := sha256File(workersPath)
	if err != nil {
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseUnreadable, Details: fmt.Sprintf("failed to hash local workers file: %v", err)}
	}
	mapredSiteSHA, err := sha256File(mapredSitePath)
	if err != nil {
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseUnreadable, Details: fmt.Sprintf("failed to hash local mapred-site.xml: %v", err)}
	}
	yarnSiteSHA, err := sha256File(yarnSitePath)
	if err != nil {
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseUnreadable, Details: fmt.Sprintf("failed to hash local yarn-site.xml: %v", err)}
	}
	hadoopEnvSHA, err := sha256File(hadoopEnvPath)
	if err != nil {
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseUnreadable, Details: fmt.Sprintf("failed to hash local hadoop-env.sh: %v", err)}
	}
	expectedHostsSHA := sha256Text(i.desiredLocalHostsBlock())
	expectedEnvSHA := sha256Text(i.desiredEnvBlock())
	cmd := fmt.Sprintf(`DATA_DIR=%q
    VERSION_PATH=%q
    HADOOP_HOME=%q
    SPARK_HOME=%q
    BASHRC=%q
    LOCAL_CLUSTER_ID=%q
    CORE_SITE="$HADOOP_HOME/etc/hadoop/core-site.xml"
    HDFS_SITE="$HADOOP_HOME/etc/hadoop/hdfs-site.xml"
    WORKERS_FILE="$HADOOP_HOME/etc/hadoop/workers"
    MAPRED_SITE="$HADOOP_HOME/etc/hadoop/mapred-site.xml"
    YARN_SITE="$HADOOP_HOME/etc/hadoop/yarn-site.xml"
    HADOOP_ENV="$HADOOP_HOME/etc/hadoop/hadoop-env.sh"
    HOSTS_FILE="/etc/hosts"
    HOSTS_BEGIN=%q
    HOSTS_END=%q
    ENV_BEGIN=%q
    ENV_END=%q
    EXPECTED_CORE_SHA=%q
    EXPECTED_HDFS_SHA=%q
    EXPECTED_WORKERS_SHA=%q
    EXPECTED_MAPRED_SHA=%q
    EXPECTED_YARN_SHA=%q
    EXPECTED_HADOOP_ENV_SHA=%q
    EXPECTED_HOSTS_SHA=%q
    EXPECTED_ENV_SHA=%q
    PARTIAL_REASONS=()
    MANAGED_SIGNALS=0

    hash_file() {
      sha256sum "$1" | awk '{print $1}'
    }

    managed_block_hash() {
      python3 - "$1" "$2" "$3" <<'PYEOF'
import hashlib
import sys

path, begin, end = sys.argv[1], sys.argv[2], sys.argv[3]
try:
    text = open(path).read()
except FileNotFoundError:
    print("missing")
    raise SystemExit(0)
start = text.find(begin)
if start == -1:
    print("missing")
    raise SystemExit(0)
end_idx = text.find(end, start)
if end_idx == -1:
    print("broken")
    raise SystemExit(0)
end_idx += len(end)
block = text[start:end_idx].replace("\r\n", "\n").rstrip("\n") + "\n"
print(hashlib.sha256(block.encode()).hexdigest())
PYEOF
    }

    if [ -d "$DATA_DIR" ]; then
      MANAGED_SIGNALS=1
    fi
    if [ -d "$DATA_DIR" ] && [ ! -f "$VERSION_PATH" ]; then
      PARTIAL_REASONS+=("datanode storage exists but VERSION file missing")
    fi

    if [ -d "$HADOOP_HOME" ]; then
      MANAGED_SIGNALS=1
      MISSING_HADOOP=()
      [ -f "$CORE_SITE" ] || MISSING_HADOOP+=("core-site.xml")
      [ -f "$HDFS_SITE" ] || MISSING_HADOOP+=("hdfs-site.xml")
      [ -f "$WORKERS_FILE" ] || MISSING_HADOOP+=("workers")
      [ -f "$MAPRED_SITE" ] || MISSING_HADOOP+=("mapred-site.xml")
      [ -f "$YARN_SITE" ] || MISSING_HADOOP+=("yarn-site.xml")
      [ -f "$HADOOP_ENV" ] || MISSING_HADOOP+=("hadoop-env.sh")
      if [ ${#MISSING_HADOOP[@]} -gt 0 ]; then
        PARTIAL_REASONS+=("hadoop config incomplete: ${MISSING_HADOOP[*]}")
      else
        [ "$(hash_file "$CORE_SITE")" = "$EXPECTED_CORE_SHA" ] || PARTIAL_REASONS+=("core-site.xml differs from namenode-managed content")
        [ "$(hash_file "$HDFS_SITE")" = "$EXPECTED_HDFS_SHA" ] || PARTIAL_REASONS+=("hdfs-site.xml differs from namenode-managed content")
        [ "$(hash_file "$WORKERS_FILE")" = "$EXPECTED_WORKERS_SHA" ] || PARTIAL_REASONS+=("workers file differs from namenode-managed content")
        [ "$(hash_file "$MAPRED_SITE")" = "$EXPECTED_MAPRED_SHA" ] || PARTIAL_REASONS+=("mapred-site.xml differs from namenode-managed content")
        [ "$(hash_file "$YARN_SITE")" = "$EXPECTED_YARN_SHA" ] || PARTIAL_REASONS+=("yarn-site.xml differs from namenode-managed content")
        [ "$(hash_file "$HADOOP_ENV")" = "$EXPECTED_HADOOP_ENV_SHA" ] || PARTIAL_REASONS+=("hadoop-env.sh differs from namenode-managed content")
      fi
    fi

    if [ -d "$SPARK_HOME" ]; then
      MANAGED_SIGNALS=1
      if [ ! -x "$SPARK_HOME/bin/spark-submit" ]; then
        PARTIAL_REASONS+=("spark home exists but spark-submit is missing")
      fi
    fi

    if [ -f "$BASHRC" ]; then
      ENV_HASH=$(managed_block_hash "$BASHRC" "$ENV_BEGIN" "$ENV_END")
      if [ "$ENV_HASH" != "missing" ]; then
        MANAGED_SIGNALS=1
        if [ "$ENV_HASH" = "broken" ]; then
          PARTIAL_REASONS+=("managed env block is incomplete")
        elif [ "$ENV_HASH" != "$EXPECTED_ENV_SHA" ]; then
          PARTIAL_REASONS+=("managed env block differs from namenode-managed content")
        fi
      fi
    fi

    HOSTS_HASH=$(managed_block_hash "$HOSTS_FILE" "$HOSTS_BEGIN" "$HOSTS_END")
    if [ "$HOSTS_HASH" = "broken" ]; then
      MANAGED_SIGNALS=1
      PARTIAL_REASONS+=("/etc/hosts managed block is incomplete")
    elif [ "$HOSTS_HASH" != "missing" ]; then
      MANAGED_SIGNALS=1
      if [ "$HOSTS_HASH" != "$EXPECTED_HOSTS_SHA" ]; then
        PARTIAL_REASONS+=("/etc/hosts managed block differs from namenode-managed content")
      fi
    fi

    if [ "$MANAGED_SIGNALS" -eq 1 ] && [ ! -d "$DATA_DIR" ]; then
      PARTIAL_REASONS+=("managed runtime exists without datanode storage")
    fi

    DETAILS=""
    if [ ${#PARTIAL_REASONS[@]} -gt 0 ]; then
      DETAILS="$(IFS='; '; printf '%%s' "${PARTIAL_REASONS[*]}")"
    fi

    if [ ! -d "$DATA_DIR" ] && [ "$MANAGED_SIGNALS" -eq 0 ]; then
      printf 'fresh||no datanode storage or managed runtime
'
    elif [ -f "$VERSION_PATH" ]; then
      CID=$(awk -F= '$1=="clusterID"{print $2}' "$VERSION_PATH" | tr -d '[:space:]')
      if [ -z "$CID" ]; then
        if [ -n "$DETAILS" ]; then
          DETAILS="VERSION file exists but clusterID missing; $DETAILS"
        else
          DETAILS="VERSION file exists but clusterID missing"
        fi
        printf 'partial||%%s
' "$DETAILS"
      elif [ "$CID" != "$LOCAL_CLUSTER_ID" ]; then
        printf 'found|%%s|clusterID differs from current NameNode (%%s)
' "$CID" "$LOCAL_CLUSTER_ID"
      elif [ -n "$DETAILS" ]; then
        printf 'partial|%%s|clusterID matches current NameNode; %%s
' "$CID" "$DETAILS"
      else
        printf 'found|%%s|clusterID matches current NameNode
' "$CID"
      fi
    elif [ -n "$DETAILS" ]; then
      printf 'partial||%%s
' "$DETAILS"
    else
      printf 'unreadable||unable to classify datanode state
'
    fi`, dataNodeDir, versionPath, hadoopHome, sparkHome, bashrcPath, localClusterID, hostsBegin, hostsEnd, envBegin, envEnd, coreSiteSHA, hdfsSiteSHA, workersSHA, mapredSiteSHA, yarnSiteSHA, hadoopEnvSHA, expectedHostsSHA, expectedEnvSHA)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runProbe := func() error {
		stdout.Reset()
		stderr.Reset()
		return runRemoteBashCommand(nil, &stdout, &stderr, target, cmd)
	}
	if err := i.withRemoteSSHRecovery(ip, "reused datanode probe", runProbe); err != nil {
		details := strings.TrimSpace(stderr.String())
		if details == "" {
			details = err.Error()
		}
		return dataNodeReuseProbe{
			IP:      ip,
			State:   dataNodeReuseUnreadable,
			Details: fmt.Sprintf("probe failed: %s", details),
		}
	}
	fields := strings.SplitN(strings.TrimSpace(stdout.String()), "|", 3)
	if len(fields) < 3 {
		return dataNodeReuseProbe{
			IP:      ip,
			State:   dataNodeReuseUnreadable,
			Details: fmt.Sprintf("unexpected probe output: %q", strings.TrimSpace(stdout.String())),
		}
	}
	switch fields[0] {
	case "fresh":
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseFresh, Details: fields[2]}
	case "partial":
		return dataNodeReuseProbe{IP: ip, State: dataNodeReusePartial, ClusterID: strings.TrimSpace(fields[1]), Details: fields[2]}
	case "unreadable":
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseUnreadable, Details: fields[2]}
	case "found":
		clusterID := strings.TrimSpace(fields[1])
		if clusterID == localClusterID {
			return dataNodeReuseProbe{IP: ip, State: dataNodeReuseCompatible, ClusterID: clusterID, Details: "clusterID matches current NameNode; managed runtime already exact"}
		}
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseConflicting, ClusterID: clusterID, Details: fmt.Sprintf("clusterID differs from current NameNode (%s)", localClusterID)}
	default:
		return dataNodeReuseProbe{
			IP:      ip,
			State:   dataNodeReuseUnreadable,
			Details: fmt.Sprintf("unexpected probe state: %q", fields[0]),
		}
	}
}

func (i *installer) reinitializeRemoteDataNodeState(ip string) error {
	target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
	baseHome := filepath.Join("/home", i.cfg.DataNodeUser)
	dataNodeDir := filepath.Join("/home", i.cfg.DataNodeUser, "hadoopdata", "datanode")
	parentDir := filepath.Join("/home", i.cfg.DataNodeUser, "hadoopdata")
	hadoopLogsDir := filepath.Join(baseHome, "hadoop", "logs")
	fmt.Printf("  - Reinitializing old DataNode state on %s\n", ip)
	cmd := fmt.Sprintf(`set -euo pipefail
DN_USER=%q
HADOOP_HOME=%q
DATA_DIR=%q
PARENT_DIR=%q
HADOOP_LOGS=%q

if [ -x "$HADOOP_HOME/bin/hdfs" ]; then
  "$HADOOP_HOME/bin/hdfs" --daemon stop datanode >/dev/null 2>&1 || true
fi
if [ -x "$HADOOP_HOME/bin/yarn" ]; then
  "$HADOOP_HOME/bin/yarn" --daemon stop nodemanager >/dev/null 2>&1 || true
fi

pkill -u "$DN_USER" -f 'org\.apache\.hadoop\.hdfs\.server\.datanode\.DataNode' >/dev/null 2>&1 || true
pkill -u "$DN_USER" -f 'org\.apache\.hadoop\.yarn\.server\.nodemanager\.NodeManager' >/dev/null 2>&1 || true
sleep 2

rm -rf "$DATA_DIR"
mkdir -p "$PARENT_DIR"
chmod -R 700 "$PARENT_DIR"

rm -rf /tmp/hadoop-"$DN_USER" /tmp/hadoop-"$DN_USER"-* "$HADOOP_LOGS"
mkdir -p "$HADOOP_LOGS"
`, i.cfg.DataNodeUser, i.hadoopHome, dataNodeDir, parentDir, hadoopLogsDir)
	op := func() error {
		return runRemoteBashCommand(nil, os.Stdout, os.Stderr, target, cmd)
	}
	if err := i.withRemoteSSHRecovery(ip, "reinitialize reused datanode state", op); err != nil {
		return fmt.Errorf("failed to reinitialize old DataNode state on %s: %w", ip, err)
	}
	return nil
}

func (i *installer) rewriteLocalConfigsForResolvedTargets() error {
	i.allowInstallManagedOverwrite = true
	defer func() {
		i.allowInstallManagedOverwrite = false
	}()
	if err := i.updateHostsBlockLocal(); err != nil {
		return err
	}
	if err := i.ensureHadoopConfigs(); err != nil {
		return err
	}
	return nil
}
