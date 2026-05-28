//go:build linux

package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (i *installer) ensureBootstrapDependenciesForMode() error {
	var deps []bootstrapDependency
	switch i.mode {
	case modeInstall, modePreflight, modeRepair, modeReconcile:
		deps = []bootstrapDependency{
			{Command: "python3", Package: "python3"},
			{Command: "ssh", Package: "openssh-client"},
			{Command: "ssh-copy-id", Package: "openssh-client"},
			{Command: "rsync", Package: "rsync"},
			{Command: "wget", Package: "wget"},
			{Command: "tar", Package: "tar"},
		}
	default:
		return nil
	}
	return i.ensureBootstrapDependencies(deps)
}

func (i *installer) runLocalBootstrapWithoutFreshGuard() error {
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
	return nil
}

func (i *installer) ensureRemoteBasePackages(targetIPs []string) error {
	section("REMOTE BASE PACKAGES (ALL DNs)")
	if len(targetIPs) == 0 {
		fmt.Println("  - No DataNodes requested")
		return nil
	}
	packages := strings.Join(basePackagesDataNode, " ")
	for _, ip := range targetIPs {
		target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
		fmt.Printf("  - Checking %s\n", ip)
		cmd := fmt.Sprintf(`MISSING=""; for p in %s; do dpkg -s "$p" >/dev/null 2>&1 || MISSING="$MISSING $p"; done; if [ -n "$MISSING" ]; then sudo apt-get update && sudo apt-get install -y $MISSING; echo "__FOXAI_REMOTE_INSTALLED__$MISSING"; else echo "__FOXAI_REMOTE_ALREADY__"; fi; if [ ! -e /usr/bin/python ]; then sudo ln -sf /usr/bin/python3 /usr/bin/python; fi`, packages)
		op := func() error {
			return runRemoteBashCommand(nil, os.Stdout, os.Stderr, target, cmd)
		}
		if err := i.withRemoteSSHRecovery(ip, "remote base packages", op); err != nil {
			return fmt.Errorf("failed to ensure remote base packages on %s: %w", ip, err)
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
	cmd := `sed -i 's|http://archive.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list && sed -i 's|http://security.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list`
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", cmd); err != nil {
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
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "apt", "update"); err != nil {
		return err
	}
	args := append([]string{"apt", "install", "-y"}, missing...)
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, args...); err != nil {
		return err
	}
	fmt.Printf("  - Installed:%s\n", " "+strings.Join(missing, " "))
	return nil
}

func (i *installer) ensurePythonSymlinkLocal() error {
	if fileExists("/usr/bin/python") {
		return nil
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "ln", "-sf", "/usr/bin/python3", "/usr/bin/python"); err != nil {
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
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "apt", "update"); err != nil {
		return err
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "apt", "install", "-y", pkg); err != nil {
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
		cmd := "wget -4 -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | gpg --dearmor -o /usr/share/keyrings/adoptium.gpg"
		if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", cmd); err != nil {
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
	cmd := fmt.Sprintf("echo %q > /etc/apt/sources.list.d/adoptium.list", line)
	return runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "bash", "-lc", cmd)
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
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "mv", filepath.Join(i.baseHome, pinnedSparkArtifact), i.sparkHome); err != nil {
		return err
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "chown", "-R", fmt.Sprintf("%s:%s", i.currentUser, i.currentUser), i.sparkHome); err != nil {
		return err
	}
	fmt.Println("  - Installed")
	return nil
}

func (i *installer) requireCommands(commands ...string) error {
	for _, command := range commands {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("required command not found: %s", command)
		}
	}
	return nil
}

func (i *installer) ensureBootstrapDependencies(deps []bootstrapDependency) error {
	section("BOOTSTRAP DEPENDENCIES")
	missingCommands := make([]string, 0)
	packagesSeen := make(map[string]bool)
	missingPackages := make([]string, 0)
	for _, dep := range deps {
		if _, err := exec.LookPath(dep.Command); err == nil {
			continue
		}
		missingCommands = append(missingCommands, dep.Command)
		if dep.Package != "" && !packagesSeen[dep.Package] {
			packagesSeen[dep.Package] = true
			missingPackages = append(missingPackages, dep.Package)
		}
	}
	if len(missingCommands) == 0 {
		fmt.Println("  - Already installed")
		return nil
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		return fmt.Errorf("missing bootstrap dependencies (%s) and apt-get is unavailable", strings.Join(missingCommands, ", "))
	}
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, "apt-get", "update"); err != nil {
		return err
	}
	installArgs := append([]string{"apt-get", "install", "-y"}, missingPackages...)
	if err := runElevatedCommand(os.Stdin, os.Stdout, os.Stderr, installArgs...); err != nil {
		return err
	}
	fmt.Printf("  - Installed missing base dependencies: %s\n", strings.Join(missingPackages, ", "))
	return nil
}
