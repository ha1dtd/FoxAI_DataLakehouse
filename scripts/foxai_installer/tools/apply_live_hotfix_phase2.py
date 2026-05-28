from pathlib import Path
import textwrap


REPO_ROOT = Path(__file__).resolve().parents[3]
INSTALLER_DIR = REPO_ROOT / "scripts" / "foxai_installer" / "internal" / "installer"


def write_text(path: Path, content: str) -> None:
    path.write_text(content, encoding="utf-8")


def replace_block(text: str, start_marker: str, end_marker: str, replacement: str) -> str:
    start = text.index(start_marker)
    end = text.index(end_marker, start)
    return text[:start] + replacement + text[end:]


EXEC_GO = textwrap.dedent(
    """\
    //go:build linux

    package installer

    import (
    	"fmt"
    	"io"
    	"os"
    	"os/exec"
    	"strings"
    )

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

    func remoteSSHArgs(target string) []string {
    	return []string{
    		"-o", "BatchMode=yes",
    		"-o", "StrictHostKeyChecking=no",
    		"-o", "ConnectTimeout=5",
    		"-T",
    		target,
    	}
    }

    func runRemoteCommand(stdin io.Reader, stdout, stderr io.Writer, target string, remoteArgs ...string) error {
    	args := append(remoteSSHArgs(target), remoteArgs...)
    	return runCommand("", stdin, stdout, stderr, "ssh", args...)
    }

    func runRemoteBashCommand(stdin io.Reader, stdout, stderr io.Writer, target, script string) error {
    	return runRemoteCommand(stdin, stdout, stderr, target, fmt.Sprintf("bash -lc %q", script))
    }

    func section(title string) {
    	fmt.Println("====================")
    	fmt.Printf("STEP: %s\\n", title)
    	fmt.Println("====================")
    }

    func mustOutput(name string, args ...string) string {
    	out, err := exec.Command(name, args...).Output()
    	if err != nil {
    		fatal(err)
    	}
    	return string(out)
    }

    func fatal(err error) {
    	if err == nil {
    		return
    	}
    	fmt.Fprintf(os.Stderr, "ERROR: %v\\n", err)
    	os.Exit(1)
    }
    """
)


