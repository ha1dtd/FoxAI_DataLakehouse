//go:build linux

package installer

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (i *installer) ensureBashrc() error {
	section("BASHRC")
	bashrcPath := filepath.Join(i.baseHome, ".bashrc")
	content, err := os.ReadFile(bashrcPath)
	if err != nil {
		return err
	}
	desiredBlock := i.desiredEnvBlock()
	blockToApply := desiredBlock
	state, details, err := evaluateManagedBlockFile(bashrcPath, envBegin, envEnd, desiredBlock)
	if err != nil {
		return err
	}
	if state == configStateExact {
		fmt.Println("  - Already configured")
		return nil
	}
	if state == configStateDrift && i.mode == modeInstall {
		choice, custom, err := i.resolveInstallDrift("~/.bashrc managed block", details)
		if err != nil {
			return err
		}
		switch choice {
		case 1:
			return fmt.Errorf("install stopped by user after ~/.bashrc managed block drift")
		case 3:
			blockToApply = custom
		case 4:
			fmt.Println("  - Skipped drifted managed block")
			return nil
		}
	}
	updated, err := upsertManagedBlockText(string(content), envBegin, envEnd, blockToApply)
	if err != nil {
		return err
	}
	if err := os.WriteFile(bashrcPath, []byte(updated), 0o644); err != nil {
		return err
	}
	if err := runCommand("", nil, io.Discard, io.Discard, "bash", "-lc", "source ~/.bashrc >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("failed to source ~/.bashrc after update: %w", err)
	}
	if state == configStateDrift {
		fmt.Println("  - Updated drifted managed block")
	} else {
		fmt.Println("  - Configured")
	}
	return nil
}

func (i *installer) updateHostsBlockLocal() error {
	section("/etc/hosts")
	desired := i.desiredLocalHostsBlock()
	blockToApply := desired
	state, details, err := evaluateManagedBlockFile("/etc/hosts", hostsBegin, hostsEnd, desired)
	if err != nil {
		return err
	}
	if state == configStateExact {
		fmt.Println("  - Already configured")
		return nil
	}
	if state == configStateDrift && i.shouldAllowManagedOverwrite() {
	} else if state == configStateDrift && i.mode == modeInstall {
		choice, custom, err := i.resolveInstallDrift("/etc/hosts managed block", details)
		if err != nil {
			return err
		}
		switch choice {
		case 1:
			return fmt.Errorf("install stopped by user after /etc/hosts managed block drift")
		case 3:
			blockToApply = custom
		case 4:
			fmt.Println("  - Skipped drifted managed block")
			return nil
		}
	}

	tempFile, err := os.CreateTemp("", "foxai-hosts-block-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())
	if _, err := tempFile.WriteString(blockToApply); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	script := `
import sys
block_file = sys.argv[1]
hosts_path = "/etc/hosts"
with open(block_file) as f:
    new_block = f.read()
with open(hosts_path) as f:
    text = f.read()
begin = "# >>> FOXAI CLUSTER HOSTS >>>"
end = "# <<< FOXAI CLUSTER HOSTS <<<"
start = text.find(begin)
if start != -1:
    end_idx = text.find(end, start)
    if end_idx != -1:
        end_idx += len(end)
        text = text[:start] + text[end_idx:]
if text and not text.endswith("\n"):
    text += "\n"
text += new_block
with open(hosts_path, "w") as f:
    f.write(text)
print("done")
`
	if err := runPythonScriptWithSudo([]string{tempFile.Name()}, script); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Updated drifted managed block")
	} else {
		fmt.Println("  - Updated")
	}
	return nil
}

func (i *installer) ensureHadoopConfigs() error {
	if err := i.ensureCoreSite(); err != nil {
		return err
	}
	if err := i.ensureHdfsSite(); err != nil {
		return err
	}
	if err := i.ensureWorkersFile(); err != nil {
		return err
	}
	if err := i.ensureMapredSite(); err != nil {
		return err
	}
	if err := i.ensureYarnSite(); err != nil {
		return err
	}
	if err := i.ensureHadoopEnv(); err != nil {
		return err
	}
	return nil
}

func (i *installer) ensureCoreSite() error {
	section("CORE-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "core-site.xml")
	content := i.desiredCoreSiteContent()
	state, details, err := evaluateHadoopXMLFile(target, i.expectedCoreSiteProperties())
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("core-site.xml", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after core-site.xml drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("core-site.xml", details)
		}
	}
	if content == "" {
		content = i.desiredCoreSiteContent()
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Overwrote drifted config")
	} else {
		fmt.Println("  - Configured")
	}
	return nil
}

