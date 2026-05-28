//go:build linux && gcloud_installer

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	pinnedHadoopVersion = "3.3.6"
	pinnedSparkArtifact = "spark-3.5.8-bin-hadoop3"
	pinnedJava11Package = "temurin-11-jdk"
	pinnedJava11Home    = "/usr/lib/jvm/temurin-11-jdk-amd64"

	defaultMinIOAccessKey  = "admin"
	defaultMinIOSecretKey  = "12345678"
	defaultUseKakaoMirror  = "yes"
	hostsBegin             = "# >>> FOXAI CLUSTER HOSTS >>>"
	hostsEnd               = "# <<< FOXAI CLUSTER HOSTS <<<"
	envBegin               = "# >>> FOXAI MANAGED ENV >>>"
	envEnd                 = "# <<< FOXAI MANAGED ENV <<<"
	sudoersTemplate        = "%s ALL=(ALL) NOPASSWD:ALL"
	remoteDNProbeBlockNote = "Full cluster hosts block is managed directly by the FoxAI installer on every node."
)

var (
	basePackagesNameNode = []string{
		"wget", "gpg", "ssh", "pdsh", "python3-venv", "python3-pip", "curl", "tar",
		"rsync", "unzip", "build-essential", "libffi-dev", "python3-dev", "libsasl2-dev",
		"libldap2-dev", "default-libmysqlclient-dev",
	}
	basePackagesDataNode = []string{
		"wget", "gpg", "ssh", "pdsh", "python3-venv", "python3-pip", "curl", "tar",
		"rsync", "unzip", "build-essential", "libffi-dev", "python3-dev", "libsasl2-dev",
		"libldap2-dev",
	}
)

type installerConfig struct {
	NameNodePrivateIP string
	DataNodeUser      string
	MinIOEndpoint     string
	MinIOAccessKey    string
	MinIOSecretKey    string
	UseKakaoMirror    bool
	ExistingNodeIPs   []string
	NewNodeIPs        []string
}

func (c installerConfig) AllDataNodeIPs() []string {
	ips := make([]string, 0, len(c.ExistingNodeIPs)+len(c.NewNodeIPs))
	ips = append(ips, c.ExistingNodeIPs...)
	ips = append(ips, c.NewNodeIPs...)
	return ips
}

func (c installerConfig) TotalDataNodes() int {
	return len(c.ExistingNodeIPs) + len(c.NewNodeIPs)
}

type installer struct {
	cfg                          installerConfig
	mode                         installerMode
	reader                       *bufio.Reader
	currentUser                  string
	baseHome                     string
	hadoopHome                   string
	sparkHome                    string
	java11Home                   string
	summary                      []summaryEntry
	allowInstallManagedOverwrite bool
}

type installerMode string

const (
	modeInstall       installerMode = "install"
	modeDryRun        installerMode = "dry-run"
	modePreflight     installerMode = "preflight"
	modeRepair        installerMode = "repair"
	modeReconcile     installerMode = "reconcile"
	modeRecommendOnly installerMode = "recommend-only"
)

type summaryStatus string

const (
	statusOK      summaryStatus = "OK"
	statusSkip    summaryStatus = "SKIP"
	statusWarn    summaryStatus = "WARN"
	statusDrift   summaryStatus = "DRIFT"
	statusFixed   summaryStatus = "FIXED"
	statusBlocked summaryStatus = "BLOCKED"
)

type summaryEntry struct {
	Target    string        `json:"target"`
	Component string        `json:"component"`
	Status    summaryStatus `json:"status"`
	Details   string        `json:"details"`
}

type runManifest struct {
	Timestamp string          `json:"timestamp"`
	Mode      installerMode   `json:"mode"`
	Success   bool            `json:"success"`
	Error     string          `json:"error,omitempty"`
	Inputs    installerConfig `json:"inputs"`
	Summary   []summaryEntry  `json:"summary"`
}

type repairSelection struct {
	FixLocalNameNode bool
	TargetIPs        []string
}

type hostSpec struct {
	Label    string
	CPUCores int
	MemoryGB int
	Source   string
}

type bootstrapDependency struct {
	Command string
	Package string
}

type dataNodeReuseState string

const (
	dataNodeReuseFresh       dataNodeReuseState = "fresh"
	dataNodeReuseCompatible  dataNodeReuseState = "compatible"
	dataNodeReuseConflicting dataNodeReuseState = "conflicting"
	dataNodeReuseUnreadable  dataNodeReuseState = "unreadable"
)

type dataNodeReuseProbe struct {
	IP        string
	State     dataNodeReuseState
	ClusterID string
	Details   string
}

type clusterRecommendation struct {
	NodeCount              int
	MinNodeCores           int
	MinNodeMemoryGB        int
	YARNMemoryMB           int
	YARNVcores             int
	HDFSReplication        int
	SparkExecutorInstances int
	SparkExecutorCores     int
	SparkExecutorMemoryMB  int
	SparkDriverMemoryMB    int
	SparkShufflePartitions int
}

type configState string

const (
	configStateExact   configState = "exact"
	configStateEmpty   configState = "empty"
	configStateMissing configState = "missing"
	configStateDrift   configState = "drift"
)

type hadoopXMLConfiguration struct {
	XMLName    xml.Name            `xml:"configuration"`
	Properties []hadoopXMLProperty `xml:"property"`
}

type hadoopXMLProperty struct {
	Name  string `xml:"name"`
	Value string `xml:"value"`
}

func main() {
	if runtime.GOOS != "linux" {
		fatal(errors.New("this installer only supports Linux"))
	}

	mode, err := parseMode(os.Args[1:])
	fatal(err)

	user := strings.TrimSpace(mustOutput("whoami"))
	inst := &installer{
		mode:        mode,
		reader:      bufio.NewReader(os.Stdin),
		currentUser: user,
		baseHome:    filepath.Join("/home", user),
		hadoopHome:  filepath.Join("/home", user, "hadoop"),
		sparkHome:   "/opt/spark",
		java11Home:  pinnedJava11Home,
	}

	runErr := inst.execute()
	if manifestErr := inst.writeManifest(runErr); manifestErr != nil {
		fmt.Fprintf(os.Stderr, "WARN: failed to write installer manifest: %v\n", manifestErr)
	}
	fatal(runErr)
}

func (i *installer) execute() error {
	if err := i.ensureBootstrapDependenciesForMode(); err != nil {
		return err
	}
	if err := i.collectInputs(); err != nil {
		return err
	}
	switch i.mode {
	case modeInstall:
		if err := i.requireCommands("python3", "ssh", "rsync", "wget", "tar"); err != nil {
			return err
		}
		if err := i.runNameNodeSetup(); err != nil {
			return err
		}
		if err := i.runAllDataNodeSetups(i.cfg.AllDataNodeIPs()); err != nil {
			return err
		}
		i.addSummary("cluster", "install", statusFixed, fmt.Sprintf("installed namenode and %d datanodes", i.cfg.TotalDataNodes()))
		return i.finishAfterMutation()
	case modeDryRun:
		return i.printDryRunPlan()
	case modePreflight:
		if err := i.requireCommands("python3", "ssh", "rsync", "wget", "tar"); err != nil {
			return err
		}
		return i.runPreflight()
	case modeRepair:
		if err := i.requireCommands("python3", "ssh", "rsync", "wget", "tar"); err != nil {
			return err
		}
		return i.runRepair()
	case modeReconcile:
		if err := i.requireCommands("python3", "ssh", "rsync", "wget", "tar"); err != nil {
			return err
		}
		return i.runReconcile()
	case modeRecommendOnly:
		return i.finishAndRecommend()
	default:
		return fmt.Errorf("unsupported installer mode: %s", i.mode)
	}
}

func (i *installer) ensureBootstrapDependenciesForMode() error {
	var deps []bootstrapDependency
	switch i.mode {
	case modeInstall, modePreflight, modeRepair, modeReconcile:
		deps = []bootstrapDependency{
			{Command: "python3", Package: "python3"},
			{Command: "ssh", Package: "openssh-client"},
			{Command: "rsync", Package: "rsync"},
			{Command: "wget", Package: "wget"},
			{Command: "tar", Package: "tar"},
		}
	default:
		return nil
	}
	return i.ensureBootstrapDependencies(deps)
}

func (i *installer) collectInputs() error {
	fmt.Println("=== FOXAI GCLOUD INSTALLER (GO) ===")
	fmt.Printf("Mode: %s\n", i.mode)
	if i.mode == modeDryRun {
		fmt.Println("Dry-run mode will collect inputs and print the execution plan only. No install commands will run.")
	}
	if i.mode == modeRecommendOnly {
		fmt.Println("Recommend-only mode will skip install steps and only collect hardware for Spark recommendations.")
	}
	if i.mode == modeRepair {
		fmt.Println("Repair mode will inspect the existing cluster, show FoxAI-managed drift, ask for confirmation, then patch the selected items.")
	}
	if i.mode == modeReconcile {
		fmt.Println("Reconcile mode will align an existing cluster to the requested FoxAI shape, including expansion with new DataNodes.")
	}
	fmt.Println("This installer preserves the current tested flow in one Linux-native entrypoint:")
	fmt.Println("1. NameNode setup locally")
	fmt.Println("2. DataNode setup remotely on each DataNode")
	fmt.Println()
	fmt.Printf("Pinned versions:\n")
	fmt.Printf("  - Hadoop: %s\n", pinnedHadoopVersion)
	fmt.Printf("  - Spark:  %s\n", pinnedSparkArtifact)
	fmt.Printf("  - Java 11 package: %s\n", pinnedJava11Package)
	fmt.Println()

	nameNodeIP, err := i.promptIP("Namenode private IP")
	if err != nil {
		return err
	}
	existingCount, err := i.promptInt("Number of EXISTING datanodes")
	if err != nil {
		return err
	}
	newCount, err := i.promptInt("Number of NEW datanodes")
	if err != nil {
		return err
	}

	existingIPs := make([]string, 0, existingCount)
	newIPs := make([]string, 0, newCount)
	if existingCount > 0 {
		fmt.Println()
		fmt.Println("=== EXISTING DATANODE IPs ===")
		for idx := 1; idx <= existingCount; idx++ {
			ip, err := i.promptIP(fmt.Sprintf("  Existing DN%d IP", idx))
			if err != nil {
				return err
			}
			existingIPs = append(existingIPs, ip)
		}
	}
	if newCount > 0 {
		fmt.Println("=== NEW DATANODE IPs ===")
		for idx := 1; idx <= newCount; idx++ {
			ip, err := i.promptIP(fmt.Sprintf("  New DN%d IP", idx))
			if err != nil {
				return err
			}
			newIPs = append(newIPs, ip)
		}
	}

	i.cfg = installerConfig{
		NameNodePrivateIP: nameNodeIP,
		ExistingNodeIPs:   existingIPs,
		NewNodeIPs:        newIPs,
	}

	derivedMinIOEndpoint := fmt.Sprintf("%s:9001", nameNodeIP)

	dataNodeUser, err := i.promptRequired("Datanode username")
	if err != nil {
		return err
	}
	minioEndpoint, err := i.promptOptional("MinIO endpoint", derivedMinIOEndpoint)
	if err != nil {
		return err
	}
	minioAccessKey, err := i.promptOptional("MinIO access key", defaultMinIOAccessKey)
	if err != nil {
		return err
	}
	minioSecretKey, err := i.promptOptional("MinIO secret key", defaultMinIOSecretKey)
	if err != nil {
		return err
	}
	useKakaoMirror, err := i.promptYesNoDefault("Apply Kakao apt mirror override from current source scripts", defaultUseKakaoMirror)
	if err != nil {
		return err
	}

	i.cfg.DataNodeUser = dataNodeUser
	i.cfg.MinIOEndpoint = minioEndpoint
	i.cfg.MinIOAccessKey = minioAccessKey
	i.cfg.MinIOSecretKey = minioSecretKey
	i.cfg.UseKakaoMirror = useKakaoMirror

	fmt.Println()
	fmt.Println("Collected inputs:")
	fmt.Printf("  - Namenode IP: %s\n", i.cfg.NameNodePrivateIP)
	fmt.Printf("  - Existing datanodes: %d\n", len(i.cfg.ExistingNodeIPs))
	fmt.Printf("  - New datanodes: %d\n", len(i.cfg.NewNodeIPs))
	fmt.Printf("  - Total datanodes: %d\n", i.cfg.TotalDataNodes())
	fmt.Printf("  - Datanode username: %s\n", i.cfg.DataNodeUser)
	fmt.Printf("  - MinIO endpoint: %s\n", i.cfg.MinIOEndpoint)
	fmt.Printf("  - MinIO access key: %s\n", i.cfg.MinIOAccessKey)
	fmt.Printf("  - MinIO secret key: [hidden]\n")
	if i.cfg.UseKakaoMirror {
		fmt.Println("  - Kakao mirror override: yes")
	} else {
		fmt.Println("  - Kakao mirror override: no")
	}
	fmt.Println()
	return nil
}

