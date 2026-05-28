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
	fmt.Printf("  - Passwordless SSH not ready for %s during %s\n", strings.Join(failedIPs, ", "), context)
	return i.recoverPasswordlessSSH(failedIPs, context)
}

func (i *installer) recoverPasswordlessSSH(failedIPs []string, context string) error {
	if len(failedIPs) == 0 {
		return nil
	}

	fmt.Printf("  - Re-running SSH bootstrap guidance for: %s\n", strings.Join(failedIPs, ", "))
	fmt.Printf("  - Context: %s\n", context)
	fmt.Println("  - Trying automatic ssh-copy-id bootstrap first")
	for _, ip := range failedIPs {
		target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		if err := runCommand("", os.Stdin, os.Stdout, os.Stderr, "ssh-copy-id", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", "-f", target); err != nil {
			if verifyErr := runCommand("", nil, io.Discard, io.Discard, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", target, "true"); verifyErr == nil {
				fmt.Printf("  - %s already reachable with the current key; continuing\n", ip)
				continue
			}
			fmt.Printf("  - %s: ssh-copy-id did not complete cleanly; will check manual fallback if still needed\n", ip)
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

	fmt.Printf("Passwordless SSH is still missing for: %s\n", strings.Join(failedIPs, ", "))
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
		fmt.Printf("  - %s: passwordless SSH not ready\n", ip)
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
	fmt.Printf("  - Passwordless SSH check failed for %s during %s. Re-running SSH bootstrap guidance.\n", ip, context)
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