func (i *installer) ensureHdfsSite() error {
	section("HDFS-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "hdfs-site.xml")
	content := i.desiredHdfsSiteContent()
	state, details, err := evaluateHadoopXMLFile(target, i.expectedHdfsSiteProperties())
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("hdfs-site.xml", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after hdfs-site.xml drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("hdfs-site.xml", details)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Printf("  - Overwrote drifted config (replication=%d)\n", minInt(3, maxInt(1, i.cfg.TotalDataNodes())))
	} else {
		fmt.Printf("  - Configured (replication=%d)\n", minInt(3, maxInt(1, i.cfg.TotalDataNodes())))
	}
	return nil
}

func (i *installer) ensureWorkersFile() error {
	section("WORKERS FILE")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "workers")
	expected := i.expectedWorkers()
	content := strings.Join(expected, "\n") + "\n"
	state, details, err := evaluateWorkersFile(target, expected)
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("workers", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after workers drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("workers", details)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Printf("  - Overwrote drifted config (1-%d)\n", i.cfg.TotalDataNodes())
	} else {
		fmt.Printf("  - Configured (1-%d)\n", i.cfg.TotalDataNodes())
	}
	return nil
}

func (i *installer) ensureMapredSite() error {
	section("MAPRED-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "mapred-site.xml")
	content := i.desiredMapredSiteContent()
	state, details, err := evaluateHadoopXMLFile(target, i.expectedMapredSiteProperties())
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("mapred-site.xml", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after mapred-site.xml drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("mapred-site.xml", details)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Overwrote drifted config")
	} else {
		fmt.Println("  - Configured")
	}
	return nil
}

func (i *installer) ensureYarnSite() error {
	section("YARN-SITE.XML")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "yarn-site.xml")
	content := i.desiredYarnSiteContent()
	state, details, err := evaluateHadoopXMLFile(target, i.expectedYarnSiteProperties())
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("yarn-site.xml", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after yarn-site.xml drift")
			case 3:
				content = custom
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("yarn-site.xml", details)
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Overwrote drifted config (memory=13312, vcores=14)")
	} else {
		fmt.Println("  - Configured (memory=13312, vcores=14)")
	}
	return nil
}