func (i *installer) printDryRunPlan() error {
	fmt.Println()
	fmt.Println("=== DRY-RUN PLAN ===")
	fmt.Println("No local files, remote files, sudo changes, rsync operations, or SSH mutations will be executed.")
	fmt.Println()
	fmt.Println("Derived cluster shape:")
	fmt.Printf("  - Namenode: %s\n", i.cfg.NameNodePrivateIP)
	fmt.Printf("  - Existing datanodes: %d\n", len(i.cfg.ExistingNodeIPs))
	fmt.Printf("  - New datanodes: %d\n", len(i.cfg.NewNodeIPs))
	fmt.Printf("  - Total datanodes: %d\n", i.cfg.TotalDataNodes())
	fmt.Printf("  - HDFS replication target: %d\n", minInt(3, maxInt(1, i.cfg.TotalDataNodes())))
	fmt.Printf("  - Hadoop install home: %s\n", i.hadoopHome)
	fmt.Printf("  - Spark install home: %s\n", i.sparkHome)
	fmt.Printf("  - Java 11 home: %s\n", i.java11Home)
	fmt.Println()

	fmt.Println("Would run on NameNode:")
	for _, step := range []string{
		"fresh-install guard",
		"SSH key generation / authorized_keys check",
		"manual SSH bootstrap prompt + passwordless SSH verification on all DataNodes",
		"NOPASSWD sudo check/config on all DataNodes",
		"optional Kakao apt mirror override",
		"base package install check",
		"python -> python3 symlink check",
		"Java 11 and Java 17 install check",
		"Hadoop install check",
		"Spark install check",
		".bashrc environment block check",
		"/etc/hosts managed block rewrite",
		"NameNode hadoopdata directory check",
		"Hadoop config file generation",
		"NameNode format check",
		"rsync Hadoop and Spark to DataNodes",
	} {
		fmt.Printf("  - %s\n", step)
	}
	fmt.Println()

	fmt.Println("Would run on each DataNode:")
	for idx, ip := range i.cfg.AllDataNodeIPs() {
		fmt.Printf("  - datanode%d (%s)\n", idx+1, ip)
		for _, step := range []string{
			"check current Linux user matches DataNode username",
			"check Hadoop path synced from NameNode",
			"check Spark path",
			"optional Kakao apt mirror override",
			"base package install check",
			"python -> python3 symlink check",
			"Java 11 install check",
			"/etc/hosts full managed block rewrite",
			"hadoop-env JAVA_HOME alignment",
			".bashrc managed environment block check",
			"DataNode hadoopdata directory check",
		} {
			fmt.Printf("      * %s\n", step)
		}
	}
	fmt.Println()
	fmt.Println("Dry-run complete.")
	fmt.Println("Use --recommend-only to test the hardware prompt and Spark recommendation flow without running installer steps.")
	return nil
}

func (i *installer) runPreflight() error {
	fmt.Println()
	fmt.Println("=== PREFLIGHT CHECKS ===")
	fmt.Println("Preflight is read-only. It will inspect the local host and remote DataNodes without mutating cluster state.")

	if err := i.reportLocalPreflight(); err != nil {
		return err
	}
	if err := i.reportRemotePreflight(); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Preflight complete.")
	fmt.Println("If any item is marked as drift or missing, full install should not be trusted on the current cluster until that gap is reviewed.")
	i.addSummary("cluster", "preflight", statusOK, "read-only inspection completed")
	i.printSummary()
	return nil
}

func (i *installer) reportLocalPreflight() error {
	section("LOCAL PREFLIGHT")
	fmt.Printf("  - Installer mode: %s\n", i.mode)
	fmt.Printf("  - Local user: %s\n", i.currentUser)
	fmt.Printf("  - Hadoop path: %s\n", i.hadoopHome)
	fmt.Printf("  - Spark path: %s\n", i.sparkHome)
	fmt.Printf("  - NameNode formatted: %s\n", yesNo(dirExists(filepath.Join(i.baseHome, "hadoopdata", "namenode", "current"))))
	fmt.Printf("  - SSH key present: %s\n", yesNo(fileExists(filepath.Join(i.baseHome, ".ssh", "id_rsa"))))

	if err := i.printLocalConfigStatus(); err != nil {
		return err
	}
	i.addSummary("namenode", "preflight", statusOK, "local config inspection completed")
	return nil
}

func (i *installer) reportRemotePreflight() error {
	section("REMOTE PREFLIGHT")
	for idx, ip := range i.cfg.AllDataNodeIPs() {
		target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		fmt.Printf("  - datanode%d (%s)\n", idx+1, ip)
		if err := runCommand("", nil, io.Discard, io.Discard, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", target, "true"); err != nil {
			i.addSummary(fmt.Sprintf("datanode%d", idx+1), "ssh", statusBlocked, "passwordless SSH unavailable")
			return fmt.Errorf("preflight ssh connectivity failed for %s: %w", ip, err)
		}
		fmt.Println("      * SSH connectivity: ok")
		i.addSummary(fmt.Sprintf("datanode%d", idx+1), "ssh", statusOK, "passwordless SSH reachable")
		if err := runCommand("", nil, io.Discard, io.Discard, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", target, "sudo", "-n", "true"); err == nil {
			fmt.Println("      * Passwordless sudo: yes")
			i.addSummary(fmt.Sprintf("datanode%d", idx+1), "sudo", statusOK, "passwordless sudo available")
		} else {
			fmt.Println("      * Passwordless sudo: no")
			i.addSummary(fmt.Sprintf("datanode%d", idx+1), "sudo", statusWarn, "passwordless sudo missing")
		}
		if err := runCommand("", nil, io.Discard, io.Discard, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", target, "test", "-x", filepath.Join(i.hadoopHome, "bin", "hdfs")); err == nil {
			fmt.Println("      * Hadoop present: yes")
			i.addSummary(fmt.Sprintf("datanode%d", idx+1), "hadoop", statusOK, "binary present")
		} else {
			fmt.Println("      * Hadoop present: no")
			i.addSummary(fmt.Sprintf("datanode%d", idx+1), "hadoop", statusDrift, "binary missing")
		}
		if err := runCommand("", nil, io.Discard, io.Discard, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", target, "test", "-x", filepath.Join(i.sparkHome, "bin", "spark-submit")); err == nil {
			fmt.Println("      * Spark present: yes")
			i.addSummary(fmt.Sprintf("datanode%d", idx+1), "spark", statusOK, "binary present")
		} else {
			fmt.Println("      * Spark present: no")
			i.addSummary(fmt.Sprintf("datanode%d", idx+1), "spark", statusWarn, "binary missing")
		}
	}
	return nil
}

func (i *installer) runtimeShellCommand(command string) string {
	return fmt.Sprintf(
		"export JAVA_HOME=%q; export HADOOP_HOME=%q; export SPARK_HOME=%q; export HADOOP_CONF_DIR=\"$HADOOP_HOME/etc/hadoop\"; export YARN_CONF_DIR=\"$HADOOP_HOME/etc/hadoop\"; export PATH=\"$PATH:$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin\"; export HADOOP_SSH_OPTS='-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10'; export PDSH_RCMD_TYPE=ssh; %s",
		i.java11Home,
		i.hadoopHome,
		i.sparkHome,
		command,
	)
}

func (i *installer) runVerificationSummary() error {
	section("VERIFICATION")
	if fileExists(filepath.Join(i.hadoopHome, "bin", "hdfs")) {
		var stdout bytes.Buffer
		if err := runCommand("", nil, &stdout, os.Stderr, filepath.Join(i.hadoopHome, "bin", "hdfs"), "getconf", "-confKey", "dfs.replication"); err == nil {
			value := strings.TrimSpace(stdout.String())
			fmt.Printf("  - dfs.replication: %s\n", value)
			i.addSummary("namenode", "dfs.replication", statusOK, value)
		} else {
			fmt.Println("  - dfs.replication: unavailable")
			i.addSummary("namenode", "dfs.replication", statusWarn, "could not read current value")
		}
	}
	var jpsOut bytes.Buffer
	if err := runCommand("", nil, &jpsOut, os.Stderr, "bash", "-lc", i.runtimeShellCommand("jps")); err == nil {
		jpsText := strings.TrimSpace(jpsOut.String())
		fmt.Println("  - jps:")
		for _, line := range strings.Split(jpsText, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			fmt.Printf("      * %s\n", line)
		}
		i.addSummary("namenode", "jps", statusOK, "captured local JVM process list")
	} else {
		i.addSummary("namenode", "jps", statusWarn, "jps command failed")
	}
	var yarnOut bytes.Buffer
	if err := runCommand("", nil, &yarnOut, os.Stderr, "bash", "-lc", i.runtimeShellCommand("yarn node -list")); err == nil {
		lines := strings.Split(strings.TrimSpace(yarnOut.String()), "\n")
		if len(lines) > 0 {
			fmt.Println("  - yarn node -list:")
			for _, line := range lines {
				if strings.TrimSpace(line) == "" {
					continue
				}
				fmt.Printf("      * %s\n", line)
			}
		}
		i.addSummary("namenode", "yarn", statusOK, "captured yarn node list")
	} else {
		i.addSummary("namenode", "yarn", statusWarn, "yarn node -list failed")
	}
	return nil
}

func (i *installer) runNameNodeSetup() error {
	originalTargets := append([]string(nil), i.cfg.AllDataNodeIPs()...)
	if err := i.ensureFreshInstallIntent(); err != nil {
		return err
	}
	if err := i.ensureLocalSSHKey(); err != nil {
		return err
	}
	if err := i.copySSHKeyToAllDataNodes(); err != nil {
		return err
	}
	if err := i.ensureDataNodesNoPasswordSudo(); err != nil {
		return err
	}
	if err := i.ensureRemoteBasePackages(i.cfg.AllDataNodeIPs()); err != nil {
		return err
	}
	if err := i.applyAptMirrorIfEnabledLocal(); err != nil {
		return err
	}
	if err := i.ensureBasePackages(basePackagesNameNode); err != nil {
		return err
	}
	if err := i.ensurePythonSymlinkLocal(); err != nil {
		return err
	}
	if err := i.ensureJavaLocal(pinnedJava11Package, i.java11Home, "JAVA 11"); err != nil {
		return err
	}
	if err := i.ensureHadoop(); err != nil {
		return err
	}
	if err := i.ensureSpark(); err != nil {
		return err
	}
	if err := i.ensureBashrc(); err != nil {
		return err
	}
	if err := i.updateHostsBlockLocal(); err != nil {
		return err
	}
	if err := i.ensureNameNodeDataDir(); err != nil {
		return err
	}
	if err := i.ensureHadoopConfigs(); err != nil {
		return err
	}
	if err := i.ensureNameNodeFormatted(); err != nil {
		return err
	}
	if _, err := i.resolveReusedDataNodesForInstall(originalTargets); err != nil {
		return err
	}
	if err := i.rewriteLocalConfigsForResolvedTargets(); err != nil {
		return err
	}
	if err := i.syncConfigsToDataNodes(i.cfg.AllDataNodeIPs()); err != nil {
		return err
	}
	return nil
}

func (i *installer) ensureFreshInstallIntent() error {
	section("INSTALL MODE GUARD")
	if i.cfg.TotalDataNodes() <= 0 {
		return fmt.Errorf("fresh install requires at least one datanode")
	}
	if dirExists(filepath.Join(i.baseHome, "hadoopdata", "namenode", "current")) {
		if len(i.cfg.ExistingNodeIPs) > 0 {
			fmt.Println("  - Existing datanodes were provided; treating install mode as cluster convergence on the current namenode")
		}
		fmt.Println("  - Namenode already formatted; treating install mode as a resumable run")
		return nil
	}
	if len(i.cfg.ExistingNodeIPs) > 0 {
		fmt.Println("  - Existing datanodes were provided alongside new datanodes; install mode will converge the requested cluster shape")
		return nil
	}
	fmt.Println("  - Fresh-install guard passed")
	return nil
}

