//go:build linux

package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
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

func runCommandWithTimeout(timeout time.Duration, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-done:
		return err
	case <-timer.C:
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done
		return fmt.Errorf("command timed out after %s: %s %s", timeout, name, strings.Join(args, " "))
	}
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
	return runRemoteCommand(strings.NewReader(script), stdout, stderr, target, "bash", "-s", "--")
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

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	os.Exit(1)
}