func (i *installer) ensureHadoopEnv() error {
	section("HADOOP-ENV.SH")
	target := filepath.Join(i.hadoopHome, "etc", "hadoop", "hadoop-env.sh")
	flexibleJava := fmt.Sprintf("export JAVA_HOME=${JAVA_HOME:-%s}", i.java11Home)
	fixedJava := fmt.Sprintf("export JAVA_HOME=%s", i.java11Home)
	state, details, err := evaluateJavaEnvFile(target, []string{flexibleJava, fixedJava})
	if err != nil {
		return err
	}
	switch state {
	case configStateExact:
		fmt.Println("  - Already configured")
		return nil
	case configStateDrift:
		if i.shouldAllowManagedOverwrite() {
		} else if i.mode == modeInstall {
			choice, custom, err := i.resolveInstallDrift("hadoop-env.sh", details)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				return fmt.Errorf("install stopped by user after hadoop-env.sh drift")
			case 3:
				if err := os.WriteFile(target, []byte(custom), 0o644); err != nil {
					return err
				}
				fmt.Println("  - Replaced with custom content")
				return nil
			case 4:
				fmt.Println("  - Skipped drifted config")
				return nil
			}
		} else {
			return driftError("hadoop-env.sh", details)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	current := string(data)
	switch {
	case strings.Contains(current, "# export JAVA_HOME="):
		current = strings.Replace(current, "# export JAVA_HOME=", flexibleJava, 1)
	default:
		if !strings.HasSuffix(current, "\n") {
			current += "\n"
		}
		current += flexibleJava + "\n"
	}
	if err := os.WriteFile(target, []byte(current), 0o644); err != nil {
		return err
	}
	if state == configStateDrift {
		fmt.Println("  - Overwrote drifted JAVA_HOME")
	} else {
		fmt.Println("  - Updated")
	}
	return nil
}

func (i *installer) desiredCoreSiteContent() string {
	return `<configuration>
  <property>
    <name>fs.defaultFS</name>
    <value>hdfs://namenode:9000</value>
  </property>
</configuration>
`
}

func (i *installer) desiredHdfsSiteContent() string {
	replication := i.cfg.TotalDataNodes()
	if replication > 3 {
		replication = 3
	}
	return fmt.Sprintf(`<configuration>
  <property><name>dfs.replication</name><value>%d</value></property>
  <property><name>dfs.namenode.name.dir</name><value>file://%s/hadoopdata/namenode</value></property>
  <property><name>dfs.datanode.data.dir</name><value>file://%s/hadoopdata/datanode</value></property>
</configuration>
`, replication, i.baseHome, i.baseHome)
}

func (i *installer) desiredMapredSiteContent() string {
	return `<configuration>
<property>
<name>mapreduce.framework.name</name>
<value>yarn</value>
</property>
</configuration>
`
}

func (i *installer) desiredYarnSiteContent() string {
	return `<configuration>
<property>
<name>yarn.resourcemanager.hostname</name>
<value>namenode</value>
</property>
<property>
<name>yarn.nodemanager.aux-services</name>
<value>mapreduce_shuffle</value>
</property>
<property>
<name>yarn.nodemanager.resource.memory-mb</name>
<value>13312</value>
</property>
<property>
<name>yarn.scheduler.maximum-allocation-mb</name>
<value>13312</value>
</property>
<property>
<name>yarn.scheduler.maximum-allocation-vcores</name>
<value>14</value>
</property>
<property>
<name>yarn.nodemanager.resource.cpu-vcores</name>
<value>14</value>
</property>
</configuration>
`
}

func (i *installer) expectedCoreSiteProperties() map[string]string {
	return map[string]string{
		"fs.defaultFS": "hdfs://namenode:9000",
	}
}

func (i *installer) expectedHdfsSiteProperties() map[string]string {
	replication := i.cfg.TotalDataNodes()
	if replication > 3 {
		replication = 3
	}
	return map[string]string{
		"dfs.replication":       strconv.Itoa(replication),
		"dfs.namenode.name.dir": fmt.Sprintf("file://%s/hadoopdata/namenode", i.baseHome),
		"dfs.datanode.data.dir": fmt.Sprintf("file://%s/hadoopdata/datanode", i.baseHome),
	}
}

func (i *installer) expectedMapredSiteProperties() map[string]string {
	return map[string]string{
		"mapreduce.framework.name": "yarn",
	}
}

func (i *installer) expectedYarnSiteProperties() map[string]string {
	return map[string]string{
		"yarn.resourcemanager.hostname":            "namenode",
		"yarn.nodemanager.aux-services":            "mapreduce_shuffle",
		"yarn.nodemanager.resource.memory-mb":      "13312",
		"yarn.scheduler.maximum-allocation-mb":     "13312",
		"yarn.scheduler.maximum-allocation-vcores": "14",
		"yarn.nodemanager.resource.cpu-vcores":     "14",
	}
}

func (i *installer) expectedWorkers() []string {
	workers := make([]string, 0, i.cfg.TotalDataNodes())
	for idx := 1; idx <= i.cfg.TotalDataNodes(); idx++ {
		workers = append(workers, fmt.Sprintf("datanode%d", idx))
	}
	return workers
}

func (i *installer) desiredLocalHostsBlock() string {
	var block bytes.Buffer
	block.WriteString(hostsBegin + "\n")
	block.WriteString(fmt.Sprintf("%s namenode\n", i.cfg.NameNodePrivateIP))
	for idx, ip := range i.cfg.AllDataNodeIPs() {
		block.WriteString(fmt.Sprintf("%s datanode%d\n", ip, idx+1))
	}
	block.WriteString(hostsEnd + "\n")
	return block.String()
}

func (i *installer) desiredEnvBlock() string {
	return fmt.Sprintf(`%s
export JAVA_HOME=%s
export HADOOP_HOME=%s
export SPARK_HOME=%s
export HADOOP_CONF_DIR=$HADOOP_HOME/etc/hadoop
export YARN_CONF_DIR=$HADOOP_HOME/etc/hadoop
export PATH=$PATH:$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh
%s
`, envBegin, i.java11Home, i.hadoopHome, i.sparkHome, envEnd)
}

func (i *installer) legacyEnvConfigured(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	text := string(data)
	requiredLines := []string{
		fmt.Sprintf("export JAVA_HOME=%s", i.java11Home),
		fmt.Sprintf("export HADOOP_HOME=%s", i.hadoopHome),
		fmt.Sprintf("export SPARK_HOME=%s", i.sparkHome),
		`export HADOOP_CONF_DIR=$HADOOP_HOME/etc/hadoop`,
		`export YARN_CONF_DIR=$HADOOP_HOME/etc/hadoop`,
		`export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"`,
		`export PDSH_RCMD_TYPE=ssh`,
	}
	for _, line := range requiredLines {
		if !strings.Contains(text, line) {
			return false, nil
		}
	}
	if !strings.Contains(text, `export PATH=$PATH:$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin`) &&
		!strings.Contains(text, `export PATH="$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin:$PATH"`) {
		return false, nil
	}
	return true, nil
}

func evaluateManagedBlockFile(path, beginMarker, endMarker, desired string) (configState, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configStateMissing, "file missing", nil
		}
		return "", "", err
	}
	block, found, err := extractManagedBlock(string(data), beginMarker, endMarker)
	if err != nil {
		return "", "", err
	}
	if !found {
		return configStateMissing, "managed block missing", nil
	}
	if normalizeText(block) == normalizeText(desired) {
		return configStateExact, "", nil
	}
	return configStateDrift, "managed block differs from desired cluster shape", nil
}