func (i *installer) runAllDataNodeSetups(targetIPs []string) error {
	for _, ip := range targetIPs {
		nodeNumber := i.nodeNumberForIP(ip)
		if nodeNumber <= 0 {
			return fmt.Errorf("could not derive datanode number for %s from the current cluster shape", ip)
		}
		if err := i.runRemoteDataNodeSetup(ip, nodeNumber); err != nil {
			return err
		}
	}
	return nil
}

func (i *installer) nodeNumberForIP(ip string) int {
	for idx, candidate := range i.cfg.AllDataNodeIPs() {
		if candidate == ip {
			return idx + 1
		}
	}
	return 0
}

func (i *installer) nodeLabelForIP(ip string) string {
	if n := i.nodeNumberForIP(ip); n > 0 {
		return fmt.Sprintf("datanode%d", n)
	}
	return ip
}

func (i *installer) shouldAllowManagedOverwrite() bool {
	return i.mode == modeRepair || i.mode == modeReconcile || i.allowInstallManagedOverwrite
}

func (i *installer) shouldAllowExistingClusterMutation() bool {
	return i.mode == modeRepair || i.mode == modeReconcile
}

func (i *installer) addSummary(target, component string, status summaryStatus, details string) {
	i.summary = append(i.summary, summaryEntry{
		Target:    target,
		Component: component,
		Status:    status,
		Details:   details,
	})
}

func (i *installer) writeManifest(runErr error) error {
	dir := filepath.Join(i.baseHome, ".foxai-gcloud-installer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	manifest := runManifest{
		Timestamp: time.Now().Format(time.RFC3339),
		Mode:      i.mode,
		Success:   runErr == nil,
		Inputs:    i.cfg,
		Summary:   i.summary,
	}
	if runErr != nil {
		manifest.Error = runErr.Error()
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "last-run.json"), data, 0o644)
}

func (i *installer) printSummary() {
	if len(i.summary) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("=== SUMMARY ===")
	for _, item := range i.summary {
		fmt.Printf("  - [%s] %s / %s", item.Status, item.Target, item.Component)
		if item.Details != "" {
			fmt.Printf(": %s", item.Details)
		}
		fmt.Println()
	}
}

func (i *installer) parseNodeSelection(candidates []string) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	fmt.Println("Select target DataNodes to patch/reconcile.")
	fmt.Println("  - Enter `all` to include every listed DataNode")
	fmt.Println("  - Enter comma-separated node numbers like `1,3,5`")
	for idx, ip := range candidates {
		fmt.Printf("    %d. %s\n", idx+1, ip)
	}
	for {
		value, err := i.readPrompt("Target DataNodes [all]: ")
		if err != nil {
			return nil, err
		}
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, "all") {
			out := make([]string, len(candidates))
			copy(out, candidates)
			return out, nil
		}
		parts := strings.Split(value, ",")
		seen := make(map[int]bool)
		out := make([]string, 0, len(parts))
		valid := true
		for _, raw := range parts {
			raw = strings.TrimSpace(raw)
			n, convErr := strconv.Atoi(raw)
			if convErr != nil || n < 1 || n > len(candidates) {
				valid = false
				break
			}
			if seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, candidates[n-1])
		}
		if valid && len(out) > 0 {
			return out, nil
		}
		fmt.Println("  Enter `all` or valid comma-separated node numbers.")
	}
}

func (i *installer) confirmProceed(prompt string) (bool, error) {
	return i.promptYesNoDefault(prompt, "no")
}

func (i *installer) runLocalBootstrapWithoutFreshGuard() error {
	if err := i.ensureLocalSSHKey(); err != nil {
		return err
	}
	if err := i.copySSHKeyToAllDataNodes(); err != nil {
		return err
	}
	if err := i.ensureDataNodesNoPasswordSudo(); err != nil {
		return err
	}
	if err := i.ensureRemoteBasePackages(i.cfg.AllDataNodeIPs()); err != nil {
		return err
	}
	if err := i.applyAptMirrorIfEnabledLocal(); err != nil {
		return err
	}
	if err := i.ensureBasePackages(basePackagesNameNode); err != nil {
		return err
	}
	if err := i.ensurePythonSymlinkLocal(); err != nil {
		return err
	}
	if err := i.ensureJavaLocal(pinnedJava11Package, i.java11Home, "JAVA 11"); err != nil {
		return err
	}
	if err := i.ensureHadoop(); err != nil {
		return err
	}
	if err := i.ensureSpark(); err != nil {
		return err
	}
	if err := i.ensureBashrc(); err != nil {
		return err
	}
	if err := i.updateHostsBlockLocal(); err != nil {
		return err
	}
	if err := i.ensureNameNodeDataDir(); err != nil {
		return err
	}
	if err := i.ensureHadoopConfigs(); err != nil {
		return err
	}
	if err := i.ensureNameNodeFormatted(); err != nil {
		return err
	}
	return nil
}

func (i *installer) runRepair() error {
	if len(i.cfg.ExistingNodeIPs) == 0 {
		return fmt.Errorf("repair mode expects existing datanodes. Provide at least one existing datanode IP")
	}
	if len(i.cfg.NewNodeIPs) > 0 {
		return fmt.Errorf("repair mode is for the current cluster only. Use reconcile mode if you are adding new datanodes")
	}
	section("REPAIR PLAN")
	if err := i.printLocalConfigStatus(); err != nil {
		return err
	}
	if err := i.reportRemotePreflight(); err != nil {
		return err
	}
	targetIPs, err := i.parseNodeSelection(i.cfg.ExistingNodeIPs)
	if err != nil {
		return err
	}
	ok, err := i.confirmProceed("Apply FoxAI repair actions to the selected items")
	if err != nil {
		return err
	}
	if !ok {
		i.addSummary("cluster", "repair", statusSkip, "user aborted repair before mutation")
		i.printSummary()
		return nil
	}
	if err := i.runLocalBootstrapWithoutFreshGuard(); err != nil {
		return err
	}
	if err := i.syncConfigsToDataNodes(targetIPs); err != nil {
		return err
	}
	if err := i.runAllDataNodeSetups(targetIPs); err != nil {
		return err
	}
	i.addSummary("cluster", "repair", statusFixed, fmt.Sprintf("repaired namenode and %d datanodes", len(targetIPs)))
	return i.finishAfterMutation()
}

func (i *installer) runReconcile() error {
	if i.cfg.TotalDataNodes() == 0 {
		return fmt.Errorf("reconcile mode requires at least one datanode")
	}
	section("RECONCILE PLAN")
	if err := i.printLocalConfigStatus(); err != nil {
		return err
	}
	if err := i.reportRemotePreflight(); err != nil {
		return err
	}
	targetIPs, err := i.parseNodeSelection(i.cfg.AllDataNodeIPs())
	if err != nil {
		return err
	}
	ok, err := i.confirmProceed("Apply FoxAI reconcile actions to the selected nodes and current cluster shape")
	if err != nil {
		return err
	}
	if !ok {
		i.addSummary("cluster", "reconcile", statusSkip, "user aborted reconcile before mutation")
		i.printSummary()
		return nil
	}
	if err := i.runLocalBootstrapWithoutFreshGuard(); err != nil {
		return err
	}
	if err := i.syncConfigsToDataNodes(targetIPs); err != nil {
		return err
	}
	if err := i.runAllDataNodeSetups(targetIPs); err != nil {
		return err
	}
	i.addSummary("cluster", "reconcile", statusFixed, fmt.Sprintf("reconciled namenode and %d datanodes", len(targetIPs)))
	return i.finishAfterMutation()
}

func (i *installer) finishAfterMutation() error {
	fmt.Println()
	fmt.Println("=== FOXAI CLUSTER ACTION DONE ===")
	startServices, err := i.promptYesNoDefault("Run HDFS and YARN start commands now for verification", "no")
	if err != nil {
		return err
	}
	if startServices {
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", i.runtimeShellCommand("start-dfs.sh && start-yarn.sh")); err != nil {
			i.addSummary("namenode", "service start", statusWarn, err.Error())
			return err
		}
		i.addSummary("namenode", "service start", statusOK, "start-dfs.sh and start-yarn.sh completed")
	}
	if err := i.runVerificationSummary(); err != nil {
		return err
	}
	i.printSummary()
	return i.finishAndRecommend()
}

func (i *installer) finishAndRecommend() error {
	fmt.Println()
	fmt.Println("=== FOXAI INSTALLER DONE ===")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Start HDFS:  start-dfs.sh")
	fmt.Println("  2. Start YARN:  start-yarn.sh")
	fmt.Println("  3. Verify:      yarn node -list")
	fmt.Println()
	fmt.Println("Captured MinIO defaults for future config alignment:")
	fmt.Printf("  - Endpoint: %s\n", i.cfg.MinIOEndpoint)
	fmt.Printf("  - Access key: %s\n", i.cfg.MinIOAccessKey)
	fmt.Println("  - Secret key: [hidden]")
	fmt.Println()

	mode, err := i.promptChoice(
		"Collect hardware specs for recommended Spark configs",
		[]string{
			"1. Auto probe hardware on NameNode + DataNodes",
			"2. Manually enter hardware specs",
		},
	)
	if err != nil {
		return err
	}

	var specs []hostSpec
	switch mode {
	case 1:
		specs, err = i.autoProbeHardware()
	case 2:
		specs, err = i.manualEnterHardware()
	default:
		return fmt.Errorf("unsupported hardware collection mode: %d", mode)
	}
	if err != nil {
		return err
	}

	rec := deriveRecommendations(specs)
	i.printRecommendationSummary(specs, rec)
	return nil
}

func (i *installer) ensureLocalSSHKey() error {
	section("SSH KEY")
	homeSSH := filepath.Join(i.baseHome, ".ssh")
	privateKey := filepath.Join(homeSSH, "id_rsa")
	publicKey := privateKey + ".pub"
	authorizedKeys := filepath.Join(homeSSH, "authorized_keys")

	if fileExists(privateKey) {
		fmt.Println("  - SSH key exists")
	} else {
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "ssh-keygen", "-t", "rsa", "-N", "", "-f", privateKey); err != nil {
			return err
		}
		fmt.Println("  - SSH key generated")
	}

	if err := os.MkdirAll(homeSSH, 0o700); err != nil {
		return err
	}
	if !fileExists(authorizedKeys) {
		if err := os.WriteFile(authorizedKeys, nil, 0o600); err != nil {
			return err
		}
	}
	if err := os.Chmod(authorizedKeys, 0o600); err != nil {
		return err
	}

	pubKeyBytes, err := os.ReadFile(publicKey)
	if err != nil {
		return err
	}
	pubKey := strings.TrimSpace(string(pubKeyBytes))
	authBytes, err := os.ReadFile(authorizedKeys)
	if err != nil {
		return err
	}
	if !strings.Contains(string(authBytes), pubKey) {
		f, err := os.OpenFile(authorizedKeys, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := fmt.Fprintln(f, pubKey); err != nil {
			return err
		}
		fmt.Println("  - Key added")
	}
	return nil
}

func (i *installer) copySSHKeyToAllDataNodes() error {
	section("MANUAL SSH BOOTSTRAP (ALL DNs)")
	failedIPs := i.passwordlessSSHFailures(i.cfg.AllDataNodeIPs())
	if len(failedIPs) == 0 {
		fmt.Println("  - Passwordless SSH already verified on all DataNodes")
		return nil
	}
	publicKeyPath := filepath.Join(i.baseHome, ".ssh", "id_rsa.pub")
	pubKeyBytes, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key %s: %w", publicKeyPath, err)
	}
	pubKey := strings.TrimSpace(string(pubKeyBytes))

	fmt.Printf("Passwordless SSH is still missing for: %s\n", strings.Join(failedIPs, ", "))
	fmt.Println("Run the following on EACH DataNode terminal before continuing:")
	fmt.Println("  mkdir -p ~/.ssh")
	fmt.Println("  chmod 700 ~/.ssh")
	fmt.Println("  touch ~/.ssh/authorized_keys")
	fmt.Println("  chmod 600 ~/.ssh/authorized_keys")
	fmt.Println("  nano ~/.ssh/authorized_keys")
	fmt.Println("Paste this NameNode public key into the DataNode authorized_keys file:")
	fmt.Println()
	fmt.Println(pubKey)
	fmt.Println()
	fmt.Println("After pasting the key on every DataNode, save the file and return here.")
	if _, err := i.readPrompt("Press Enter to verify passwordless SSH to all DataNodes..."); err != nil {
		return err
	}

	failedIPs = i.passwordlessSSHFailures(i.cfg.AllDataNodeIPs())
	for _, ip := range i.cfg.AllDataNodeIPs() {
		if containsString(failedIPs, ip) {
			fmt.Printf("  - %s: passwordless SSH not ready\n", ip)
			continue
		}
		fmt.Printf("  - %s: passwordless SSH verified\n", ip)
	}
	if len(failedIPs) > 0 {
		return fmt.Errorf("passwordless SSH is still unavailable for: %s", strings.Join(failedIPs, ", "))
	}
	return nil
}