SSH_GO = textwrap.dedent(
    """\
    //go:build linux

    package installer

    import (
    	"fmt"
    	"io"
    	"os"
    	"path/filepath"
    	"strings"
    )

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
    	section("SSH BOOTSTRAP (ALL DNs)")
    	failedIPs := i.passwordlessSSHFailures(i.cfg.AllDataNodeIPs())
    	if len(failedIPs) == 0 {
    		fmt.Println("  - Passwordless SSH already verified on all DataNodes")
    		return nil
    	}
    	return i.recoverPasswordlessSSH(failedIPs, "initial SSH bootstrap")
    }

    func (i *installer) ensurePasswordlessSSHOrRecover(targetIPs []string, context string) error {
    	failedIPs := i.passwordlessSSHFailures(targetIPs)
    	if len(failedIPs) == 0 {
    		return nil
    	}
    	fmt.Printf("  - Passwordless SSH not ready for %s during %s\\n", strings.Join(failedIPs, ", "), context)
    	return i.recoverPasswordlessSSH(failedIPs, context)
    }

    func (i *installer) recoverPasswordlessSSH(failedIPs []string, context string) error {
    	if len(failedIPs) == 0 {
    		return nil
    	}

    	fmt.Printf("  - Re-running SSH bootstrap guidance for: %s\\n", strings.Join(failedIPs, ", "))
    	fmt.Printf("  - Context: %s\\n", context)
    	fmt.Println("  - Trying automatic ssh-copy-id bootstrap first")
    	for _, ip := range failedIPs {
    		target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
    		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "ssh-copy-id", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-f", target); err != nil {
    			if verifyErr := runCommand("", nil, io.Discard, io.Discard, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", target, "true"); verifyErr == nil {
    				fmt.Printf("  - %s already reachable with the current key; continuing\\n", ip)
    				continue
    			}
    			fmt.Printf("  - %s: ssh-copy-id did not complete cleanly; will check manual fallback if still needed\\n", ip)
    		}
    	}

    	failedIPs = i.passwordlessSSHFailures(failedIPs)
    	if len(failedIPs) == 0 {
    		fmt.Println("  - Passwordless SSH verified after recovery")
    		return nil
    	}

    	publicKeyPath := filepath.Join(i.baseHome, ".ssh", "id_rsa.pub")
    	pubKeyBytes, err := os.ReadFile(publicKeyPath)
    	if err != nil {
    		return fmt.Errorf("failed to read public key %s: %w", publicKeyPath, err)
    	}
    	pubKey := strings.TrimSpace(string(pubKeyBytes))

    	fmt.Printf("Passwordless SSH is still missing for: %s\\n", strings.Join(failedIPs, ", "))
    	fmt.Println("Automatic ssh-copy-id was not sufficient for every node.")
    	fmt.Println("Run the following on EACH remaining DataNode terminal before continuing:")
    	fmt.Println("  mkdir -p ~/.ssh")
    	fmt.Println("  chmod 700 ~/.ssh")
    	fmt.Println("  touch ~/.ssh/authorized_keys")
    	fmt.Println("  chmod 600 ~/.ssh/authorized_keys")
    	fmt.Println("  nano ~/.ssh/authorized_keys")
    	fmt.Println("Paste this NameNode public key into the DataNode authorized_keys file:")
    	fmt.Println()
    	fmt.Println(pubKey)
    	fmt.Println()
    	fmt.Println("After pasting the key on every remaining DataNode, save the file and return here.")
    	if _, err := i.readPrompt("Press Enter to verify passwordless SSH to the remaining DataNodes..."); err != nil {
    		return err
    	}

    	failedIPs = i.passwordlessSSHFailures(failedIPs)
    	for _, ip := range failedIPs {
    		fmt.Printf("  - %s: passwordless SSH not ready\\n", ip)
    	}
    	if len(failedIPs) > 0 {
    		return fmt.Errorf("passwordless SSH is still unavailable for: %s", strings.Join(failedIPs, ", "))
    	}
    	fmt.Println("  - Passwordless SSH verified after manual recovery")
    	return nil
    }

    func (i *installer) retryIfSSHNotReady(ip, context string, originalErr error) (bool, error) {
    	if len(i.passwordlessSSHFailures([]string{ip})) == 0 {
    		return false, originalErr
    	}
    	fmt.Printf("  - Passwordless SSH check failed for %s during %s. Re-running SSH bootstrap guidance.\\n", ip, context)
    	if err := i.recoverPasswordlessSSH([]string{ip}, context); err != nil {
    		return false, err
    	}
    	return true, nil
    }

    func (i *installer) withRemoteSSHRecovery(ip, context string, op func() error) error {
    	if err := op(); err != nil {
    		retried, recoveryErr := i.retryIfSSHNotReady(ip, context, err)
    		if recoveryErr != nil {
    			return recoveryErr
    		}
    		if !retried {
    			return err
    		}
    		return op()
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
    		fmt.Printf("  - Checking %s\\n", ip)
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
    """
)


INSTALL_DATANODE_GO = textwrap.dedent(
    """\
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
    		fmt.Printf("  - Syncing to %s\\n", ip)
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
    """
)


