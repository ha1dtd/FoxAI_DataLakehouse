//go:build linux

package installer

const (
	pinnedHadoopVersion = "3.3.6"
	pinnedSparkArtifact = "spark-3.5.8-bin-hadoop3"
	pinnedJava11Package = "temurin-11-jdk"
	pinnedJava11Home    = "/usr/lib/jvm/temurin-11-jdk-amd64"

	defaultMinIOAccessKey  = "admin"
	defaultMinIOSecretKey  = "12345678"
	defaultUseKakaoMirror  = "yes"
	hostsBegin             = "# >>> FOXAI CLUSTER HOSTS >>>"
	hostsEnd               = "# <<< FOXAI CLUSTER HOSTS <<<"
	envBegin               = "# >>> FOXAI MANAGED ENV >>>"
	envEnd                 = "# <<< FOXAI MANAGED ENV <<<"
	sudoersTemplate        = "%s ALL=(ALL) NOPASSWD:ALL"
	remoteDNProbeBlockNote = "Full cluster hosts block is managed directly by the FoxAI installer on every node."
)

var (
	basePackagesNameNode = []string{
		"wget", "gpg", "ssh", "pdsh", "python3-venv", "python3-pip", "curl", "tar",
		"rsync", "unzip", "build-essential", "libffi-dev", "python3-dev", "libsasl2-dev",
		"libldap2-dev", "default-libmysqlclient-dev",
	}
	basePackagesDataNode = []string{
		"wget", "gpg", "ssh", "pdsh", "python3-venv", "python3-pip", "curl", "tar",
		"rsync", "unzip", "build-essential", "libffi-dev", "python3-dev", "libsasl2-dev",
		"libldap2-dev",
	}
)

const (
	modeInstall       installerMode = "install"
	modeDryRun        installerMode = "dry-run"
	modePreflight     installerMode = "preflight"
	modeRepair        installerMode = "repair"
	modeReconcile     installerMode = "reconcile"
	modeRecommendOnly installerMode = "recommend-only"
)

const (
	statusOK      summaryStatus = "OK"
	statusSkip    summaryStatus = "SKIP"
	statusWarn    summaryStatus = "WARN"
	statusDrift   summaryStatus = "DRIFT"
	statusFixed   summaryStatus = "FIXED"
	statusBlocked summaryStatus = "BLOCKED"
)

const (
	dataNodeReuseFresh       dataNodeReuseState = "fresh"
	dataNodeReuseCompatible  dataNodeReuseState = "compatible"
	dataNodeReuseConflicting dataNodeReuseState = "conflicting"
	dataNodeReusePartial     dataNodeReuseState = "partial"
	dataNodeReuseUnreadable  dataNodeReuseState = "unreadable"
)

const (
	configStateExact   configState = "exact"
	configStateEmpty   configState = "empty"
	configStateMissing configState = "missing"
	configStateDrift   configState = "drift"
)
