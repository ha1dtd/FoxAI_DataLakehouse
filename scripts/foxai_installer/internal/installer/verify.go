//go:build linux

package installer

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
		"ssh-copy-id to all DataNodes, then manual SSH bootstrap fallback if needed",
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
	if err := runCommandWithTimeout(20*time.Second, "", nil, &jpsOut, os.Stderr, "bash", "-lc", i.runtimeShellCommand("jps")); err == nil {
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
	if err := runCommandWithTimeout(30*time.Second, "", nil, &yarnOut, os.Stderr, "bash", "-lc", i.runtimeShellCommand("yarn node -list")); err == nil {
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