func (i *installer) passwordlessSSHFailures(targetIPs []string) []string {
	failedIPs := make([]string, 0)
	for _, ip := range targetIPs {
		target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		if err := runCommand("", nil, io.Discard, io.Discard, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", target, "true"); err != nil {
			failedIPs = append(failedIPs, ip)
		}
	}
	return failedIPs
}

func (i *installer) ensureDataNodesNoPasswordSudo() error {
	section("NOPASSWD (ALL DNs)")
	for _, ip := range i.cfg.AllDataNodeIPs() {
		target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		fmt.Printf("  - Checking %s\n", ip)
		err := runCommand("", nil, io.Discard, io.Discard, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", target, "sudo", "-n", "true")
		if err == nil {
			fmt.Println("    * Already NOPASSWD")
			continue
		}
		fmt.Println("    * Configuring NOPASSWD (enter password if prompted)")
		line := fmt.Sprintf(sudoersTemplate, i.cfg.DataNodeUser)
		remoteCmd := fmt.Sprintf("echo %q | sudo tee /etc/sudoers.d/%s >/dev/null", line, i.cfg.DataNodeUser)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "ssh", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-tt", target, remoteCmd); err != nil {
			return fmt.Errorf("failed to configure NOPASSWD on %s: %w", ip, err)
		}
	}
	return nil
}

func (i *installer) ensureRemoteBasePackages(targetIPs []string) error {
	section("REMOTE BASE PACKAGES (ALL DNs)")
	if len(targetIPs) == 0 {
		fmt.Println("  - No DataNodes requested")
		return nil
	}
	packages := strings.Join(basePackagesDataNode, " ")
	for _, ip := range targetIPs {
		target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		fmt.Printf("  - Checking %s\n", ip)
		cmd := fmt.Sprintf(`MISSING=""; for p in %s; do dpkg -s "$p" >/dev/null 2>&1 || MISSING="$MISSING $p"; done; if [ -n "$MISSING" ]; then sudo apt-get update && sudo apt-get install -y $MISSING; echo "__FOXAI_REMOTE_INSTALLED__$MISSING"; else echo "__FOXAI_REMOTE_ALREADY__"; fi; if [ ! -e /usr/bin/python ]; then sudo ln -sf /usr/bin/python3 /usr/bin/python; fi`, packages)
		if err := runCommand("", nil, os.Stdout, os.Stderr, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-T", target, "bash", "-lc", cmd); err != nil {
			return fmt.Errorf("failed to ensure remote base packages on %s: %w", ip, err)
		}
	}
	return nil
}

func (i *installer) applyAptMirrorIfEnabledLocal() error {
	section("APT MIRROR")
	if !i.cfg.UseKakaoMirror {
		fmt.Println("  - Skipped premise-specific Kakao mirror override")
		return nil
	}
	hostsFile := "/etc/apt/sources.list"
	data, err := os.ReadFile(hostsFile)
	if err == nil && strings.Contains(string(data), "mirror.kakao.com/ubuntu") {
		fmt.Println("  - Already configured")
		return nil
	}
	cmd := `sed -i 's|http://archive.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list && sed -i 's|http://security.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list`
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", cmd); err != nil {
		return err
	}
	fmt.Println("  - Configured")
	return nil
}

func (i *installer) ensureBasePackages(packages []string) error {
	section("BASE PACKAGES")
	missing := make([]string, 0)
	for _, pkg := range packages {
		if err := runCommand("", nil, io.Discard, io.Discard, "dpkg", "-s", pkg); err != nil {
			missing = append(missing, pkg)
		}
	}
	if len(missing) == 0 {
		fmt.Println("  - Already installed")
		return nil
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "apt", "update"); err != nil {
		return err
	}
	args := append([]string{"apt", "install", "-y"}, missing...)
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, args...); err != nil {
		return err
	}
	fmt.Printf("  - Installed:%s\n", " "+strings.Join(missing, " "))
	return nil
}

func (i *installer) ensurePythonSymlinkLocal() error {
	if fileExists("/usr/bin/python") {
		return nil
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "ln", "-sf", "/usr/bin/python3", "/usr/bin/python"); err != nil {
		return err
	}
	fmt.Println("  - Linked python -> python3")
	return nil
}

func (i *installer) ensureJavaLocal(pkg, home, title string) error {
	section(title)
	if dirExists(home) {
		fmt.Println("  - Already installed")
		return nil
	}
	if err := i.ensureAdoptiumRepoLocal(); err != nil {
		return err
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "apt", "update"); err != nil {
		return err
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "apt", "install", "-y", pkg); err != nil {
		return err
	}
	fmt.Println("  - Installed")
	if !dirExists(home) {
		return fmt.Errorf("java home invalid after install: %s", home)
	}
	return nil
}

func (i *installer) ensureAdoptiumRepoLocal() error {
	if !fileExists("/usr/share/keyrings/adoptium.gpg") {
		cmd := "wget -4 -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | gpg --dearmor -o /usr/share/keyrings/adoptium.gpg"
		if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", cmd); err != nil {
			return err
		}
	}
	if fileExists("/etc/apt/sources.list.d/adoptium.list") {
		return nil
	}
	codename := "bookworm"
	if content, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(line, "VERSION_CODENAME=") {
				value := strings.TrimPrefix(line, "VERSION_CODENAME=")
				value = strings.Trim(value, `"`)
				if strings.TrimSpace(value) != "" {
					codename = strings.TrimSpace(value)
				}
			}
		}
	}
	line := fmt.Sprintf("deb [signed-by=/usr/share/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb %s main", codename)
	cmd := fmt.Sprintf("echo %q > /etc/apt/sources.list.d/adoptium.list", line)
	return runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", cmd)
}

func (i *installer) ensureHadoop() error {
	section("HADOOP")
	if fileExists(filepath.Join(i.hadoopHome, "bin", "hdfs")) {
		fmt.Println("  - Already installed")
		return nil
	}
	tarball := fmt.Sprintf("hadoop-%s.tar.gz", pinnedHadoopVersion)
	url := fmt.Sprintf("https://dlcdn.apache.org/hadoop/common/hadoop-%s/%s", pinnedHadoopVersion, tarball)
	if err := runCommand(i.baseHome, os.Stdin, os.Stdout, os.Stderr, "wget", "-4", url); err != nil {
		return err
	}
	if err := runCommand(i.baseHome, os.Stdin, os.Stdout, os.Stderr, "tar", "-xzf", tarball); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(i.baseHome, "hadoop-"+pinnedHadoopVersion), i.hadoopHome); err != nil {
		return err
	}
	fmt.Println("  - Installed")
	return nil
}

func (i *installer) ensureSpark() error {
	section("SPARK")
	if fileExists(filepath.Join(i.sparkHome, "bin", "spark-submit")) {
		fmt.Println("  - Already installed")
		return nil
	}
	tarball := pinnedSparkArtifact + ".tgz"
	url := fmt.Sprintf("https://dlcdn.apache.org/spark/spark-3.5.8/%s", tarball)
	if err := runCommand(i.baseHome, os.Stdin, os.Stdout, os.Stderr, "wget", "-4", url); err != nil {
		return err
	}
	if err := runCommand(i.baseHome, os.Stdin, os.Stdout, os.Stderr, "tar", "-xzf", tarball); err != nil {
		return err
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "mv", filepath.Join(i.baseHome, pinnedSparkArtifact), i.sparkHome); err != nil {
		return err
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "chown", "-R", fmt.Sprintf("%s:%s", i.currentUser, i.currentUser), i.sparkHome); err != nil {
		return err
	}
	fmt.Println("  - Installed")
	return nil
}

func (i *installer) ensureBashrc() error {
	section("BASHRC")
	bashrcPath := filepath.Join(i.baseHome, ".bashrc")
	content, err := os.ReadFile(bashrcPath)
	if err != nil {
		return err
	}
	desiredBlock := i.desiredEnvBlock()
	blockToApply := desiredBlock
	state, details, err := evaluateManagedBlockFile(bashrcPath, envBegin, envEnd, desiredBlock)
	if err != nil {
		return err
	}
	if state == configStateExact {
		fmt.Println("  - Already configured")
		return nil
	}
	if state == configStateDrift && i.mode == modeInstall {
		choice, custom, err := i.resolveInstallDrift("~/.bashrc managed block", details)
		if err != nil {
			return err
		}
		switch choice {
		case 1:
			return fmt.Errorf("install stopped by user after ~/.bashrc managed block drift")
		case 3:
			blockToApply = custom
		case 4:
			fmt.Println("  - Skipped drifted managed block")
			return nil
		}
	}
	updated, err := upsertManagedBlockText(string(content), envBegin, envEnd, blockToApply)
	if err != nil {
		return err
	}
	if err := os.WriteFile(bashrcPath, []byte(updated), 0o644); err != nil {
		return err
	}
	if err := runCommand("", nil, io.Discard, io.Discard, "bash", "-lc", "source ~/.bashrc >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("failed to source ~/.bashrc after update: %w", err)
	}
	if state == configStateDrift {
		fmt.Println("  - Updated drifted managed block")
	} else {
		fmt.Println("  - Configured")
	}
	return nil
}

func (i *installer) updateHostsBlockLocal() error {
	section("/etc/hosts")
	desired := i.desiredLocalHostsBlock()
	blockToApply := desired
	state, details, err := evaluateManagedBlockFile("/etc/hosts", hostsBegin, hostsEnd, desired)
	if err != nil {
		return err
	}
	if state == configStateExact {
		fmt.Println("  - Already configured")
		return nil
	}
	if state == configStateDrift && i.shouldAllowManagedOverwrite() {
	} else if state == configStateDrift && i.mode == modeInstall {
		choice, custom, err := i.resolveInstallDrift("/etc/hosts managed block", details)
		if err != nil {
			return err
		}
		switch choice {
		case 1:
			return fmt.Errorf("install stopped by user after /etc/hosts managed block drift")
		case 3:
			blockToApply = custom
		case 4:
			fmt.Println("  - Skipped drifted managed block")
			return nil
		}
	}

	tempFile, err := os.CreateTemp("", "foxai-hosts-block-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())
	if _, err := tempFile.WriteString(blockToApply); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	script := `
import sys
block_file = sys.argv[1]
hosts_path = "/etc/hosts"
with open(block_file) as f:
    new_block = f.read()
with open(hosts_path) as f:
    text = f.read()
begin = "# >>> FOXAI CLUSTER HOSTS >>>"
end = "# <<< FOXAI CLUSTER HOSTS <<<"
start = text.find(begin)
if start != -1:
    end_idx = text.find(end, start)
    if end_idx != -1:
        end_idx += len(end)
        text = text[:start] + text[end_idx:]
if text and not text.endswith("\n"):
    text += "\n"
text += new_block
with open(hosts_path, "w") as f:
    f.write(text)
print("done")
`
	if err := runPythonScriptWithSudo([]string{tempFile.Name()}, script); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Updated drifted managed block")
	} else {
		fmt.Println("  - Updated")
	}
	return nil
}

func (i *installer) ensureNameNodeDataDir() error {
	section("HADOOP DATA DIR")
	target := filepath.Join(i.baseHome, "hadoopdata", "namenode")
	if dirExists(target) {
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "chmod", "-R", "700", filepath.Join(i.baseHome, "hadoopdata")); err != nil {
			return err
		}
		fmt.Println("  - Already exists")
		return nil
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "chmod", "-R", "700", filepath.Join(i.baseHome, "hadoopdata")); err != nil {
		return err
	}
	fmt.Println("  - Created")
	return nil
}

func (i *installer) ensureHadoopConfigs() error {
	if err := i.ensureCoreSite(); err != nil {
		return err
	}
	if err := i.ensureHdfsSite(); err != nil {
		return err
	}
	if err := i.ensureWorkersFile(); err != nil {
		return err
	}
	if err := i.ensureMapredSite(); err != nil {
		return err
	}
	if err := i.ensureYarnSite(); err != nil {
		return err
	}
	if err := i.ensureHadoopEnv(); err != nil {
		return err
	}
	return nil
}

