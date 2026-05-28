//go:build linux

package installer

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

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

func (i *installer) syncConfigsToDataNodes(targetIPs []string) error {
	section("SYNC TO DATANODES")
	rsyncSSH := "ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=5"
	for _, ip := range targetIPs {
		if err := i.ensureRemoteRsync(ip); err != nil {
			return err
		}
		fmt.Printf("  - Syncing to %s\n", ip)
		target := fmt.Sprintf("%s@%s:%s/", i.cfg.DataNodeUser, ip, i.hadoopHome)
		rsyncHadoop := func() error {
			return runCommand("", os.Stdin, os.Stdout, os.Stderr, "rsync", "-e", rsyncSSH, "-az", "--delete", i.hadoopHome+"/", target)
		}
		if err := i.withRemoteSSHRecovery(ip, "hadoop rsync", rsyncHadoop); err != nil {
			return err
		}
		sparkTarget := fmt.Sprintf("%s@%s:%s/", i.cfg.DataNodeUser, ip, i.sparkHome)
		rsyncSpark := func() error {
			return runCommand("", os.Stdin, os.Stdout, os.Stderr, "rsync", "-e", rsyncSSH, "-az", "--delete", "--rsync-path=sudo rsync", i.sparkHome+"/", sparkTarget)
		}
		if err := i.withRemoteSSHRecovery(ip, "spark rsync", rsyncSpark); err != nil {
			return err
		}
		targetHost := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		chownSpark := func() error {
			return runRemoteCommand(nil, os.Stdout, os.Stderr, targetHost, "sudo", "chown", "-R", fmt.Sprintf("%s:%s", i.cfg.DataNodeUser, i.cfg.DataNodeUser), i.sparkHome)
		}
		if err := i.withRemoteSSHRecovery(ip, "spark ownership normalization", chownSpark); err != nil {
			return fmt.Errorf("failed to normalize spark ownership on %s: %w", ip, err)
		}
	}
	return nil
}

func (i *installer) ensureRemoteRsync(ip string) error {
	target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
	cmd := `command -v rsync >/dev/null 2>&1 || { sudo apt-get update && sudo apt-get install -y rsync; }`
	op := func() error {
		return runRemoteBashCommand(nil, os.Stdout, os.Stderr, target, cmd)
	}
	if err := i.withRemoteSSHRecovery(ip, "ensure remote rsync", op); err != nil {
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
	op := func() error {
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
	return i.withRemoteSSHRecovery(ip, "remote datanode setup", op)
}
