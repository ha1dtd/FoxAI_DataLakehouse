//go:build linux

package main

import (
	"bufio"
	"bytes"
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
)

const (
	pinnedHadoopVersion = "3.3.6"
	pinnedSparkArtifact = "spark-3.5.8-bin-hadoop3"
	pinnedJava11Package = "temurin-11-jdk"
	pinnedJava17Package = "temurin-17-jdk"
	pinnedJava11Home    = "/usr/lib/jvm/temurin-11-jdk-amd64"
	pinnedJava17Home    = "/usr/lib/jvm/temurin-17-jdk-amd64"

	defaultMinIOEndpoint   = "192.168.100.66:9001"
	defaultMinIOAccessKey  = "admin"
	defaultMinIOSecretKey  = "12345678"
	defaultUseKakaoMirror  = "yes"
	hostsBegin             = "# >>> FOXAI CLUSTER HOSTS >>>"
	hostsEnd               = "# <<< FOXAI CLUSTER HOSTS <<<"
	sudoersTemplate        = "%s ALL=(ALL) NOPASSWD:ALL"
	remoteDNProbeBlockNote = "Full cluster hosts block remains aligned to the current tested flow and is synced from the NameNode."
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
	cfg         installerConfig
	mode        installerMode
	reader      *bufio.Reader
	currentUser string
	baseHome    string
	hadoopHome  string
	sparkHome   string
	java11Home  string
	java17Home  string
}

type installerMode string

const (
	modeInstall       installerMode = "install"
	modeDryRun        installerMode = "dry-run"
	modeRecommendOnly installerMode = "recommend-only"
)

type hostSpec struct {
	Label    string
	CPUCores int
	MemoryGB int
	Source   string
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
		java17Home:  pinnedJava17Home,
	}

	fatal(inst.collectInputs())
	switch inst.mode {
	case modeInstall:
		fatal(inst.requireCommands("python3", "ssh", "rsync", "wget", "tar", "sudo"))
		fatal(inst.runNameNodeSetup())
		fatal(inst.runAllDataNodeSetups())
		fatal(inst.finishAndRecommend())
	case modeDryRun:
		fatal(inst.printDryRunPlan())
	case modeRecommendOnly:
		fatal(inst.finishAndRecommend())
	default:
		fatal(fmt.Errorf("unsupported installer mode: %s", inst.mode))
	}
}