func (i *installer) ensureCoreSite() error {
	section("CORE-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "core-site.xml")
	content := i.desiredCoreSiteContent()
	state, details, err := evaluateHadoopXMLFile(target, i.expectedCoreSiteProperties())
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("core-site.xml", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after core-site.xml drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("core-site.xml", details)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Overwrote drifted config")
	} else {
		fmt.Println("  - Configured")
	}
	return nil
}

func (i *installer) ensureHdfsSite() error {
	section("HDFS-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "hdfs-site.xml")
	content := i.desiredHdfsSiteContent()
	state, details, err := evaluateHadoopXMLFile(target, i.expectedHdfsSiteProperties())
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("hdfs-site.xml", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after hdfs-site.xml drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("hdfs-site.xml", details)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Printf("  - Overwrote drifted config (replication=%d)\n", minInt(3, maxInt(1, i.cfg.TotalDataNodes())))
	} else {
		fmt.Printf("  - Configured (replication=%d)\n", minInt(3, maxInt(1, i.cfg.TotalDataNodes())))
	}
	return nil
}

func (i *installer) ensureWorkersFile() error {
	section("WORKERS FILE")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "workers")
	expected := i.expectedWorkers()
	content := strings.Join(expected, "\n") + "\n"
	state, details, err := evaluateWorkersFile(target, expected)
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("workers", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after workers drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("workers", details)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Printf("  - Overwrote drifted config (1-%d)\n", i.cfg.TotalDataNodes())
	} else {
		fmt.Printf("  - Configured (1-%d)\n", i.cfg.TotalDataNodes())
	}
	return nil
}

func (i *installer) ensureMapredSite() error {
	section("MAPRED-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "mapred-site.xml")
	content := i.desiredMapredSiteContent()
	state, details, err := evaluateHadoopXMLFile(target, i.expectedMapredSiteProperties())
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("mapred-site.xml", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after mapred-site.xml drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("mapred-site.xml", details)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Overwrote drifted config")
	} else {
		fmt.Println("  - Configured")
	}
	return nil
}

func (i *installer) ensureYarnSite() error {
	section("YARN-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "yarn-site.xml")
	content := i.desiredYarnSiteContent()
	state, details, err := evaluateHadoopXMLFile(target, i.expectedYarnSiteProperties())
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("yarn-site.xml", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after yarn-site.xml drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("yarn-site.xml", details)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Overwrote drifted config (memory=13312, vcores=14)")
	} else {
		fmt.Println("  - Configured (memory=13312, vcores=14)")
	}
	return nil
}

func (i *installer) ensureHadoopEnv() error {
	section("HADOOP-ENV.SH")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "hadoop-env.sh")
	flexibleJava := fmt.Sprintf("export JAVA_HOME=${JAVA_HOME:-%s}", i.java11Home)
	fixedJava := fmt.Sprintf("export JAVA_HOME=%s", i.java11Home)
	state, details, err := evaluateJavaEnvFile(target, []string{flexibleJava, fixedJava})
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("hadoop-env.sh", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after hadoop-env.sh drift")
			case 3:
				if err := os.WriteFile(target, []byte(custom), 0o644); err != nil {
					return err
				}
				fmt.Println("  - Replaced with custom content")
				return nil
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("hadoop-env.sh", details)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	current := string(data)
	switch {
	case strings.Contains(current, "# export JAVA_HOME="):
		current = strings.Replace(current, "# export JAVA_HOME=", flexibleJava, 1)
	default:
		if !strings.HasSuffix(current, "\n") {
			current += "\n"
		}
		current += flexibleJava + "\n"
	}
	if err := os.WriteFile(target, []byte(current), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Overwrote drifted JAVA_HOME")
	} else {
		fmt.Println("  - Updated")
	}
	return nil
}

func (i *installer) desiredCoreSiteContent() string {
	return `<configuration>
  <property>
    <name>fs.defaultFS</name>
    <value>hdfs://namenode:9000</value>
  </property>
</configuration>
`
}

func (i *installer) desiredHdfsSiteContent() string {
	replication := i.cfg.TotalDataNodes()
	if replication > 3 {
		replication = 3
	}
	return fmt.Sprintf(`<configuration>
  <property><name>dfs.replication</name><value>%d</value></property>
  <property><name>dfs.namenode.name.dir</name><value>file://%s/hadoopdata/namenode</value></property>
  <property><name>dfs.datanode.data.dir</name><value>file://%s/hadoopdata/datanode</value></property>
</configuration>
`, replication, i.baseHome, i.baseHome)
}

func (i *installer) desiredMapredSiteContent() string {
	return `<configuration>
<property>
<name>mapreduce.framework.name</name>
<value>yarn</value>
</property>
</configuration>
`
}

func (i *installer) desiredYarnSiteContent() string {
	return `<configuration>
<property>
<name>yarn.resourcemanager.hostname</name>
<value>namenode</value>
</property>
<property>
<name>yarn.nodemanager.aux-services</name>
<value>mapreduce_shuffle</value>
</property>
<property>
<name>yarn.nodemanager.resource.memory-mb</name>
<value>13312</value>
</property>
<property>
<name>yarn.scheduler.maximum-allocation-mb</name>
<value>13312</value>
</property>
<property>
<name>yarn.scheduler.maximum-allocation-vcores</name>
<value>14</value>
</property>
<property>
<name>yarn.nodemanager.resource.cpu-vcores</name>
<value>14</value>
</property>
</configuration>
`
}

func (i *installer) expectedCoreSiteProperties() map[string]string {
	return map[string]string{
		"fs.defaultFS": "hdfs://namenode:9000",
	}
}

func (i *installer) expectedHdfsSiteProperties() map[string]string {
	replication := i.cfg.TotalDataNodes()
	if replication > 3 {
		replication = 3
	}
	return map[string]string{
		"dfs.replication":       strconv.Itoa(replication),
		"dfs.namenode.name.dir": fmt.Sprintf("file://%s/hadoopdata/namenode", i.baseHome),
		"dfs.datanode.data.dir": fmt.Sprintf("file://%s/hadoopdata/datanode", i.baseHome),
	}
}

func (i *installer) expectedMapredSiteProperties() map[string]string {
	return map[string]string{
		"mapreduce.framework.name": "yarn",
	}
}

func (i *installer) expectedYarnSiteProperties() map[string]string {
	return map[string]string{
		"yarn.resourcemanager.hostname":            "namenode",
		"yarn.nodemanager.aux-services":            "mapreduce_shuffle",
		"yarn.nodemanager.resource.memory-mb":      "13312",
		"yarn.scheduler.maximum-allocation-mb":     "13312",
		"yarn.scheduler.maximum-allocation-vcores": "14",
		"yarn.nodemanager.resource.cpu-vcores":     "14",
	}
}

func (i *installer) expectedWorkers() []string {
	workers := make([]string, 0, i.cfg.TotalDataNodes())
	for idx := 1; idx <= i.cfg.TotalDataNodes(); idx++ {
		workers = append(workers, fmt.Sprintf("datanode%d", idx))
	}
	return workers
}

func (i *installer) ensureNameNodeFormatted() error {
	section("FORMAT NAMENODE")
	currentDir := filepath.Join(i.baseHome, "hadoopdata", "namenode", "current")
	if dirExists(currentDir) {
		fmt.Println("  - Already formatted")
		return nil
	}
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, filepath.Join(i.hadoopHome, "bin", "hdfs"), "namenode", "-format", "-force", "-nonInteractive"); err != nil {
		return err
	}
	fmt.Println("  - Formatted")
	return nil
}

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
	actionable := make([]dataNodeReuseProbe, 0)
	for _, ip := range originalTargets {
		probe := i.probeDataNodeReuse(ip, clusterID)
		probes = append(probes, probe)
		fmt.Printf("  - %s: %s", ip, probe.State)
		if probe.ClusterID != "" {
			fmt.Printf(" (clusterID=%s)", probe.ClusterID)
		}
		if probe.Details != "" {
			fmt.Printf(" - %s", probe.Details)
		}
		fmt.Println()
		if probe.State == dataNodeReuseConflicting || probe.State == dataNodeReuseUnreadable {
			actionable = append(actionable, probe)
		}
	}
	if len(actionable) == 0 {
		fmt.Println("  - All DataNodes are fresh or compatible")
		return originalTargets, nil
	}

	choice, err := i.promptChoice(
		fmt.Sprintf("We found %d reused/unreadable DataNodes. Recommended action is to wipe old HDFS DataNode storage and reuse them.", len(actionable)),
		[]string{
			"1. Stop installer",
			"2. Wipe all reused/unreadable DataNodes and reuse them (Recommended)",
			"3. Skip all reused/unreadable DataNodes for this run",
			"4. Review one by one",
		},
	)
	if err != nil {
		return nil, err
	}

	skipped := make(map[string]bool)
	switch choice {
	case 1:
		return nil, fmt.Errorf("install stopped by user after reused DataNode detection")
	case 2:
		for _, probe := range actionable {
			if err := i.wipeRemoteDataNodeStorage(probe.IP); err != nil {
				return nil, err
			}
			i.addSummary(i.nodeLabelForIP(probe.IP), "reused datanode", statusFixed, "wiped old HDFS DataNode storage and reused node")
		}
	case 3:
		for _, probe := range actionable {
			skipped[probe.IP] = true
			i.addSummary(i.nodeLabelForIP(probe.IP), "reused datanode", statusSkip, "skipped for this install run")
		}
	case 4:
		for _, probe := range actionable {
			resolution, err := i.promptChoice(
				fmt.Sprintf("%s (%s) is %s. %s", i.nodeLabelForIP(probe.IP), probe.IP, probe.State, probe.Details),
				[]string{
					"1. Stop installer",
					"2. Wipe old HDFS DataNode storage and reuse this node (Recommended)",
					"3. Skip this node for this run",
					"4. Keep old storage and force continue (Unsafe)",
				},
			)
			if err != nil {
				return nil, err
			}
			switch resolution {
			case 1:
				return nil, fmt.Errorf("install stopped by user while reviewing reused DataNodes")
			case 2:
				if err := i.wipeRemoteDataNodeStorage(probe.IP); err != nil {
					return nil, err
				}
				i.addSummary(i.nodeLabelForIP(probe.IP), "reused datanode", statusFixed, "wiped old HDFS DataNode storage and reused node")
			case 3:
				skipped[probe.IP] = true
				i.addSummary(i.nodeLabelForIP(probe.IP), "reused datanode", statusSkip, "skipped for this install run")
			case 4:
				i.addSummary(i.nodeLabelForIP(probe.IP), "reused datanode", statusWarn, "forced continue with old HDFS DataNode storage")
			}
		}
	}

	activeTargets := make([]string, 0, len(originalTargets))
	for _, ip := range originalTargets {
		if skipped[ip] {
			continue
		}
		activeTargets = append(activeTargets, ip)
	}
	if len(activeTargets) == 0 {
		return nil, fmt.Errorf("no DataNodes remain active for this install run after reused-node resolution")
	}
	i.cfg.ExistingNodeIPs = nil
	i.cfg.NewNodeIPs = append([]string(nil), activeTargets...)
	fmt.Printf("  - Active DataNodes for this run: %s\n", strings.Join(activeTargets, ", "))
	return activeTargets, nil
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

func (i *installer) probeDataNodeReuse(ip, localClusterID string) dataNodeReuseProbe {
	target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
	dataNodeDir := filepath.Join("/home", i.cfg.DataNodeUser, "hadoopdata", "datanode")
	versionPath := filepath.Join(dataNodeDir, "current", "VERSION")
	cmd := fmt.Sprintf(`DATA_DIR=%q; VERSION_PATH=%q; if [ ! -d "$DATA_DIR" ]; then printf 'fresh||no datanode storage\n'; elif [ ! -f "$VERSION_PATH" ]; then printf 'unreadable||VERSION file missing\n'; else CID=$(awk -F= '$1=="clusterID"{print $2}' "$VERSION_PATH" | tr -d '[:space:]'); if [ -z "$CID" ]; then printf 'unreadable||clusterID missing\n'; else printf 'found|%s|\n' "$CID"; fi; fi`, dataNodeDir, versionPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCommand("", nil, &stdout, &stderr, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-T", target, "bash", "-lc", cmd); err != nil {
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
	case "unreadable":
		return dataNodeReuseProbe{IP: ip, State: dataNodeReuseUnreadable, Details: fields[2]}
	case "found":
		clusterID := strings.TrimSpace(fields[1])
		if clusterID == localClusterID {
			return dataNodeReuseProbe{IP: ip, State: dataNodeReuseCompatible, ClusterID: clusterID, Details: "clusterID matches current NameNode"}
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

func (i *installer) wipeRemoteDataNodeStorage(ip string) error {
	target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
	dataNodeDir := filepath.Join("/home", i.cfg.DataNodeUser, "hadoopdata", "datanode")
	parentDir := filepath.Join("/home", i.cfg.DataNodeUser, "hadoopdata")
	fmt.Printf("  - Wiping old HDFS DataNode storage on %s\n", ip)
	cmd := fmt.Sprintf(`rm -rf %q && mkdir -p %q && chmod -R 700 %q`, dataNodeDir, parentDir, parentDir)
	if err := runCommand("", nil, os.Stdout, os.Stderr, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-T", target, "bash", "-lc", cmd); err != nil {
		return fmt.Errorf("failed to wipe old HDFS DataNode storage on %s: %w", ip, err)
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

func (i *installer) syncConfigsToDataNodes(targetIPs []string) error {
	section("SYNC TO DATANODES")
	rsyncSSH := "ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=5"
	for _, ip := range targetIPs {
		if err := i.ensureRemoteRsync(ip); err != nil {
			return err
		}
		fmt.Printf("  - Syncing to %s\n", ip)
		target := fmt.Sprintf("%s@%s:%s/", i.cfg.DataNodeUser, ip, i.hadoopHome)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "rsync", "-e", rsyncSSH, "-az", "--delete", i.hadoopHome+"/", target); err != nil {
			return err
		}
		sparkTarget := fmt.Sprintf("%s@%s:%s/", i.cfg.DataNodeUser, ip, i.sparkHome)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "rsync", "-e", rsyncSSH, "-az", "--delete", "--rsync-path=sudo rsync", i.sparkHome+"/", sparkTarget); err != nil {
			return err
		}
		targetHost := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		if err := runCommand("", nil, os.Stdout, os.Stderr, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-T", targetHost, "sudo", "chown", "-R", fmt.Sprintf("%s:%s", i.cfg.DataNodeUser, i.cfg.DataNodeUser), i.sparkHome); err != nil {
			return fmt.Errorf("failed to normalize spark ownership on %s: %w", ip, err)
		}
	}
	return nil
}

func (i *installer) ensureRemoteRsync(ip string) error {
	target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
	cmd := `command -v rsync >/dev/null 2>&1 || { sudo apt-get update && sudo apt-get install -y rsync; }`
	if err := runCommand("", nil, os.Stdout, os.Stderr, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-T", target, "bash", "-lc", cmd); err != nil {
		return fmt.Errorf("failed to ensure rsync on %s: %w", ip, err)
	}
	return nil
}

func (i *installer) runRemoteDataNodeSetup(ip string, nodeNumber int) error {
	section(fmt.Sprintf("DATANODE SETUP (%s)", ip))
	mirrorFlag := "0"
	if i.cfg.UseKakaoMirror {
		mirrorFlag = "1"
	}

	target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=5",
		"-T",
		target,
		"bash", "-s", "--",
		i.cfg.NameNodePrivateIP,
		strconv.Itoa(i.cfg.TotalDataNodes()),
		strconv.Itoa(nodeNumber),
		i.cfg.DataNodeUser,
		mirrorFlag,
		i.java11Home,
		i.sparkHome,
		i.hadoopHome,
	}
	args = append(args, i.cfg.AllDataNodeIPs()...)
	cmd := exec.Command("ssh", args...)
	cmd.Stdin = strings.NewReader(remoteDataNodeScript)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (i *installer) autoProbeHardware() ([]hostSpec, error) {
	specs := make([]hostSpec, 0, i.cfg.TotalDataNodes()+1)
	local, err := i.probeLocalHost()
	if err != nil {
		return nil, err
	}
	specs = append(specs, local)
	for idx, ip := range i.cfg.AllDataNodeIPs() {
		spec, err := i.probeRemoteHost(ip, idx+1)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func (i *installer) manualEnterHardware() ([]hostSpec, error) {
	specs := make([]hostSpec, 0, i.cfg.TotalDataNodes()+1)
	fmt.Println()
	fmt.Println("Enter hardware for recommendation only. This does not change the installed cluster automatically.")

	localCores, err := i.promptInt("NameNode CPU cores")
	if err != nil {
		return nil, err
	}
	localRAM, err := i.promptInt("NameNode RAM (GB)")
	if err != nil {
		return nil, err
	}
	specs = append(specs, hostSpec{
		Label:    "namenode",
		CPUCores: localCores,
		MemoryGB: localRAM,
		Source:   "manual",
	})

	for idx := range i.cfg.AllDataNodeIPs() {
		label := fmt.Sprintf("datanode%d", idx+1)
		cores, err := i.promptInt(fmt.Sprintf("%s CPU cores", label))
		if err != nil {
			return nil, err
		}
		ram, err := i.promptInt(fmt.Sprintf("%s RAM (GB)", label))
		if err != nil {
			return nil, err
		}
		specs = append(specs, hostSpec{
			Label:    label,
			CPUCores: cores,
			MemoryGB: ram,
			Source:   "manual",
		})
	}
	return specs, nil
}

func (i *installer) probeLocalHost() (hostSpec, error) {
	cores, memGB, err := probeHostCommand("")
	if err != nil {
		return hostSpec{}, err
	}
	return hostSpec{
		Label:    "namenode",
		CPUCores: cores,
		MemoryGB: memGB,
		Source:   "auto",
	}, nil
}

func (i *installer) probeRemoteHost(ip string, nodeNumber int) (hostSpec, error) {
	target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
	cores, memGB, err := probeHostCommand(target)
	if err != nil {
		return hostSpec{}, fmt.Errorf("auto probe failed for %s: %w", ip, err)
	}
	return hostSpec{
		Label:    fmt.Sprintf("datanode%d", nodeNumber),
		CPUCores: cores,
		MemoryGB: memGB,
		Source:   "auto",
	}, nil
}

func deriveRecommendations(specs []hostSpec) clusterRecommendation {
	dataNodes := make([]hostSpec, 0)
	for _, spec := range specs {
		if strings.HasPrefix(spec.Label, "datanode") {
			dataNodes = append(dataNodes, spec)
		}
	}

	minCores := 0
	minRAM := 0
	for idx, spec := range dataNodes {
		if idx == 0 || spec.CPUCores < minCores {
			minCores = spec.CPUCores
		}
		if idx == 0 || spec.MemoryGB < minRAM {
			minRAM = spec.MemoryGB
		}
	}

	replication := len(dataNodes)
	if replication > 3 {
		replication = 3
	}
	yarnVcores := recommendedYARNVcores(minCores)
	yarnMemoryMB := recommendedYARNMemoryMB(minRAM)
	executorsPerNode := 1
	if yarnVcores >= 8 && yarnMemoryMB >= 12288 {
		executorsPerNode = 2
	}
	executorCores := maxInt(1, minInt(4, yarnVcores/executorsPerNode))
	executorMemoryMB := maxInt(1024, int(float64(yarnMemoryMB/executorsPerNode)*0.8))
	driverMemoryMB := minInt(4096, maxInt(2048, executorMemoryMB/2))
	executorInstances := maxInt(1, executorsPerNode*len(dataNodes))
	shufflePartitions := maxInt(32, executorInstances*executorCores*2)

	return clusterRecommendation{
		NodeCount:              len(dataNodes),
		MinNodeCores:           minCores,
		MinNodeMemoryGB:        minRAM,
		YARNMemoryMB:           yarnMemoryMB,
		YARNVcores:             yarnVcores,
		HDFSReplication:        replication,
		SparkExecutorInstances: executorInstances,
		SparkExecutorCores:     executorCores,
		SparkExecutorMemoryMB:  executorMemoryMB,
		SparkDriverMemoryMB:    driverMemoryMB,
		SparkShufflePartitions: shufflePartitions,
	}
}

func recommendedYARNVcores(totalCores int) int {
	if totalCores <= 2 {
		return maxInt(1, totalCores)
	}
	if totalCores <= 8 {
		return totalCores - 1
	}
	return totalCores - 2
}

func recommendedYARNMemoryMB(totalGB int) int {
	if totalGB <= 0 {
		return 0
	}
	reserveGB := 2
	switch {
	case totalGB <= 8:
		reserveGB = 2
	case totalGB <= 16:
		reserveGB = 3
	case totalGB <= 32:
		reserveGB = 4
	default:
		reserveGB = maxInt(4, totalGB/10)
	}
	usableGB := maxInt(1, totalGB-reserveGB)
	return usableGB * 1024
}

func (i *installer) printRecommendationSummary(specs []hostSpec, rec clusterRecommendation) {
	fmt.Println()
	fmt.Println("=== HARDWARE PROBE SUMMARY ===")
	for _, spec := range specs {
		fmt.Printf("  - %s: %d cores, %d GB RAM (%s)\n", spec.Label, spec.CPUCores, spec.MemoryGB, spec.Source)
	}
	fmt.Println()
	fmt.Println("=== APPLIED PLATFORM CONFIG RECOMMENDATION ===")
	fmt.Printf("  - DataNodes: %d\n", rec.NodeCount)
	fmt.Printf("  - HDFS replication: %d\n", rec.HDFSReplication)
	fmt.Printf("  - Recommended YARN memory per DataNode: %d MB\n", rec.YARNMemoryMB)
	fmt.Printf("  - Recommended YARN vcores per DataNode: %d\n", rec.YARNVcores)
	fmt.Println()
	fmt.Println("=== RECOMMENDED SPARK DEFAULTS (DO NOT AUTO-APPLY TO DAGS/JOBS) ===")
	fmt.Printf("  - --executor-memory %dm\n", rec.SparkExecutorMemoryMB)
	fmt.Printf("  - --executor-cores %d\n", rec.SparkExecutorCores)
	fmt.Printf("  - --conf spark.executor.instances=%d\n", rec.SparkExecutorInstances)
	fmt.Printf("  - --driver-memory %dm\n", rec.SparkDriverMemoryMB)
	fmt.Printf("  - --conf spark.sql.shuffle.partitions=%d\n", rec.SparkShufflePartitions)
	fmt.Println()
	fmt.Println("Suggested Spark snippet:")
	fmt.Printf("  --driver-memory %dm \\\n", rec.SparkDriverMemoryMB)
	fmt.Printf("  --executor-memory %dm \\\n", rec.SparkExecutorMemoryMB)
	fmt.Printf("  --executor-cores %d \\\n", rec.SparkExecutorCores)
	fmt.Printf("  --conf spark.executor.instances=%d \\\n", rec.SparkExecutorInstances)
	fmt.Printf("  --conf spark.sql.shuffle.partitions=%d\n", rec.SparkShufflePartitions)
	fmt.Println()
}

func (i *installer) printLocalConfigStatus() error {
	bashrcState, bashrcDetails, bashrcErr := evaluateManagedBlockFile(filepath.Join(i.baseHome, ".bashrc"), envBegin, envEnd, i.desiredEnvBlock())
	if bashrcErr != nil {
		return bashrcErr
	}
	if bashrcState == configStateMissing {
		legacyOK, err := i.legacyEnvConfigured(filepath.Join(i.baseHome, ".bashrc"))
		if err != nil {
			return err
		}
		if legacyOK {
			bashrcState = configStateExact
			bashrcDetails = "legacy env lines present without managed block"
		}
	}

	items := []struct {
		label   string
		state   configState
		details string
		err     error
	}{
		{
			label: "Local /etc/hosts managed block",
			state: func() configState {
				state, _, _ := evaluateManagedBlockFile("/etc/hosts", hostsBegin, hostsEnd, i.desiredLocalHostsBlock())
				return state
			}(),
			details: func() string {
				_, details, _ := evaluateManagedBlockFile("/etc/hosts", hostsBegin, hostsEnd, i.desiredLocalHostsBlock())
				return details
			}(),
			err: func() error {
				_, _, err := evaluateManagedBlockFile("/etc/hosts", hostsBegin, hostsEnd, i.desiredLocalHostsBlock())
				return err
			}(),
		},
		{
			label:   "Local .bashrc managed env block",
			state:   bashrcState,
			details: bashrcDetails,
			err:     nil,
		},
		{
			label: "Local core-site.xml",
			state: func() configState {
				state, _, _ := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "core-site.xml"), i.expectedCoreSiteProperties())
				return state
			}(),
			details: func() string {
				_, details, _ := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "core-site.xml"), i.expectedCoreSiteProperties())
				return details
			}(),
			err: func() error {
				_, _, err := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "core-site.xml"), i.expectedCoreSiteProperties())
				return err
			}(),
		},
		{
			label: "Local hdfs-site.xml",
			state: func() configState {
				state, _, _ := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "hdfs-site.xml"), i.expectedHdfsSiteProperties())
				return state
			}(),
			details: func() string {
				_, details, _ := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "hdfs-site.xml"), i.expectedHdfsSiteProperties())
				return details
			}(),
			err: func() error {
				_, _, err := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "hdfs-site.xml"), i.expectedHdfsSiteProperties())
				return err
			}(),
		},
		{
			label: "Local workers",
			state: func() configState {
				state, _, _ := evaluateWorkersFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "workers"), i.expectedWorkers())
				return state
			}(),
			details: func() string {
				_, details, _ := evaluateWorkersFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "workers"), i.expectedWorkers())
				return details
			}(),
			err: func() error {
				_, _, err := evaluateWorkersFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "workers"), i.expectedWorkers())
				return err
			}(),
		},
		{
			label: "Local mapred-site.xml",
			state: func() configState {
				state, _, _ := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "mapred-site.xml"), i.expectedMapredSiteProperties())
				return state
			}(),
			details: func() string {
				_, details, _ := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "mapred-site.xml"), i.expectedMapredSiteProperties())
				return details
			}(),
			err: func() error {
				_, _, err := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "mapred-site.xml"), i.expectedMapredSiteProperties())
				return err
			}(),
		},
		{
			label: "Local yarn-site.xml",
			state: func() configState {
				state, _, _ := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "yarn-site.xml"), i.expectedYarnSiteProperties())
				return state
			}(),
			details: func() string {
				_, details, _ := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "yarn-site.xml"), i.expectedYarnSiteProperties())
				return details
			}(),
			err: func() error {
				_, _, err := evaluateHadoopXMLFile(filepath.Join(i.hadoopHome, "etc", "hadoop", "yarn-site.xml"), i.expectedYarnSiteProperties())
				return err
			}(),
		},
	}

	for _, item := range items {
		if item.err != nil {
			return item.err
		}
		fmt.Printf("  - %s: %s", item.label, item.state)
		if item.details != "" {
			fmt.Printf(" (%s)", item.details)
		}
		fmt.Println()
	}
	return nil
}