func extractManagedBlock(text, beginMarker, endMarker string) (string, bool, error) {
	start := strings.Index(text, beginMarker)
	if start == -1 {
		return "", false, nil
	}
	end := strings.Index(text[start:], endMarker)
	if end == -1 {
		return "", false, fmt.Errorf("found %s without matching %s", beginMarker, endMarker)
	}
	end += start + len(endMarker)
	block := text[start:end]
	if !strings.HasSuffix(block, "\n") {
		block += "\n"
	}
	return block, true, nil
}

func upsertManagedBlockText(text, beginMarker, endMarker, desiredBlock string) (string, error) {
	start := strings.Index(text, beginMarker)
	if start != -1 {
		end := strings.Index(text[start:], endMarker)
		if end == -1 {
			return "", fmt.Errorf("found %s without matching %s", beginMarker, endMarker)
		}
		end += start + len(endMarker)
		if end < len(text) && text[end] == '\n' {
			end++
		}
		updated := text[:start] + desiredBlock + text[end:]
		return normalizeManagedFileText(updated), nil
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return normalizeManagedFileText(text + desiredBlock), nil
}

func normalizeManagedFileText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	return text + "\n"
}

func evaluateHadoopXMLFile(path string, expected map[string]string) (configState, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configStateMissing, "file missing", nil
		}
		return "", "", err
	}
	var cfg hadoopXMLConfiguration
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return configStateDrift, "file is not parseable Hadoop XML", nil
	}
	props := make(map[string]string, len(cfg.Properties))
	for _, prop := range cfg.Properties {
		name := strings.TrimSpace(prop.Name)
		if name == "" {
			continue
		}
		props[name] = strings.TrimSpace(prop.Value)
	}
	if len(props) == 0 {
		return configStateEmpty, "no configured properties", nil
	}
	for name, expectedValue := range expected {
		actualValue, ok := props[name]
		if !ok {
			return configStateDrift, fmt.Sprintf("missing %s", name), nil
		}
		if actualValue != expectedValue {
			return configStateDrift, fmt.Sprintf("%s=%q (expected %q)", name, actualValue, expectedValue), nil
		}
	}
	return configStateExact, "", nil
}

func evaluateWorkersFile(path string, expected []string) (configState, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configStateMissing, "file missing", nil
		}
		return "", "", err
	}
	lines := make([]string, 0)
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return configStateEmpty, "no worker hosts listed", nil
	}
	if len(lines) == 1 && lines[0] == "localhost" {
		return configStateMissing, "stock default localhost entry", nil
	}
	if strings.Join(lines, "\n") == strings.Join(expected, "\n") {
		return configStateExact, "", nil
	}
	return configStateDrift, fmt.Sprintf("workers=%q expected=%q", strings.Join(lines, ","), strings.Join(expected, ",")), nil
}

func evaluateJavaEnvFile(path string, accepted []string) (configState, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configStateMissing, "file missing", nil
		}
		return "", "", err
	}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "export JAVA_HOME=") {
			continue
		}
		for _, candidate := range accepted {
			if line == candidate {
				return configStateExact, "", nil
			}
		}
		return configStateDrift, fmt.Sprintf("JAVA_HOME line=%q", line), nil
	}
	return configStateEmpty, "JAVA_HOME export not set", nil
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimSpace(text)
}

func ensureTrailingNewline(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text
}

func driftError(component, details string) error {
	if details == "" {
		details = "existing state differs from the installer-managed desired state"
	}
	return fmt.Errorf("%s drift detected: %s. Refusing to overwrite automatically in install mode; run --preflight and review the existing cluster state first", component, details)
}

func contentContains(path, needle string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), needle)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
