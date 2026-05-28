//go:build linux

package installer

import (
	"bufio"
	"encoding/xml"
)

type installerConfig struct {
	NameNodePrivateIP string
	DataNodeUser      string
	MinIOEndpoint     string
	MinIOAccessKey    string
	MinIOSecretKey    string
	UseKakaoMirror    bool
	ExistingNodeIPs   []string
	NewNodeIPs        []string
}

func (c installerConfig) AllDataNodeIPs() []string {
	ips := make([]string, 0, len(c.ExistingNodeIPs)+len(c.NewNodeIPs))
	ips = append(ips, c.ExistingNodeIPs...)
	ips = append(ips, c.NewNodeIPs...)
	return ips
}

func (c installerConfig) TotalDataNodes() int {
	return len(c.ExistingNodeIPs) + len(c.NewNodeIPs)
}

type installer struct {
	cfg                          installerConfig
	mode                         installerMode
	reader                       *bufio.Reader
	currentUser                  string
	baseHome                     string
	hadoopHome                   string
	sparkHome                    string
	java11Home                   string
	installTargetIPs             []string
	summary                      []summaryEntry
	allowInstallManagedOverwrite bool
}

type installerMode string

type summaryStatus string

type summaryEntry struct {
	Target    string        `json:"target"`
	Component string        `json:"component"`
	Status    summaryStatus `json:"status"`
	Details   string        `json:"details"`
}

type runManifest struct {
	Timestamp string          `json:"timestamp"`
	Mode      installerMode   `json:"mode"`
	Success   bool            `json:"success"`
	Error     string          `json:"error,omitempty"`
	Inputs    installerConfig `json:"inputs"`
	Summary   []summaryEntry  `json:"summary"`
}

type repairSelection struct {
	FixLocalNameNode bool
	TargetIPs        []string
}

type hostSpec struct {
	Label    string
	CPUCores int
	MemoryGB int
	Source   string
}

type bootstrapDependency struct {
	Command string
	Package string
}

type dataNodeReuseState string

type dataNodeReuseProbe struct {
	IP        string
	State     dataNodeReuseState
	ClusterID string
	Details   string
}

type clusterRecommendation struct {
	NodeCount              int
	MinNodeCores           int
	MinNodeMemoryGB        int
	YARNMemoryMB           int
	YARNVcores             int
	HDFSReplication        int
	SparkExecutorInstances int
	SparkExecutorCores     int
	SparkExecutorMemoryMB  int
	SparkDriverMemoryMB    int
	SparkShufflePartitions int
}

type configState string

type hadoopXMLConfiguration struct {
	XMLName    xml.Name            `xml:"configuration"`
	Properties []hadoopXMLProperty `xml:"property"`
}

type hadoopXMLProperty struct {
	Name  string `xml:"name"`
	Value string `xml:"value"`
}