func (i *installer) desiredLocalHostsBlock() string {
	var block bytes.Buffer
	block.WriteString(hostsBegin + "\n")
	block.WriteString(fmt.Sprintf("%s namenode\n", i.cfg.NameNodePrivateIP))
	for idx, ip := range i.cfg.AllDataNodeIPs() {
		block.WriteString(fmt.Sprintf("%s datanode%d\n", ip, idx+1))
	}
	block.WriteString(hostsEnd + "\n")
	return block.String()
}

func (i *installer) desiredEnvBlock() string {
	return fmt.Sprintf(`%s
export JAVA_HOME=%s
export HADOOP_HOME=%s
export SPARK_HOME=%s
export HADOOP_CONF_DIR=$HADOOP_HOME/etc/hadoop
export YARN_CONF_DIR=$HADOOP_HOME/etc/hadoop
export PATH=$PATH:$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh
%s
	`, envBegin, i.java11Home, i.hadoopHome, i.sparkHome, envEnd)
}

func (i *installer) legacyEnvConfigured(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	text := string(data)
	requiredLines := []string{
		fmt.Sprintf("export JAVA_HOME=%s", i.java11Home),
		fmt.Sprintf("export HADOOP_HOME=%s", i.hadoopHome),
		fmt.Sprintf("export SPARK_HOME=%s", i.sparkHome),
		`export HADOOP_CONF_DIR=$HADOOP_HOME/etc/hadoop`,
		`export YARN_CONF_DIR=$HADOOP_HOME/etc/hadoop`,
		`export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"`,
		`export PDSH_RCMD_TYPE=ssh`,
	}
	for _, line := range requiredLines {
		if !strings.Contains(text, line) {
			return false, nil
		}
	}
	if !strings.Contains(text, `export PATH=$PATH:$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin`) &&
		!strings.Contains(text, `export PATH="$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin:$PATH"`) {
		return false, nil
	}
	return true, nil
}

