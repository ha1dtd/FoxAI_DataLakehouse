//go:build linux

package installer

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (i *installer) autoProbeHardware() ([]hostSpec, error) {
	specs := make([]hostSpec, 0, i.cfg.TotalDataNodes()+1)
	local, err := i.probeLocalHost()
	if err != nil {
		return nil, err
	}
	specs = append(specs, local)
	for idx, ip := range i.cfg.AllDataNodeIPs() {
		spec, err := i.probeRemoteHost(ip, idx+1)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func (i *installer) manualEnterHardware() ([]hostSpec, error) {
	specs := make([]hostSpec, 0, i.cfg.TotalDataNodes()+1)
	fmt.Println()
	fmt.Println("Enter hardware for recommendation only. This does not change the installed cluster automatically.")

	localCores, err := i.promptInt("NameNode CPU cores")
	if err != nil {
		return nil, err
	}
	localRAM, err := i.promptInt("NameNode RAM (GB)")
	if err != nil {
		return nil, err
	}
	specs = append(specs, hostSpec{
		Label:    "namenode",
		CPUCores: localCores,
		MemoryGB: localRAM,
		Source:   "manual",
	})

	for idx := range i.cfg.AllDataNodeIPs() {
		label := fmt.Sprintf("datanode%d", idx+1)
		cores, err := i.promptInt(fmt.Sprintf("%s CPU cores", label))
		if err != nil {
			return nil, err
		}
		ram, err := i.promptInt(fmt.Sprintf("%s RAM (GB)", label))
		if err != nil {
			return nil, err
		}
		specs = append(specs, hostSpec{
			Label:    label,
			CPUCores: cores,
			MemoryGB: ram,
			Source:   "manual",
		})
	}
	return specs, nil
}

func (i *installer) probeLocalHost() (hostSpec, error) {
	cores, memGB, err := probeHostCommand("")
	if err != nil {
		return hostSpec{}, err
	}
	return hostSpec{
		Label:    "namenode",
		CPUCores: cores,
		MemoryGB: memGB,
		Source:   "auto",
	}, nil
}

func (i *installer) probeRemoteHost(ip string, nodeNumber int) (hostSpec, error) {
	target := fmt.Sprintf("%s@%s", i.cfg.DataNodeUser, ip)
	cores, memGB, err := probeHostCommand(target)
	if err != nil {
		return hostSpec{}, fmt.Errorf("auto probe failed for %s: %w", ip, err)
	}
	return hostSpec{
		Label:    fmt.Sprintf("datanode%d", nodeNumber),
		CPUCores: cores,
		MemoryGB: memGB,
		Source:   "auto",
	}, nil
}

func deriveRecommendations(specs []hostSpec) clusterRecommendation {
	dataNodes := make([]hostSpec, 0)
	for _, spec := range specs {
		if strings.HasPrefix(spec.Label, "datanode") {
			dataNodes = append(dataNodes, spec)
		}
	}

	minCores := 0
	minRAM := 0
	for idx, spec := range dataNodes {
		if idx == 0 || spec.CPUCores < minCores {
			minCores = spec.CPUCores
		}
		if idx == 0 || spec.MemoryGB < minRAM {
			minRAM = spec.MemoryGB
		}
	}

	replication := len(dataNodes)
	if replication > 3 {
		replication = 3
	}
	yarnVcores := recommendedYARNVcores(minCores)
	yarnMemoryMB := recommendedYARNMemoryMB(minRAM)
	executorsPerNode := 1
	if yarnVcores >= 8 && yarnMemoryMB >= 12288 {
		executorsPerNode = 2
	}
	executorCores := maxInt(1, minInt(4, yarnVcores/executorsPerNode))
	executorMemoryMB := maxInt(1024, int(float64(yarnMemoryMB/executorsPerNode)*0.8))
	driverMemoryMB := minInt(4096, maxInt(2048, executorMemoryMB/2))
	executorInstances := maxInt(1, executorsPerNode*len(dataNodes))
	shufflePartitions := maxInt(32, executorInstances*executorCores*2)

	return clusterRecommendation{
		NodeCount:              len(dataNodes),
		MinNodeCores:           minCores,
		MinNodeMemoryGB:        minRAM,
		YARNMemoryMB:           yarnMemoryMB,
		YARNVcores:             yarnVcores,
		HDFSReplication:        replication,
		SparkExecutorInstances: executorInstances,
		SparkExecutorCores:     executorCores,
		SparkExecutorMemoryMB:  executorMemoryMB,
		SparkDriverMemoryMB:    driverMemoryMB,
		SparkShufflePartitions: shufflePartitions,
	}
}

func recommendedYARNVcores(totalCores int) int {
	if totalCores <= 2 {
		return maxInt(1, totalCores)
	}
	if totalCores <= 8 {
		return totalCores - 1
	}
	return totalCores - 2
}

func recommendedYARNMemoryMB(totalGB int) int {
	if totalGB <= 0 {
		return 0
	}
	reserveGB := 2
	switch {
	case totalGB <= 8:
		reserveGB = 2
	case totalGB <= 16:
		reserveGB = 3
	case totalGB <= 32:
		reserveGB = 4
	default:
		reserveGB = maxInt(4, totalGB/10)
	}
	usableGB := maxInt(1, totalGB-reserveGB)
	return usableGB * 1024
}

func (i *installer) printRecommendationSummary(specs []hostSpec, rec clusterRecommendation) {
	fmt.Println()
	fmt.Println("=== HARDWARE PROBE SUMMARY ===")
	for _, spec := range specs {
		fmt.Printf("  - %s: %d cores, %d GB RAM (%s)\n", spec.Label, spec.CPUCores, spec.MemoryGB, spec.Source)
	}
	fmt.Println()
	fmt.Println("=== APPLIED PLATFORM CONFIG RECOMMENDATION ===")
	fmt.Printf("  - DataNodes: %d\n", rec.NodeCount)
	fmt.Printf("  - HDFS replication: %d\n", rec.HDFSReplication)
	fmt.Printf("  - Recommended YARN memory per DataNode: %d MB\n", rec.YARNMemoryMB)
	fmt.Printf("  - Recommended YARN vcores per DataNode: %d\n", rec.YARNVcores)
	fmt.Println()
	fmt.Println("=== RECOMMENDED SPARK DEFAULTS (DO NOT AUTO-APPLY TO DAGS/JOBS) ===")
	fmt.Printf("  - --executor-memory %dm\n", rec.SparkExecutorMemoryMB)
	fmt.Printf("  - --executor-cores %d\n", rec.SparkExecutorCores)
	fmt.Printf("  - --conf spark.executor.instances=%d\n", rec.SparkExecutorInstances)
	fmt.Printf("  - --driver-memory %dm\n", rec.SparkDriverMemoryMB)
	fmt.Printf("  - --conf spark.sql.shuffle.partitions=%d\n", rec.SparkShufflePartitions)
	fmt.Println()
	fmt.Println("Suggested Spark snippet:")
	fmt.Printf("  --driver-memory %dm \\\n", rec.SparkDriverMemoryMB)
	fmt.Printf("  --executor-memory %dm \\\n", rec.SparkExecutorMemoryMB)
	fmt.Printf("  --executor-cores %d \\\n", rec.SparkExecutorCores)
	fmt.Printf("  --conf spark.executor.instances=%d \\\n", rec.SparkExecutorInstances)
	fmt.Printf("  --conf spark.sql.shuffle.partitions=%d\n", rec.SparkShufflePartitions)
	fmt.Println()
}

func probeHostCommand(sshTarget string) (int, int, error) {
	script := `set -euo pipefail
CORES="$(nproc)"
MEM_KB="$(awk '/MemTotal/ {print $2}' /proc/meminfo)"
MEM_GB=$((MEM_KB / 1024 / 1024))
printf '%s %s\n' "$CORES" "$MEM_GB"
`
	var stdout bytes.Buffer
	var err error
	if sshTarget == "" {
		err = runCommand("", strings.NewReader(script), &stdout, os.Stderr, "bash", "-s")
	} else {
		err = runCommand("", strings.NewReader(script), &stdout, os.Stderr, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=5", sshTarget, "bash", "-s")
	}
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected probe output: %q", stdout.String())
	}
	cores, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, err
	}
	memGB, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, err
	}
	return cores, memGB, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
