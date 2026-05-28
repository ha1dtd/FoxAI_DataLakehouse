//go:build linux

package installer

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

func (i *installer) collectInputs() error {
	fmt.Println("=== FOXAI UNIFIED INSTALLER (GO) ===")
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

	nameNodeIP, err := i.promptLocalNameNodeIP()
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

func (i *installer) promptLocalNameNodeIP() (string, error) {
	defaultIP, err := detectLocalPrivateIPv4()
	if err != nil {
		return "", err
	}
	if defaultIP == "" {
		return i.promptIP("Namenode private IP")
	}
	for {
		value, err := i.readPrompt(fmt.Sprintf("Namenode private IP [%s]: ", defaultIP))
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return defaultIP, nil
		}
		ip, err := parseSingleIPToken(value)
		if err != nil {
			fmt.Printf("  %v\n", err)
			continue
		}
		return ip, nil
	}
}

func detectLocalPrivateIPv4() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to inspect local network interfaces: %w", err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			prefix, err := netip.ParsePrefix(addr.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr()
			if !ip.Is4() || !ip.IsPrivate() {
				continue
			}
			return ip.String(), nil
		}
	}
	return "", nil
}

func (i *installer) promptDataNodeIPs(role string, count int) ([]string, error) {
	if count == 0 {
		return nil, nil
	}

	fmt.Println()
	fmt.Printf("=== %s DATANODE IPs ===\n", role)
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
				fmt.Printf("  %v\n", err)
				continue
			}
		case 3:
			value, err := i.readPrompt(fmt.Sprintf("%s DataNode IP/range expression (examples: 192.168.1.10-12,192.168.1.20): ", role))
			if err != nil {
				return nil, err
			}
			ips, err = parseDataNodeTargets(value, true)
			if err != nil {
				fmt.Printf("  %v\n", err)
				continue
			}
		}

		if len(ips) != count {
			fmt.Printf("  Expected %d IPs but resolved %d. Try again.\n", count, len(ips))
			continue
		}
		fmt.Printf("  - Resolved %d %s DataNode IP(s): %s\n", len(ips), role, strings.Join(ips, ", "))
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