func evaluateManagedBlockFile(path, beginMarker, endMarker, desired string) (configState, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configStateMissing, "file missing", nil
		}
		return "", "", err
	}
	block, found, err := extractManagedBlock(string(data), beginMarker, endMarker)
	if err != nil {
		return "", "", err
	}
	if !found {
		return configStateMissing, "managed block missing", nil
	}
	if normalizeText(block) == normalizeText(desired) {
		return configStateExact, "", nil
	}
	return configStateDrift, "managed block differs from desired cluster shape", nil
}

func extractManagedBlock(text, beginMarker, endMarker string) (string, bool, error) {
	start := strings.Index(text, beginMarker)
	if start == -1 {
		return "", false, nil
	}
	end := strings.Index(text[start:], endMarker)
	if end == -1 {
		return "", false, fmt.Errorf("found %s without matching %s", beginMarker, endMarker)
	}
	end += start + len(endMarker)
	block := text[start:end]
	if !strings.HasSuffix(block, "\n") {
		block += "\n"
	}
	return block, true, nil
}

func upsertManagedBlockText(text, beginMarker, endMarker, desiredBlock string) (string, error) {
	start := strings.Index(text, beginMarker)
	if start != -1 {
		end := strings.Index(text[start:], endMarker)
		if end == -1 {
			return "", fmt.Errorf("found %s without matching %s", beginMarker, endMarker)
		}
		end += start + len(endMarker)
		if end < len(text) && text[end] == '\n' {
			end++
		}
		updated := text[:start] + desiredBlock + text[end:]
		return normalizeManagedFileText(updated), nil
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return normalizeManagedFileText(text + desiredBlock), nil
}

func normalizeManagedFileText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	return text + "\n"
}

func evaluateHadoopXMLFile(path string, expected map[string]string) (configState, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configStateMissing, "file missing", nil
		}
		return "", "", err
	}
	var cfg hadoopXMLConfiguration
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return configStateDrift, "file is not parseable Hadoop XML", nil
	}
	props := make(map[string]string, len(cfg.Properties))
	for _, prop := range cfg.Properties {
		name := strings.TrimSpace(prop.Name)
		if name == "" {
			continue
		}
		props[name] = strings.TrimSpace(prop.Value)
	}
	if len(props) == 0 {
		return configStateEmpty, "no configured properties", nil
	}
	for name, expectedValue := range expected {
		actualValue, ok := props[name]
		if !ok {
			return configStateDrift, fmt.Sprintf("missing %s", name), nil
		}
		if actualValue != expectedValue {
			return configStateDrift, fmt.Sprintf("%s=%q (expected %q)", name, actualValue, expectedValue), nil
		}
	}
	return configStateExact, "", nil
}

func evaluateWorkersFile(path string, expected []string) (configState, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configStateMissing, "file missing", nil
		}
		return "", "", err
	}
	lines := make([]string, 0)
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return configStateEmpty, "no worker hosts listed", nil
	}
	if len(lines) == 1 && lines[0] == "localhost" {
		return configStateMissing, "stock default localhost entry", nil
	}
	if strings.Join(lines, "\n") == strings.Join(expected, "\n") {
		return configStateExact, "", nil
	}
	return configStateDrift, fmt.Sprintf("workers=%q expected=%q", strings.Join(lines, ","), strings.Join(expected, ",")), nil
}

func evaluateJavaEnvFile(path string, accepted []string) (configState, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configStateMissing, "file missing", nil
		}
		return "", "", err
	}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "export JAVA_HOME=") {
			continue
		}
		for _, candidate := range accepted {
			if line == candidate {
				return configStateExact, "", nil
			}
		}
		return configStateDrift, fmt.Sprintf("JAVA_HOME line=%q", line), nil
	}
	return configStateEmpty, "JAVA_HOME export not set", nil
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimSpace(text)
}

func ensureTrailingNewline(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text
}

func driftError(component, details string) error {
	if details == "" {
		details = "existing state differs from the installer-managed desired state"
	}
	return fmt.Errorf("%s drift detected: %s. Refusing to overwrite automatically in install mode; run --preflight and review the existing cluster state first", component, details)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func parseMode(args []string) (installerMode, error) {
	if len(args) == 0 {
		return modeInstall, nil
	}
	if len(args) != 1 {
		return "", fmt.Errorf("unsupported arguments: %s", strings.Join(args, " "))
	}
	switch args[0] {
	case "--dry-run":
		return modeDryRun, nil
	case "--preflight":
		return modePreflight, nil
	case "--repair":
		return modeRepair, nil
	case "--reconcile":
		return modeReconcile, nil
	case "--recommend-only":
		return modeRecommendOnly, nil
	case "--help", "-h":
		fmt.Println("Usage:")
		fmt.Println("  foxai-installer                  Run the full installer flow")
		fmt.Println("  foxai-installer --dry-run        Print the execution plan only")
		fmt.Println("  foxai-installer --preflight      Inspect local/remote installer prerequisites without changing cluster state")
		fmt.Println("  foxai-installer --repair         Inspect an existing cluster, ask for confirmation, then repair selected nodes")
		fmt.Println("  foxai-installer --reconcile      Align an existing cluster to the requested FoxAI shape, including new DataNodes")
		fmt.Println("  foxai-installer --recommend-only Skip install steps and print hardware-based Spark recommendations")
		os.Exit(0)
	default:
		return "", fmt.Errorf("unknown option: %s", args[0])
	}
	return modeInstall, nil
}

func probeHostCommand(sshTarget string) (int, int, error) {
	script := `set -euo pipefail
CORES="$(nproc)"
MEM_KB="$(awk '/MemTotal/ {print $2}' /proc/meminfo)"
MEM_GB=$((MEM_KB / 1024 / 1024))
printf '%s %s\n' "$CORES" "$MEM_GB"
`
	var stdout bytes.Buffer
	var err error
	if sshTarget == "" {
		err = runCommand("", strings.NewReader(script), &stdout, os.Stderr, "bash", "-s")
	} else {
		err = runCommand("", strings.NewReader(script), &stdout, os.Stderr, "ssh", sshTarget, "bash", "-s")
	}
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected probe output: %q", stdout.String())
	}
	cores, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, err
	}
	memGB, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, err
	}
	return cores, memGB, nil
}

func (i *installer) promptRequired(label string) (string, error) {
	for {
		value, err := i.readPrompt(fmt.Sprintf("%s: ", label))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
		fmt.Println("  Value is required.")
	}
}

func (i *installer) promptOptional(label, defaultValue string) (string, error) {
	value, err := i.readPrompt(fmt.Sprintf("%s [%s]: ", label, defaultValue))
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func (i *installer) promptInt(label string) (int, error) {
	for {
		value, err := i.readPrompt(fmt.Sprintf("%s: ", label))
		if err != nil {
			return 0, err
		}
		value = strings.TrimSpace(value)
		number, convErr := strconv.Atoi(value)
		if convErr == nil && number >= 0 {
			return number, nil
		}
		fmt.Println("  Enter a whole number.")
	}
}

func (i *installer) promptIP(label string) (string, error) {
	for {
		value, err := i.readPrompt(fmt.Sprintf("%s: ", label))
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if _, parseErr := netip.ParseAddr(value); parseErr == nil {
			return value, nil
		}
		fmt.Println("  Invalid IP address.")
	}
}

func (i *installer) promptYesNoDefault(label, defaultValue string) (bool, error) {
	normalized := "Y/n"
	defaultYes := strings.EqualFold(defaultValue, "yes")
	if !defaultYes {
		normalized = "y/N"
	}
	for {
		value, err := i.readPrompt(fmt.Sprintf("%s [%s]: ", label, normalized))
		if err != nil {
			return false, err
		}
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return defaultYes, nil
		}
		switch value {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Println("  Enter yes or no.")
		}
	}
}

func (i *installer) promptChoice(label string, options []string) (int, error) {
	fmt.Println(label)
	for _, option := range options {
		fmt.Println("  " + option)
	}
	for {
		value, err := i.readPrompt(fmt.Sprintf("Choose 1-%d: ", len(options)))
		if err != nil {
			return 0, err
		}
		number, convErr := strconv.Atoi(strings.TrimSpace(value))
		if convErr == nil && number >= 1 && number <= len(options) {
			return number, nil
		}
		fmt.Println("  Enter one of the listed choices.")
	}
}

