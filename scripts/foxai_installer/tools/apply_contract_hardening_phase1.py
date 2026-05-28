from pathlib import Path
import textwrap


REPO_ROOT = Path(__file__).resolve().parents[3]
INSTALLER_DIR = REPO_ROOT / "scripts" / "foxai_installer" / "internal" / "installer"


def write_file(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


CONSTANTS_GO = textwrap.dedent(
    """\
    //go:build linux

    package installer

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

    const (
    	modeInstall       installerMode = "install"
    	modeDryRun        installerMode = "dry-run"
    	modePreflight     installerMode = "preflight"
    	modeRepair        installerMode = "repair"
    	modeReconcile     installerMode = "reconcile"
    	modeRecommendOnly installerMode = "recommend-only"
    )

    const (
    	statusOK      summaryStatus = "OK"
    	statusSkip    summaryStatus = "SKIP"
    	statusWarn    summaryStatus = "WARN"
    	statusDrift   summaryStatus = "DRIFT"
    	statusFixed   summaryStatus = "FIXED"
    	statusBlocked summaryStatus = "BLOCKED"
    )

    const (
    	dataNodeReuseFresh       dataNodeReuseState = "fresh"
    	dataNodeReuseCompatible  dataNodeReuseState = "compatible"
    	dataNodeReuseConflicting dataNodeReuseState = "conflicting"
    	dataNodeReusePartial     dataNodeReuseState = "partial"
    	dataNodeReuseUnreadable  dataNodeReuseState = "unreadable"
    )

    const (
    	configStateExact   configState = "exact"
    	configStateEmpty   configState = "empty"
    	configStateMissing configState = "missing"
    	configStateDrift   configState = "drift"
    )
    """
)


PROMPTS_GO = textwrap.dedent(
    """\
    //go:build linux

    package installer

    import (
    	"errors"
    	"fmt"
    	"io"
    	"net/netip"
    	"strconv"
    	"strings"
    )

    func (i *installer) collectInputs() error {
    	fmt.Println("=== FOXAI UNIFIED INSTALLER (GO) ===")
    	fmt.Printf("Mode: %s\\n", i.mode)
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
    	fmt.Printf("Pinned versions:\\n")
    	fmt.Printf("  - Hadoop: %s\\n", pinnedHadoopVersion)
    	fmt.Printf("  - Spark:  %s\\n", pinnedSparkArtifact)
    	fmt.Printf("  - Java 11 package: %s\\n", pinnedJava11Package)
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

    	existingIPs, err := i.promptDataNodeIPs("EXISTING", existingCount)
    	if err != nil {
    		return err
    	}
    	newIPs, err := i.promptDataNodeIPs("NEW", newCount)
    	if err != nil {
    		return err
    	}

    	i.cfg = installerConfig{
    		NameNodePrivateIP: nameNodeIP,
    		ExistingNodeIPs:   existingIPs,
    		NewNodeIPs:        newIPs,
    	}

    	derivedMinIOEndpoint := fmt.Sprintf("%s:9001", nameNodeIP)

    	dataNodeUser, err := i.promptOptional("Datanode username", i.currentUser)
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
    	fmt.Printf("  - Namenode IP: %s\\n", i.cfg.NameNodePrivateIP)
    	fmt.Printf("  - Existing datanodes: %d\\n", len(i.cfg.ExistingNodeIPs))
    	fmt.Printf("  - New datanodes: %d\\n", len(i.cfg.NewNodeIPs))
    	fmt.Printf("  - Total datanodes: %d\\n", i.cfg.TotalDataNodes())
    	fmt.Printf("  - Datanode username: %s\\n", i.cfg.DataNodeUser)
    	fmt.Printf("  - MinIO endpoint: %s\\n", i.cfg.MinIOEndpoint)
    	fmt.Printf("  - MinIO access key: %s\\n", i.cfg.MinIOAccessKey)
    	fmt.Printf("  - MinIO secret key: [hidden]\\n")
    	if i.cfg.UseKakaoMirror {
    		fmt.Println("  - Kakao mirror override: yes")
    	} else {
    		fmt.Println("  - Kakao mirror override: no")
    	}
    	fmt.Println()
    	return nil
    }

    func (i *installer) promptDataNodeIPs(role string, count int) ([]string, error) {
    	if count == 0 {
    		return nil, nil
    	}

    	fmt.Println()
    	fmt.Printf("=== %s DATANODE IPs ===\\n", role)
    	for {
    		choice, err := i.promptChoice(
    			fmt.Sprintf("How do you want to enter %d %s DataNode IP(s)?", count, strings.ToLower(role)),
    			[]string{
    				"1. Enter IPs one by one",
    				"2. Paste a comma-separated IP list",
    				"3. Enter a combined IP/range expression",
    			},
    		)
    		if err != nil {
    			return nil, err
    		}

    		var ips []string
    		switch choice {
    		case 1:
    			ips = make([]string, 0, count)
    			for idx := 1; idx <= count; idx++ {
    				ip, err := i.promptIP(fmt.Sprintf("  %s DN%d IP", role, idx))
    				if err != nil {
    					return nil, err
    				}
    				ips = append(ips, ip)
    			}
    		case 2:
    			value, err := i.readPrompt(fmt.Sprintf("%s DataNode IPs (comma-separated, exactly %d): ", role, count))
    			if err != nil {
    				return nil, err
    			}
    			ips, err = parseDataNodeTargets(value, false)
    			if err != nil {
    				fmt.Printf("  %v\\n", err)
    				continue
    			}
    		case 3:
    			value, err := i.readPrompt(fmt.Sprintf("%s DataNode IP/range expression (examples: 192.168.1.10-12,192.168.1.20): ", role))
    			if err != nil {
    				return nil, err
    			}
    			ips, err = parseDataNodeTargets(value, true)
    			if err != nil {
    				fmt.Printf("  %v\\n", err)
    				continue
    			}
    		}

    		if len(ips) != count {
    			fmt.Printf("  Expected %d IPs but resolved %d. Try again.\\n", count, len(ips))
    			continue
    		}
    		fmt.Printf("  - Resolved %d %s DataNode IP(s): %s\\n", len(ips), role, strings.Join(ips, ", "))
    		return ips, nil
    	}
    }

    func parseDataNodeTargets(value string, allowRanges bool) ([]string, error) {
    	rawParts := strings.Split(value, ",")
    	ips := make([]string, 0, len(rawParts))
    	seen := make(map[string]bool)

    	for _, rawPart := range rawParts {
    		token := strings.TrimSpace(rawPart)
    		if token == "" {
    			return nil, fmt.Errorf("input contains an empty IP entry")
    		}

    		expanded, err := expandDataNodeTargetToken(token, allowRanges)
    		if err != nil {
    			return nil, err
    		}
    		for _, ip := range expanded {
    			if seen[ip] {
    				return nil, fmt.Errorf("duplicate IP detected: %s", ip)
    			}
    			seen[ip] = true
    			ips = append(ips, ip)
    		}
    	}

    	return ips, nil
    }

    func expandDataNodeTargetToken(token string, allowRanges bool) ([]string, error) {
    	if allowRanges && strings.Contains(token, "-") {
    		return expandIPv4RangeToken(token)
    	}
    	ip, err := parseSingleIPToken(token)
    	if err != nil {
    		return nil, err
    	}
    	return []string{ip}, nil
    }

    func parseSingleIPToken(token string) (string, error) {
    	addr, err := netip.ParseAddr(token)
    	if err != nil {
    		return "", fmt.Errorf("invalid IP address %q", token)
    	}
    	return addr.String(), nil
    }

    func expandIPv4RangeToken(token string) ([]string, error) {
    	parts := strings.SplitN(token, "-", 2)
    	if len(parts) != 2 {
    		return nil, fmt.Errorf("invalid IP range %q", token)
    	}

    	startAddr, err := netip.ParseAddr(strings.TrimSpace(parts[0]))
    	if err != nil {
    		return nil, fmt.Errorf("invalid range start %q", parts[0])
    	}
    	if !startAddr.Is4() {
    		return nil, fmt.Errorf("IP ranges currently support IPv4 only: %q", token)
    	}
    	startBytes := startAddr.As4()
    	startLastOctet := int(startBytes[3])

    	endToken := strings.TrimSpace(parts[1])
    	endLastOctet := 0
    	if numericEnd, convErr := strconv.Atoi(endToken); convErr == nil {
    		endLastOctet = numericEnd
    	} else {
    		endAddr, err := netip.ParseAddr(endToken)
    		if err != nil {
    			return nil, fmt.Errorf("invalid range end %q", endToken)
    		}
    		if !endAddr.Is4() {
    			return nil, fmt.Errorf("IP ranges currently support IPv4 only: %q", token)
    		}
    		endBytes := endAddr.As4()
    		if startBytes[0] != endBytes[0] || startBytes[1] != endBytes[1] || startBytes[2] != endBytes[2] {
    			return nil, fmt.Errorf("range %q must stay within one /24 for now", token)
    		}
    		endLastOctet = int(endBytes[3])
    	}

    	if endLastOctet < startLastOctet {
    		return nil, fmt.Errorf("range end must be >= start in %q", token)
    	}
    	if endLastOctet > 255 {
    		return nil, fmt.Errorf("range end octet out of range in %q", token)
    	}

    	ips := make([]string, 0, endLastOctet-startLastOctet+1)
    	for lastOctet := startLastOctet; lastOctet <= endLastOctet; lastOctet++ {
    		addr := netip.AddrFrom4([4]byte{startBytes[0], startBytes[1], startBytes[2], byte(lastOctet)})
    		ips = append(ips, addr.String())
    	}
    	return ips, nil
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
    	fmt.Printf("Finish with a line containing only %s\\n", endMarker)
    	lines := make([]string, 0)
    	for {
    		value, err := i.reader.ReadString('\\n')
    		if err != nil && !errors.Is(err, io.EOF) {
    			return "", err
    		}
    		line := strings.TrimRight(value, "\\r\\n")
    		if line == endMarker {
    			return ensureTrailingNewline(strings.Join(lines, "\\n")), nil
    		}
    		lines = append(lines, line)
    		if errors.Is(err, io.EOF) {
    			return "", fmt.Errorf("reached EOF before %s marker", endMarker)
    		}
    	}
    }

    func (i *installer) resolveInstallDrift(component, details string) (int, string, error) {
    	fmt.Printf("  - Drift detected in %s: %s\\n", component, details)
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
    	value, err := i.reader.ReadString('\\n')
    	if err != nil && !errors.Is(err, io.EOF) {
    		return "", err
    	}
    	return strings.TrimRight(value, "\\r\\n"), nil
    }
    """
)


REUSE_GO = textwrap.dedent(
    """\
    //go:build linux

    package installer

    import (
    	"bytes"
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
    	fmt.Printf("  - Local NameNode clusterID: %s\\n", clusterID)

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
    		if isActionableReuseState(probe.State) {
    			actionable = append(actionable, probe)
    		}
    	}
    	if len(actionable) == 0 {
    		fmt.Println("  - All DataNodes are fresh or compatible")
    		return originalTargets, nil
    	}

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

    	skipped := make(map[string]bool)
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
    	fmt.Printf("  - Active DataNodes for this run: %s\\n", strings.Join(activeTargets, ", "))
    	return activeTargets, nil
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
    		if err := i.wipeRemoteDataNodeStorage(probe.IP); err != nil {
    			return err
    		}
    		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusFixed, fmt.Sprintf("%s node wiped and reused", probe.State))
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
    			"2. Wipe old HDFS DataNode storage and reuse this node (Recommended)",
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
    		if err := i.wipeRemoteDataNodeStorage(probe.IP); err != nil {
    			return err
    		}
    		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusFixed, fmt.Sprintf("%s node wiped and reused", probe.State))
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
    			"2. Wipe old HDFS DataNode storage and reuse this node (Recommended)",
    			"3. Continue with normal installer sync/setup",
    			"4. Skip this node for this run",
    		)
    		wipeChoice = 2
    		continueChoice = 3
    		skipChoice = 4
    	} else {
    		options = append(options,
    			"2. Continue with normal installer sync/setup (Recommended)",
    			"3. Wipe old HDFS DataNode storage and reuse this node",
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
    		if err := i.wipeRemoteDataNodeStorage(probe.IP); err != nil {
    			return err
    		}
    		i.addSummary(i.nodeLabelForIP(probe.IP), "datanode state", statusFixed, "partial node wiped and reused")
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
    	for _, rawLine := range strings.Split(text, "\\n") {
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
    	hadoopHome := filepath.Join("/home", i.cfg.DataNodeUser, "hadoop")
    	sparkHome := i.sparkHome
    	bashrcPath := filepath.Join("/home", i.cfg.DataNodeUser, ".bashrc")
    	cmd := fmt.Sprintf(`DATA_DIR=%q
    VERSION_PATH=%q
    HADOOP_HOME=%q
    SPARK_HOME=%q
    BASHRC=%q
    LOCAL_CLUSTER_ID=%q
    CORE_SITE="$HADOOP_HOME/etc/hadoop/core-site.xml"
    HDFS_SITE="$HADOOP_HOME/etc/hadoop/hdfs-site.xml"
    WORKERS_FILE="$HADOOP_HOME/etc/hadoop/workers"
    HADOOP_ENV="$HADOOP_HOME/etc/hadoop/hadoop-env.sh"
    PARTIAL_REASONS=()
    MANAGED_SIGNALS=0

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
      [ -f "$HADOOP_ENV" ] || MISSING_HADOOP+=("hadoop-env.sh")
      if [ ${#MISSING_HADOOP[@]} -gt 0 ]; then
        PARTIAL_REASONS+=("hadoop config incomplete: ${MISSING_HADOOP[*]}")
      fi
    fi

    if [ -d "$SPARK_HOME" ]; then
      MANAGED_SIGNALS=1
      if [ ! -x "$SPARK_HOME/bin/spark-submit" ]; then
        PARTIAL_REASONS+=("spark home exists but spark-submit is missing")
      fi
    fi

    if [ -f "$BASHRC" ]; then
      HAS_ENV_BEGIN=0
      HAS_ENV_END=0
      grep -Fq %q "$BASHRC" && HAS_ENV_BEGIN=1
      grep -Fq %q "$BASHRC" && HAS_ENV_END=1
      if [ "$HAS_ENV_BEGIN" -eq 1 ] || [ "$HAS_ENV_END" -eq 1 ]; then
        MANAGED_SIGNALS=1
        if [ "$HAS_ENV_BEGIN" -ne 1 ] || [ "$HAS_ENV_END" -ne 1 ]; then
          PARTIAL_REASONS+=("managed env block is incomplete")
        fi
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
      printf 'fresh||no datanode storage or managed runtime\n'
    elif [ -f "$VERSION_PATH" ]; then
      CID=$(awk -F= '$1=="clusterID"{print $2}' "$VERSION_PATH" | tr -d '[:space:]')
      if [ -z "$CID" ]; then
        if [ -n "$DETAILS" ]; then
          DETAILS="VERSION file exists but clusterID missing; $DETAILS"
        else
          DETAILS="VERSION file exists but clusterID missing"
        fi
        printf 'partial||%%s\n' "$DETAILS"
      elif [ "$CID" != "$LOCAL_CLUSTER_ID" ]; then
        printf 'found|%%s|clusterID differs from current NameNode (%%s)\n' "$CID" "$LOCAL_CLUSTER_ID"
      elif [ -n "$DETAILS" ]; then
        printf 'partial|%%s|clusterID matches current NameNode; %%s\n' "$CID" "$DETAILS"
      else
        printf 'found|%%s|clusterID matches current NameNode\n' "$CID"
      fi
    elif [ -n "$DETAILS" ]; then
      printf 'partial||%%s\n' "$DETAILS"
    else
      printf 'unreadable||unable to classify datanode state\n'
    fi`, dataNodeDir, versionPath, hadoopHome, sparkHome, bashrcPath, localClusterID, envBegin, envEnd)
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
    	case "partial":
    		return dataNodeReuseProbe{IP: ip, State: dataNodeReusePartial, ClusterID: strings.TrimSpace(fields[1]), Details: fields[2]}
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
    	fmt.Printf("  - Wiping old HDFS DataNode storage on %s\\n", ip)
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
    """
)


def main() -> None:
    write_file(INSTALLER_DIR / "constants.go", CONSTANTS_GO)
    write_file(INSTALLER_DIR / "prompts.go", PROMPTS_GO)
    write_file(INSTALLER_DIR / "reuse.go", REUSE_GO)


if __name__ == "__main__":
    main()