func (i *installer) collectInputs() error {
	fmt.Println("=== FOXAI SINGLE-FILE INSTALLER (GO) ===")
	fmt.Printf("Mode: %s\n", i.mode)
	if i.mode == modeDryRun {
		fmt.Println("Dry-run mode will collect inputs and print the execution plan only. No install commands will run.")
	}
	if i.mode == modeRecommendOnly {
		fmt.Println("Recommend-only mode will skip install steps and only collect hardware for Spark recommendations.")
	}
	fmt.Println("This installer preserves the current tested flow in one Linux-native entrypoint:")
	fmt.Println("1. NameNode setup locally")
	fmt.Println("2. DataNode setup remotely on each DataNode")
	fmt.Println()
	fmt.Printf("Pinned versions:\n")
	fmt.Printf("  - Hadoop: %s\n", pinnedHadoopVersion)
	fmt.Printf("  - Spark:  %s\n", pinnedSparkArtifact)
	fmt.Printf("  - Java 11 package: %s\n", pinnedJava11Package)
	fmt.Printf("  - Java 17 package: %s\n", pinnedJava17Package)
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

	dataNodeUser, err := i.promptRequired("Datanode username")
	if err != nil {
		return err
	}
	minioEndpoint, err := i.promptOptional("MinIO endpoint", defaultMinIOEndpoint)
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
	fmt.Printf("  - Java 17 home: %s\n", i.java17Home)
	fmt.Println()

	fmt.Println("Would run on NameNode:")
	for _, step := range []string{
		"SSH key generation / authorized_keys check",
		"ssh-copy-id to all DataNodes",
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
			"/etc/hosts minimal block rewrite",
			"hadoop-env JAVA_HOME alignment",
			".bashrc environment block check",
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

func (i *installer) runNameNodeSetup() error {
	if err := i.ensureLocalSSHKey(); err != nil {
		return err
	}
	if err := i.copySSHKeyToAllDataNodes(); err != nil {
		return err
	}
	if err := i.ensureDataNodesNoPasswordSudo(); err != nil {
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
	if err := i.ensureJavaLocal(pinnedJava17Package, i.java17Home, "JAVA 17"); err != nil {
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
	if err := i.syncConfigsToDataNodes(); err != nil {
		return err
	}
	return nil
}

func (i *installer) runAllDataNodeSetups() error {
	allNodes := i.cfg.AllDataNodeIPs()
	for idx, ip := range allNodes {
		if err := i.runRemoteDataNodeSetup(ip, idx+1); err != nil {
			return err
		}
	}
	return nil
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
	section("SSH COPY TO ALL DNs")
	for _, ip := range i.cfg.AllDataNodeIPs() {
		target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "ssh-copy-id", "-f", target); err != nil {
			return fmt.Errorf("ssh-copy-id failed for %s: %w", ip, err)
		}
	}
	return nil
}

func (i *installer) ensureDataNodesNoPasswordSudo() error {
	section("NOPASSWD (ALL DNs)")
	for _, ip := range i.cfg.AllDataNodeIPs() {
		target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		fmt.Printf("  - Checking %s\n", ip)
		err := runCommand("", nil, io.Discard, io.Discard, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", target, "sudo", "-n", "true")
		if err == nil {
			fmt.Println("    * Already NOPASSWD")
			continue
		}
		fmt.Println("    * Configuring NOPASSWD (enter password if prompted)")
		line := fmt.Sprintf(sudoersTemplate, i.cfg.DataNodeUser)
		remoteCmd := fmt.Sprintf("echo %q | sudo tee /etc/sudoers.d/%s >/dev/null", line, i.cfg.DataNodeUser)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "ssh", "-tt", target, remoteCmd); err != nil {
			return fmt.Errorf("failed to configure NOPASSWD on %s: %w", ip, err)
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
	cmd := `sudo sed -i 's|http://archive.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list && sudo sed -i 's|http://security.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list`
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", cmd); err != nil {
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
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "sudo", "apt", "update"); err != nil {
		return err
	}
	args := append([]string{"apt", "install", "-y"}, missing...)
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "sudo", args...); err != nil {
		return err
	}
	fmt.Printf("  - Installed:%s\n", " "+strings.Join(missing, " "))
	return nil
}

func (i *installer) ensurePythonSymlinkLocal() error {
	if fileExists("/usr/bin/python") {
		return nil
	}
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "sudo", "ln", "-sf", "/usr/bin/python3", "/usr/bin/python"); err != nil {
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
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "sudo", "apt", "update"); err != nil {
		return err
	}
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "sudo", "apt", "install", "-y", pkg); err != nil {
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
		cmd := "wget -4 -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | sudo gpg --dearmor -o /usr/share/keyrings/adoptium.gpg"
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", cmd); err != nil {
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
	cmd := fmt.Sprintf("echo %q | sudo tee /etc/apt/sources.list.d/adoptium.list >/dev/null", line)
	return runCommand("", os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", cmd)
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
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "sudo", "mv", filepath.Join(i.baseHome, pinnedSparkArtifact), i.sparkHome); err != nil {
		return err
	}
	if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "sudo", "chown", "-R", fmt.Sprintf("%s:%s", i.currentUser, i.currentUser), i.sparkHome); err != nil {
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
	if strings.Contains(string(content), "HADOOP_HOME") {
		fmt.Println("  - Already configured")
		return nil
	}
	block := fmt.Sprintf(`
export JAVA_HOME=%s
export HADOOP_HOME=%s
export SPARK_HOME=%s
export HADOOP_CONF_DIR=$HADOOP_HOME/etc/hadoop
export YARN_CONF_DIR=$HADOOP_HOME/etc/hadoop
export PATH=$PATH:$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh
`, i.java11Home, i.hadoopHome, i.sparkHome)
	f, err := os.OpenFile(bashrcPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(block); err != nil {
		return err
	}
	fmt.Println("  - Configured")
	return nil
}

func (i *installer) updateHostsBlockLocal() error {
	section("/etc/hosts")
	tempFile, err := os.CreateTemp("", "foxai-hosts-block-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())

	var block bytes.Buffer
	block.WriteString(hostsBegin + "\n")
	block.WriteString(fmt.Sprintf("%s namenode\n", i.cfg.NameNodePrivateIP))
	for idx, ip := range i.cfg.AllDataNodeIPs() {
		block.WriteString(fmt.Sprintf("%s datanode%d\n", ip, idx+1))
	}
	block.WriteString(hostsEnd + "\n")
	if _, err := tempFile.Write(block.Bytes()); err != nil {
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
	fmt.Println("  - Updated")
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
	if contentContains(target, "hdfs://namenode:9000") {
		fmt.Println("  - Already configured")
		return nil
	}
	content := `<configuration>
  <property>
    <name>fs.defaultFS</name>
    <value>hdfs://namenode:9000</value>
  </property>
</configuration>
`
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Println("  - Configured")
	return nil
}

func (i *installer) ensureHdfsSite() error {
	section("HDFS-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "hdfs-site.xml")
	if contentContains(target, "dfs.datanode.data.dir") {
		fmt.Println("  - Already configured")
		return nil
	}
	replication := i.cfg.TotalDataNodes()
	if replication > 3 {
		replication = 3
	}
	content := fmt.Sprintf(`<configuration>
  <property><name>dfs.replication</name><value>%d</value></property>
  <property><name>dfs.namenode.name.dir</name><value>file://%s/hadoopdata/namenode</value></property>
  <property><name>dfs.datanode.data.dir</name><value>file:///%s/hadoopdata/datanode</value></property>
</configuration>
`, replication, i.baseHome, i.cfg.DataNodeUser)
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Printf("  - Configured (replication=%d)\n", replication)
	return nil
}

func (i *installer) ensureWorkersFile() error {
	section("WORKERS FILE")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "workers")
	data, _ := os.ReadFile(target)
	existing := string(data)
	ready := true
	for idx := 1; idx <= i.cfg.TotalDataNodes(); idx++ {
		line := fmt.Sprintf("datanode%d", idx)
		if !strings.Contains(existing, line) {
			ready = false
			break
		}
	}
	if ready {
		fmt.Println("  - Already configured")
		return nil
	}
	var builder strings.Builder
	for idx := 1; idx <= i.cfg.TotalDataNodes(); idx++ {
		builder.WriteString(fmt.Sprintf("datanode%d\n", idx))
	}
	if err := os.WriteFile(target, []byte(builder.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("  - Configured (1-%d)\n", i.cfg.TotalDataNodes())
	return nil
}

func (i *installer) ensureMapredSite() error {
	section("MAPRED-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "mapred-site.xml")
	if contentContains(target, "mapreduce.framework.name") {
		fmt.Println("  - Already configured")
		return nil
	}
	content := `<configuration>
<property>
<name>mapreduce.framework.name</name>
<value>yarn</value>
</property>
</configuration>
`
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Println("  - Configured")
	return nil
}

func (i *installer) ensureYarnSite() error {
	section("YARN-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "yarn-site.xml")
	if contentContains(target, "yarn.nodemanager.resource.memory-mb") {
		fmt.Println("  - Already configured")
		return nil
	}
	content := `<configuration>
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
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Println("  - Configured (memory=13312, vcores=14)")
	return nil
}

func (i *installer) ensureHadoopEnv() error {
	section("HADOOP-ENV.SH")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "hadoop-env.sh")
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	current := string(data)
	flexibleJava := fmt.Sprintf("export JAVA_HOME=${JAVA_HOME:-%s}", i.java11Home)
	switch {
	case strings.Contains(current, "export JAVA_HOME=${JAVA_HOME"):
		fmt.Println("  - Already configured")
		return nil
	case strings.Contains(current, "# export JAVA_HOME="):
		current = strings.Replace(current, "# export JAVA_HOME=", flexibleJava, 1)
	case strings.Contains(current, "export JAVA_HOME="):
		lines := strings.Split(current, "\n")
		for idx, line := range lines {
			if strings.HasPrefix(line, "export JAVA_HOME=") {
				lines[idx] = flexibleJava
			}
		}
		current = strings.Join(lines, "\n")
	default:
		if !strings.HasSuffix(current, "\n") {
			current += "\n"
		}
		current += flexibleJava + "\n"
	}
	if err := os.WriteFile(target, []byte(current), 0o644); err != nil {
		return err
	}
	fmt.Println("  - Updated")
	return nil
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

func (i *installer) syncConfigsToDataNodes() error {
	section("SYNC TO EXISTING DATANODES (CONFIGS ONLY)")
	for _, ip := range i.cfg.ExistingNodeIPs {
		fmt.Printf("  - Syncing to %s (existing)\n", ip)
		target := fmt.Sprintf("%s@%s:%s/", i.cfg.DataNodeUser, ip, i.hadoopHome)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "rsync", "-az", "--delete", i.hadoopHome+"/", target); err != nil {
			return err
		}
		sparkTarget := fmt.Sprintf("%s@%s:%s/", i.cfg.DataNodeUser, ip, i.sparkHome)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "rsync", "-az", "--delete", i.sparkHome+"/", sparkTarget); err != nil {
			return err
		}
	}

	section("SYNC TO NEW DATANODES (FULL)")
	for _, ip := range i.cfg.NewNodeIPs {
		fmt.Printf("  - Syncing to %s (new)\n", ip)
		target := fmt.Sprintf("%s@%s:%s/", i.cfg.DataNodeUser, ip, i.hadoopHome)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "rsync", "-az", "--delete", i.hadoopHome+"/", target); err != nil {
			return err
		}
		sparkTarget := fmt.Sprintf("%s@%s:%s/", i.cfg.DataNodeUser, ip, i.sparkHome)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "rsync", "-az", "--delete", i.sparkHome+"/", sparkTarget); err != nil {
			return err
		}
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
		"-tt",
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
	case "--recommend-only":
		return modeRecommendOnly, nil
	case "--help", "-h":
		fmt.Println("Usage:")
		fmt.Println("  foxai-installer                  Run the full installer flow")
		fmt.Println("  foxai-installer --dry-run        Print the execution plan only")
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
		value, err := i.readPrompt("Choose 1 or 2: ")
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

func runPythonScriptWithSudo(args []string, script string) error {
	cmdArgs := append([]string{"python3", "-"}, args...)
	return runCommand("", strings.NewReader(script), os.Stdout, os.Stderr, "sudo", cmdArgs...)
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
BASE_HOME="/home/$DN_USER"

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

section "/etc/hosts (MINIMAL LOCAL BLOCK)"
echo "  Note: ` + remoteDNProbeBlockNote + `"
MY_IP="$(hostname -I | awk '{print $1}')"
sudo python3 - "$NN_PRIVATE_IP" "$MY_IP" "$MY_DN_NUM" <<'PYEOF'
import sys

nn_ip, my_ip, my_num = sys.argv[1], sys.argv[2], sys.argv[3]
hosts = open("/etc/hosts").read()
begin = "# >>> FOXAI CLUSTER HOSTS >>>"
end = "# <<< FOXAI CLUSTER HOSTS <<<"
start = hosts.find(begin)
if start != -1:
    end_idx = hosts.find(end, start)
    if end_idx != -1:
        hosts = hosts[:start] + hosts[end_idx + len(end):]
minimal = f"""{begin}
{nn_ip} namenode
datanode{my_num} {my_ip}
{end}
"""
hosts = hosts.rstrip() + "\n" + minimal
open("/etc/hosts", "w").write(hosts)
PYEOF
echo "  - Written"

section "HADOOP JAVA_HOME"
HADOOP_ENV="$HADOOP_HOME/etc/hadoop/hadoop-env.sh"
if grep -qE "^export JAVA_HOME=\$\\{JAVA_HOME" "$HADOOP_ENV" 2>/dev/null; then
    echo "  - Already configured (flexible)"
elif grep -qE "^export JAVA_HOME=" "$HADOOP_ENV" 2>/dev/null; then
    sed -i "s|^export JAVA_HOME=.*|export JAVA_HOME=$JAVA_HOME|" "$HADOOP_ENV"
    echo "  - Updated"
else
    echo "export JAVA_HOME=$JAVA_HOME" >> "$HADOOP_ENV"
    echo "  - Added"
fi

section "SHELL ENVIRONMENT"
if grep -qE "HADOOP_HOME" ~/.bashrc 2>/dev/null; then
    echo "  - Already configured"
else
    cat <<EOT >> ~/.bashrc
export JAVA_HOME=$JAVA_HOME
export HADOOP_HOME=$HADOOP_HOME
export SPARK_HOME=$SPARK_HOME
export HADOOP_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export YARN_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export PATH=\$PATH:\$JAVA_HOME/bin:\$HADOOP_HOME/bin:\$HADOOP_HOME/sbin:\$SPARK_HOME/bin:\$SPARK_HOME/sbin
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh
EOT
    echo "  - Configured"
fi

export JAVA_HOME="$JAVA_HOME"
export HADOOP_HOME="$HADOOP_HOME"
export SPARK_HOME="$SPARK_HOME"
export HADOOP_CONF_DIR="$HADOOP_HOME/etc/hadoop"
export YARN_CONF_DIR="$HADOOP_HOME/etc/hadoop"
export PATH="$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin:$PATH"

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