func (i *installer) readMultiline(label string) (string, error) {
	const endMarker = "__FOXAI_END__"
	fmt.Println(label)
	fmt.Printf("Finish with a line containing only %s\n", endMarker)
	lines := make([]string, 0)
	for {
		value, err := i.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		line := strings.TrimRight(value, "\r\n")
		if line == endMarker {
			return ensureTrailingNewline(strings.Join(lines, "\n")), nil
		}
		lines = append(lines, line)
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("reached EOF before %s marker", endMarker)
		}
	}
}

func (i *installer) resolveInstallDrift(component, details string) (int, string, error) {
	fmt.Printf("  - Drift detected in %s: %s\n", component, details)
	choice, err := i.promptChoice(
		fmt.Sprintf("%s is drifted. Choose how to continue:", component),
		[]string{
			"1. Stop installer",
			"2. Replace with installer value",
			"3. Enter custom replacement",
			"4. Skip this step",
		},
	)
	if err != nil {
		return 0, "", err
	}
	if choice != 3 {
		return choice, "", nil
	}
	custom, err := i.readMultiline(fmt.Sprintf("Paste the replacement content for %s.", component))
	if err != nil {
		return 0, "", err
	}
	return choice, custom, nil
}

func (i *installer) readPrompt(prompt string) (string, error) {
	fmt.Print(prompt)
	value, err := i.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(value, "\r\n"), nil
}

func (i *installer) requireCommands(commands ...string) error {
	for _, command := range commands {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("required command not found: %s", command)
		}
	}
	return nil
}

func (i *installer) ensureBootstrapDependencies(deps []bootstrapDependency) error {
	section("BOOTSTRAP DEPENDENCIES")
	missingCommands := make([]string, 0)
	packagesSeen := make(map[string]bool)
	missingPackages := make([]string, 0)
	for _, dep := range deps {
		if _, err := exec.LookPath(dep.Command); err == nil {
			continue
		}
		missingCommands = append(missingCommands, dep.Command)
		if dep.Package != "" && !packagesSeen[dep.Package] {
			packagesSeen[dep.Package] = true
			missingPackages = append(missingPackages, dep.Package)
		}
	}
	if len(missingCommands) == 0 {
		fmt.Println("  - Already installed")
		return nil
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		return fmt.Errorf("missing bootstrap dependencies (%s) and apt-get is unavailable", strings.Join(missingCommands, ", "))
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "apt-get", "update"); err != nil {
		return err
	}
	installArgs := append([]string{"apt-get", "install", "-y"}, missingPackages...)
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, installArgs...); err != nil {
		return err
	}
	fmt.Printf("  - Installed missing base dependencies: %s\n", strings.Join(missingPackages, ", "))
	return nil
}

func runPythonScriptWithSudo(args []string, script string) error {
	cmdArgs := append([]string{"python3", "-"}, args...)
	return runElevatedCommand(strings.NewReader(script), os.Stdout, os.Stderr, cmdArgs...)
}

func runElevatedCommand(stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command provided")
	}
	if os.Geteuid() == 0 {
		return runCommand("", stdin, stdout, stderr, args[0], args[1:]...)
	}
	if _, err := exec.LookPath("sudo"); err == nil {
		return runCommand("", stdin, stdout, stderr, "sudo", args...)
	}
	return fmt.Errorf("root privileges are required to install bootstrap dependencies")
}

func runCommand(dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func section(title string) {
	fmt.Println("====================")
	fmt.Printf("STEP: %s\n", title)
	fmt.Println("====================")
}

func mustOutput(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		fatal(err)
	}
	return string(out)
}

func contentContains(path, needle string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), needle)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	os.Exit(1)
}

const remoteDataNodeScript = `set -euo pipefail

NN_PRIVATE_IP="$1"
TOTAL_DN="$2"
MY_DN_NUM="$3"
DN_USER="$4"
USE_KAKAO_MIRROR="$5"
JAVA_HOME="$6"
SPARK_HOME="$7"
HADOOP_HOME="$8"
shift 8
ALL_DN_IPS=("$@")
BASE_HOME="/home/$DN_USER"
HOSTS_BEGIN="# >>> FOXAI CLUSTER HOSTS >>>"
HOSTS_END="# <<< FOXAI CLUSTER HOSTS <<<"
ENV_BEGIN="# >>> FOXAI MANAGED ENV >>>"
ENV_END="# <<< FOXAI MANAGED ENV <<<"

section() {
    echo "===================="
    echo "REMOTE STEP: $1"
    echo "===================="
}

ensure_adoptium_repo() {
    if [ ! -f /usr/share/keyrings/adoptium.gpg ]; then
        wget -4 -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | sudo gpg --dearmor -o /usr/share/keyrings/adoptium.gpg
    fi
    . /etc/os-release
    ADOPT_CODENAME="${VERSION_CODENAME:-bookworm}"
    if [ ! -f /etc/apt/sources.list.d/adoptium.list ]; then
        echo "deb [signed-by=/usr/share/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb ${ADOPT_CODENAME} main" | sudo tee /etc/apt/sources.list.d/adoptium.list >/dev/null
    fi
}

section "CHECK USER"
if [ "$(whoami)" != "$DN_USER" ]; then
    echo "ERROR: Run as user '$DN_USER' (now: $(whoami))."
    exit 1
fi

section "CHECK HADOOP FROM NAMENODE"
if [ ! -x "$HADOOP_HOME/bin/hdfs" ]; then
    echo "ERROR: $HADOOP_HOME missing."
    echo "Run the unified installer from Namenode so it syncs Hadoop here first."
    exit 1
fi
echo "  - Hadoop present"

section "CHECK SPARK (/opt/spark)"
if [ -x "$SPARK_HOME/bin/spark-submit" ]; then
    echo "  - Spark present"
else
    echo "  WARNING: Spark not found at $SPARK_HOME"
fi

section "APT MIRROR"
if [ "$USE_KAKAO_MIRROR" = "1" ]; then
    if grep -qE "mirror\.kakao\.com/ubuntu" /etc/apt/sources.list; then
        echo "  - Already configured"
    else
        sudo sed -i 's|http://archive.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list
        sudo sed -i 's|http://security.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list
        echo "  - Configured"
    fi
else
    echo "  - Skipped premise-specific Kakao mirror override"
fi

section "BASE PACKAGES"
MISSING=""
for p in wget gpg ssh pdsh python3-venv python3-pip curl tar rsync unzip build-essential libffi-dev python3-dev libsasl2-dev libldap2-dev; do
    if ! dpkg -s "$p" >/dev/null 2>&1; then
        MISSING="$MISSING $p"
    fi
done
if [ -n "$MISSING" ]; then
    sudo apt update
    sudo apt install -y $MISSING
    echo "  - Installed:$MISSING"
else
    echo "  - Already installed"
fi

if [ ! -e /usr/bin/python ]; then
    sudo ln -sf /usr/bin/python3 /usr/bin/python
fi

section "JAVA 11"
if [ -d "$JAVA_HOME" ]; then
    echo "  - Already installed"
else
    ensure_adoptium_repo
    sudo apt update
    sudo apt install -y temurin-11-jdk
    echo "  - Installed"
fi

section "/etc/hosts (FULL MANAGED BLOCK)"
echo "  Note: ` + remoteDNProbeBlockNote + `"
HOSTS_STATUS="$(sudo python3 - "$NN_PRIVATE_IP" "$HOSTS_BEGIN" "$HOSTS_END" "${ALL_DN_IPS[@]}" <<'PYEOF'
import sys

nn_ip, begin, end = sys.argv[1], sys.argv[2], sys.argv[3]
dn_ips = sys.argv[4:]
hosts = open("/etc/hosts").read()
lines = [begin, f"{nn_ip} namenode"]
for idx, ip in enumerate(dn_ips, start=1):
    lines.append(f"{ip} datanode{idx}")
lines.append(end)
desired = "\n".join(lines) + "\n"
start = hosts.find(begin)
if start != -1:
    end_idx = hosts.find(end, start)
    if end_idx != -1:
        end_idx += len(end)
        current = hosts[start:end_idx]
        if current.strip() == desired.strip():
            print("exact")
            raise SystemExit(0)
        hosts = hosts[:start] + desired + hosts[end_idx:]
        print("updated")
        open("/etc/hosts", "w").write(hosts.rstrip() + "\n")
        raise SystemExit(0)
hosts = hosts.rstrip() + "\n" + desired
open("/etc/hosts", "w").write(hosts.rstrip() + "\n")
print("written")
PYEOF
)"
if [ "$HOSTS_STATUS" = "exact" ]; then
    echo "  - Already configured"
elif [ "$HOSTS_STATUS" = "written" ] || [ "$HOSTS_STATUS" = "updated" ]; then
    echo "  - Updated"
else
    echo "ERROR: failed to manage /etc/hosts FoxAI block"
    exit 1
fi

section "HADOOP JAVA_HOME"
HADOOP_ENV="$HADOOP_HOME/etc/hadoop/hadoop-env.sh"
FLEXIBLE_JAVA="export JAVA_HOME=\${JAVA_HOME:-$JAVA_HOME}"
FIXED_JAVA="export JAVA_HOME=$JAVA_HOME"
if grep -qF "$FLEXIBLE_JAVA" "$HADOOP_ENV" 2>/dev/null; then
    echo "  - Already configured (flexible)"
elif grep -qFx "$FIXED_JAVA" "$HADOOP_ENV" 2>/dev/null; then
    echo "  - Already configured"
elif grep -qE '^# export JAVA_HOME=' "$HADOOP_ENV" 2>/dev/null; then
    sed -i "s|^# export JAVA_HOME=.*|$FLEXIBLE_JAVA|" "$HADOOP_ENV"
    echo "  - Updated"
elif grep -qE "^export JAVA_HOME=" "$HADOOP_ENV" 2>/dev/null; then
    sed -i "s|^export JAVA_HOME=.*|$FLEXIBLE_JAVA|" "$HADOOP_ENV"
    echo "  - Updated"
else
    echo "$FLEXIBLE_JAVA" >> "$HADOOP_ENV"
    echo "  - Added"
fi

section "SHELL ENVIRONMENT"
ENV_BLOCK="$(cat <<EOT
${ENV_BEGIN}
export JAVA_HOME=$JAVA_HOME
export HADOOP_HOME=$HADOOP_HOME
export SPARK_HOME=$SPARK_HOME
export HADOOP_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export YARN_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export PATH=\$PATH:\$JAVA_HOME/bin:\$HADOOP_HOME/bin:\$HADOOP_HOME/sbin:\$SPARK_HOME/bin:\$SPARK_HOME/sbin
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh
${ENV_END}
EOT
)"
ENV_STATUS="$(python3 - "$HOME/.bashrc" "$ENV_BEGIN" "$ENV_END" "$ENV_BLOCK" <<'PYEOF'
import sys

path, begin, end, desired = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
text = open(path).read()
start = text.find(begin)
if start != -1:
    end_idx = text.find(end, start)
    if end_idx == -1:
        print("error")
        raise SystemExit(0)
    end_idx += len(end)
    current = text[start:end_idx]
    if current.strip() == desired.strip():
        print("exact")
        raise SystemExit(0)
    text = text[:start] + desired + "\n" + text[end_idx:].lstrip("\n")
    open(path, "w").write(text.rstrip() + "\n")
    print("updated")
    raise SystemExit(0)
text = text.rstrip() + "\n" + desired + "\n"
open(path, "w").write(text.rstrip() + "\n")
print("written")
PYEOF
)"
if [ "$ENV_STATUS" = "exact" ]; then
    echo "  - Already configured"
elif [ "$ENV_STATUS" = "written" ] || [ "$ENV_STATUS" = "updated" ]; then
    echo "  - Updated"
else
    echo "ERROR: failed to manage FoxAI environment block in ~/.bashrc"
    exit 1
fi

source "$HOME/.bashrc" >/dev/null 2>&1 || true

export JAVA_HOME="$JAVA_HOME"
export HADOOP_HOME="$HADOOP_HOME"
export SPARK_HOME="$SPARK_HOME"
export HADOOP_CONF_DIR="$HADOOP_HOME/etc/hadoop"
export YARN_CONF_DIR="$HADOOP_HOME/etc/hadoop"
export PATH="$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin:$PATH"
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh

section "DATANODE DIRECTORY"
if [ -d "$BASE_HOME/hadoopdata/datanode" ]; then
    chmod -R 700 "$BASE_HOME/hadoopdata"
    echo "  - Already exists"
else
    mkdir -p "$BASE_HOME/hadoopdata/datanode"
    chmod -R 700 "$BASE_HOME/hadoopdata"
    echo "  - Created"
fi

echo "=== REMOTE DATANODE SETUP DONE ==="
`
