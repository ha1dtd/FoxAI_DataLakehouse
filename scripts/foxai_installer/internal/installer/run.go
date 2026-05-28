//go:build linux

package installer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func Main() {
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
		if err := i.requireCommands("python3", "ssh", "ssh-copy-id", "rsync", "wget", "tar"); err != nil {
			return err
		}
		if err := i.runNameNodeSetup(); err != nil {
			return err
		}
		targetIPs := i.installTargetIPs
		if targetIPs == nil {
			targetIPs = i.cfg.AllDataNodeIPs()
		}
		if err := i.runAllDataNodeSetups(targetIPs); err != nil {
			return err
		}
		i.addSummary("cluster", "install", statusFixed, fmt.Sprintf("cluster shape=%d datanodes, mutation targets=%d", i.cfg.TotalDataNodes(), len(targetIPs)))
		return i.finishAfterMutation()
	case modeDryRun:
		return i.printDryRunPlan()
	case modePreflight:
		if err := i.requireCommands("python3", "ssh", "ssh-copy-id", "rsync", "wget", "tar"); err != nil {
			return err
		}
		return i.runPreflight()
	case modeRepair:
		if err := i.requireCommands("python3", "ssh", "ssh-copy-id", "rsync", "wget", "tar"); err != nil {
			return err
		}
		return i.runRepair()
	case modeReconcile:
		if err := i.requireCommands("python3", "ssh", "ssh-copy-id", "rsync", "wget", "tar"); err != nil {
			return err
		}
		return i.runReconcile()
	case modeRecommendOnly:
		return i.finishAndRecommend()
	default:
		return fmt.Errorf("unsupported installer mode: %s", i.mode)
	}
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
	dir := filepath.Join(i.baseHome, ".foxai-unified-installer")
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
		const serviceStartTimeout = 2 * time.Minute

		if err := runCommandWithTimeout(serviceStartTimeout, "", os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", i.runtimeShellCommand("start-dfs.sh")); err != nil {
			i.addSummary("namenode", "hdfs start", statusWarn, err.Error())
			fmt.Printf("WARN: HDFS start step did not complete cleanly: %v\n", err)
		} else {
			i.addSummary("namenode", "hdfs start", statusOK, "start-dfs.sh completed")
		}

		if err := runCommandWithTimeout(serviceStartTimeout, "", os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", i.runtimeShellCommand("start-yarn.sh")); err != nil {
			i.addSummary("namenode", "yarn start", statusWarn, err.Error())
			fmt.Printf("WARN: YARN start step did not complete cleanly: %v\n", err)
			fmt.Println("WARN: continuing to verification summary instead of blocking indefinitely")
		} else {
			i.addSummary("namenode", "yarn start", statusOK, "start-yarn.sh completed")
		}
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
