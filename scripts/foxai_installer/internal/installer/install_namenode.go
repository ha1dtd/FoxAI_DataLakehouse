//go:build linux

package installer

import (
	"fmt"
	"os"
	"path/filepath"
)

func (i *installer) runNameNodeSetup() error {
	originalTargets := append([]string(nil), i.cfg.AllDataNodeIPs()...)
	i.installTargetIPs = nil
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
	mutationTargets, err := i.resolveReusedDataNodesForInstall(originalTargets)
	if err != nil {
		return err
	}
	if err := i.rewriteLocalConfigsForResolvedTargets(); err != nil {
		return err
	}
	i.installTargetIPs = append([]string(nil), mutationTargets...)
	if len(mutationTargets) == 0 {
		fmt.Println("  - No DataNodes require sync/setup in this run")
		return nil
	}
	if err := i.syncConfigsToDataNodes(mutationTargets); err != nil {
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