def main() -> None:
    write_text(INSTALLER_DIR / "exec.go", EXEC_GO)
    write_text(INSTALLER_DIR / "ssh.go", SSH_GO)
    write_text(INSTALLER_DIR / "install_datanode.go", INSTALL_DATANODE_GO)

    bootstrap_path = INSTALLER_DIR / "bootstrap.go"
    bootstrap_text = bootstrap_path.read_text(encoding="utf-8")
    old_bootstrap = textwrap.dedent(
        """\
        func (i *installer) ensureRemoteBasePackages(targetIPs []string) error {
        \tsection("REMOTE BASE PACKAGES (ALL DNs)")
        \tif len(targetIPs) == 0 {
        \t\tfmt.Println("  - No DataNodes requested")
        \t\treturn nil
        \t}
        \tpackages := strings.Join(basePackagesDataNode, " ")
        \tfor _, ip := range targetIPs {
        \t\ttarget := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
        \t\tfmt.Printf("  - Checking %s\\n", ip)
        \t\tcmd := fmt.Sprintf(`MISSING=""; for p in %s; do dpkg -s "$p" >/dev/null 2>&1 || MISSING="$MISSING $p"; done; if [ -n "$MISSING" ]; then sudo apt-get update && sudo apt-get install -y $MISSING; echo "__FOXAI_REMOTE_INSTALLED__$MISSING"; else echo "__FOXAI_REMOTE_ALREADY__"; fi; if [ ! -e /usr/bin/python ]; then sudo ln -sf /usr/bin/python3 /usr/bin/python; fi`, packages)
        \t\tif err := runCommand("", nil, os.Stdout, os.Stderr, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-T", target, "bash", "-lc", cmd); err != nil {
        \t\t\treturn fmt.Errorf("failed to ensure remote base packages on %s: %w", ip, err)
        \t\t}
        \t}
        \treturn nil
        }
        """
    )
    new_bootstrap = textwrap.dedent(
        """\
        func (i *installer) ensureRemoteBasePackages(targetIPs []string) error {
        \tsection("REMOTE BASE PACKAGES (ALL DNs)")
        \tif len(targetIPs) == 0 {
        \t\tfmt.Println("  - No DataNodes requested")
        \t\treturn nil
        \t}
        \tpackages := strings.Join(basePackagesDataNode, " ")
        \tfor _, ip := range targetIPs {
        \t\ttarget := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
        \t\tfmt.Printf("  - Checking %s\\n", ip)
        \t\tcmd := fmt.Sprintf(`MISSING=""; for p in %s; do dpkg -s "$p" >/dev/null 2>&1 || MISSING="$MISSING $p"; done; if [ -n "$MISSING" ]; then sudo apt-get update && sudo apt-get install -y $MISSING; echo "__FOXAI_REMOTE_INSTALLED__$MISSING"; else echo "__FOXAI_REMOTE_ALREADY__"; fi; if [ ! -e /usr/bin/python ]; then sudo ln -sf /usr/bin/python3 /usr/bin/python; fi`, packages)
        \t\top := func() error {
        \t\t\treturn runRemoteBashCommand(nil, os.Stdout, os.Stderr, target, cmd)
        \t\t}
        \t\tif err := i.withRemoteSSHRecovery(ip, "remote base packages", op); err != nil {
        \t\t\treturn fmt.Errorf("failed to ensure remote base packages on %s: %w", ip, err)
        \t\t}
        \t}
        \treturn nil
        }
        """
    )
    bootstrap_text = bootstrap_text.replace(old_bootstrap, new_bootstrap)
    write_text(bootstrap_path, bootstrap_text)

    reuse_path = INSTALLER_DIR / "reuse.go"
    reuse_text = reuse_path.read_text(encoding="utf-8")
    old_probe = textwrap.dedent(
        """\
        \tif err := runCommand("", nil, &stdout, &stderr, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-T", target, "bash", "-lc", cmd); err != nil {
        \t\tdetails := strings.TrimSpace(stderr.String())
        \t\tif details == "" {
        \t\t\tdetails = err.Error()
        \t\t}
        \t\treturn dataNodeReuseProbe{
        \t\t\tIP:      ip,
        \t\t\tState:   dataNodeReuseUnreadable,
        \t\t\tDetails: fmt.Sprintf("probe failed: %s", details),
        \t\t}
        \t}
        """
    )
    new_probe = textwrap.dedent(
        """\
        \trunProbe := func() error {
        \t\tstdout.Reset()
        \t\tstderr.Reset()
        \t\treturn runRemoteBashCommand(nil, &stdout, &stderr, target, cmd)
        \t}
        \tif err := i.withRemoteSSHRecovery(ip, "reused datanode probe", runProbe); err != nil {
        \t\tdetails := strings.TrimSpace(stderr.String())
        \t\tif details == "" {
        \t\t\tdetails = err.Error()
        \t\t}
        \t\treturn dataNodeReuseProbe{
        \t\t\tIP:      ip,
        \t\t\tState:   dataNodeReuseUnreadable,
        \t\t\tDetails: fmt.Sprintf("probe failed: %s", details),
        \t\t}
        \t}
        """
    )
    reuse_text = reuse_text.replace(old_probe, new_probe)

    old_wipe = textwrap.dedent(
        """\
        \tif err := runCommand("", nil, os.Stdout, os.Stderr, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-T", target, "bash", "-lc", cmd); err != nil {
        \t\treturn fmt.Errorf("failed to wipe old HDFS DataNode storage on %s: %w", ip, err)
        \t}
        """
    )
    new_wipe = textwrap.dedent(
        """\
        \top := func() error {
        \t\treturn runRemoteBashCommand(nil, os.Stdout, os.Stderr, target, cmd)
        \t}
        \tif err := i.withRemoteSSHRecovery(ip, "wipe reused datanode storage", op); err != nil {
        \t\treturn fmt.Errorf("failed to wipe old HDFS DataNode storage on %s: %w", ip, err)
        \t}
        """
    )
    reuse_text = reuse_text.replace(old_wipe, new_wipe)
    write_text(reuse_path, reuse_text)


if __name__ == "__main__":
    main()
