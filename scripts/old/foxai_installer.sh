#!/bin/bash
set -euo pipefail

# Single-file FoxAI installer.
# Source-of-truth logic comes from:
# - scripts/setup_namenode_v5.sh
# - scripts/setup_datanode.sh
#
# This installer preserves the current tested flow:
# 1. Run Namenode setup locally first
# 2. Then run the Datanode setup flow remotely on each datanode
#
# Versions are intentionally pinned to match the source scripts exactly.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PINNED_HADOOP_VERSION="3.3.6"
PINNED_SPARK_ARTIFACT="spark-3.5.8-bin-hadoop3"
PINNED_JAVA11_PACKAGE="temurin-11-jdk"
PINNED_JAVA11_HOME="/usr/lib/jvm/temurin-11-jdk-amd64"
DEFAULT_MINIO_ENDPOINT="192.168.100.66:9001"
DEFAULT_MINIO_ACCESS_KEY="admin"
DEFAULT_MINIO_SECRET_KEY="12345678"
DEFAULT_USE_KAKAO_MIRROR="yes"

NN_PRIVATE_IP=""
DN_USER=""
MINIO_ENDPOINT=""
MINIO_ACCESS_KEY=""
MINIO_SECRET_KEY=""
USE_KAKAO_MIRROR=""
NUM_EXISTING_DN=0
NUM_NEW_DN=0
TOTAL_DN=0
declare -a EXISTING_DN_IPS=()
declare -a NEW_DN_IPS=()
declare -a ALL_DNS=()

NN_USER="$(whoami)"
BASE_HOME="/home/$NN_USER"
HADOOP_HOME="$BASE_HOME/hadoop"
SPARK_HOME="/opt/spark"
JAVA11_HOME="$PINNED_JAVA11_HOME"
HOSTS_BEGIN="# >>> FOXAI CLUSTER HOSTS >>>"
HOSTS_END="# <<< FOXAI CLUSTER HOSTS <<<"


section() {
    echo "===================="
    echo "STEP: $1"
    echo "===================="
}


require_command() {
    local cmd="$1"
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "ERROR: Required command not found: $cmd"
        exit 1
    fi
}


prompt_required() {
    local label="$1"
    local value=""
    while true; do
        read -r -p "$label: " value
        value="${value//[$'\r\n']}"
        if [ -n "$value" ]; then
            printf '%s\n' "$value"
            return
        fi
        echo "  Value is required."
    done
}


prompt_optional() {
    local label="$1"
    local default_value="$2"
    local value=""
    read -r -p "$label [$default_value]: " value
    value="${value//[$'\r\n']}"
    if [ -z "$value" ]; then
        printf '%s\n' "$default_value"
    else
        printf '%s\n' "$value"
    fi
}


prompt_int() {
    local label="$1"
    local value=""
    while true; do
        read -r -p "$label: " value
        value="${value//[$'\r\n']}"
        if [[ "$value" =~ ^[0-9]+$ ]]; then
            printf '%s\n' "$value"
            return
        fi
        echo "  Enter a whole number."
    done
}


prompt_yes_no_default() {
    local label="$1"
    local default_value="$2"
    local value=""
    local normalized_default=""
    if [ "$default_value" = "yes" ]; then
        normalized_default="Y/n"
    else
        normalized_default="y/N"
    fi

    while true; do
        read -r -p "$label [$normalized_default]: " value
        value="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
        if [ -z "$value" ]; then
            printf '%s\n' "$default_value"
            return
        fi
        case "$value" in
            y|yes)
                printf 'yes\n'
                return
                ;;
            n|no)
                printf 'no\n'
                return
                ;;
        esac
        echo "  Enter yes or no."
    done
}


validate_ip() {
    local ip="$1"
    python3 - "$ip" <<'PYEOF'
import ipaddress
import sys

try:
    ipaddress.ip_address(sys.argv[1])
except ValueError:
    raise SystemExit(1)
PYEOF
}


prompt_ip() {
    local label="$1"
    local value=""
    while true; do
        read -r -p "$label: " value
        value="${value//[$'\r\n']}"
        if validate_ip "$value"; then
            printf '%s\n' "$value"
            return
        fi
        echo "  Invalid IP address."
    done
}


collect_inputs() {
    echo "=== FOXAI SINGLE-FILE INSTALLER ==="
    echo "This installer runs the current tested flow in one entrypoint:"
    echo "1. NameNode setup locally"
    echo "2. DataNode setup remotely on each node"
    echo ""
    echo "Pinned versions:"
    echo "  - Hadoop: $PINNED_HADOOP_VERSION"
    echo "  - Spark:  $PINNED_SPARK_ARTIFACT"
    echo "  - Java 11 package: $PINNED_JAVA11_PACKAGE"
    echo ""

    NN_PRIVATE_IP="$(prompt_ip "Namenode private IP")"
    NUM_EXISTING_DN="$(prompt_int "Number of EXISTING datanodes")"
    NUM_NEW_DN="$(prompt_int "Number of NEW datanodes")"

    echo ""
    if [ "$NUM_EXISTING_DN" -gt 0 ]; then
        echo "=== EXISTING DATANODE IPs ==="
        for i in $(seq 1 "$NUM_EXISTING_DN"); do
            EXISTING_DN_IPS+=("$(prompt_ip "  Existing DN$i IP")")
        done
    fi

    if [ "$NUM_NEW_DN" -gt 0 ]; then
        echo "=== NEW DATANODE IPs ==="
        for i in $(seq 1 "$NUM_NEW_DN"); do
            NEW_DN_IPS+=("$(prompt_ip "  New DN$i IP")")
        done
    fi

    DN_USER="$(prompt_required "Datanode username")"
    MINIO_ENDPOINT="$(prompt_optional "MinIO endpoint" "$DEFAULT_MINIO_ENDPOINT")"
    MINIO_ACCESS_KEY="$(prompt_optional "MinIO access key" "$DEFAULT_MINIO_ACCESS_KEY")"
    MINIO_SECRET_KEY="$(prompt_optional "MinIO secret key" "$DEFAULT_MINIO_SECRET_KEY")"
    USE_KAKAO_MIRROR="$(prompt_yes_no_default "Apply Kakao apt mirror override from current source scripts" "$DEFAULT_USE_KAKAO_MIRROR")"

    ALL_DNS=("${EXISTING_DN_IPS[@]}" "${NEW_DN_IPS[@]}")
    TOTAL_DN=$((NUM_EXISTING_DN + NUM_NEW_DN))

    echo ""
    echo "Collected inputs:"
    echo "  - Namenode IP: $NN_PRIVATE_IP"
    echo "  - Existing datanodes: $NUM_EXISTING_DN"
    echo "  - New datanodes: $NUM_NEW_DN"
    echo "  - Total datanodes: $TOTAL_DN"
    echo "  - Datanode username: $DN_USER"
    echo "  - MinIO endpoint: $MINIO_ENDPOINT"
    echo "  - MinIO access key: $MINIO_ACCESS_KEY"
    echo "  - MinIO secret key: [hidden]"
    echo "  - Kakao mirror override: $USE_KAKAO_MIRROR"
    echo ""
}


apply_apt_mirror_if_enabled() {
    if [ "$USE_KAKAO_MIRROR" != "yes" ]; then
        echo "  - Skipped premise-specific Kakao mirror override"
        return
    fi

    if grep -qE "mirror\.kakao\.com/ubuntu" /etc/apt/sources.list; then
        echo "  - Already configured"
    else
        sudo sed -i 's|http://archive.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list
        sudo sed -i 's|http://security.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list
        echo "  - Configured"
    fi
}


ensure_python_symlink() {
    if [ ! -x /usr/bin/python ]; then
        sudo ln -sf /usr/bin/python3 /usr/bin/python
        echo "  - Linked python -> python3"
    fi
}


ensure_adoptium_repo() {
    if [ ! -f /usr/share/keyrings/adoptium.gpg ]; then
        wget -4 -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | sudo gpg --dearmor -o /usr/share/keyrings/adoptium.gpg
    fi
    . /etc/os-release
    ADOPT_CODENAME="${VERSION_CODENAME:-bookworm}"
    if [ ! -f /etc/apt/sources.list.d/adoptium.list ]; then
        echo "deb [signed-by=/usr/share/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb ${ADOPT_CODENAME} main" | sudo tee /etc/apt/sources.list.d/adoptium.list >/dev/null
    fi
}


run_namenode_setup() {
    local p=""
    local MISSING_PKGS=""
    local HOSTS_BLOCK_FILE="/tmp/foxai_hosts_block.txt"
    local REP_VALUE=0
    local WORKERS_EXISTS=true
    local HADOOP_ENV="$HADOOP_HOME/etc/hadoop/hadoop-env.sh"
    local FLEXIBLE_JAVA="export JAVA_HOME=\${JAVA_HOME:-$JAVA11_HOME}"

    section "SSH KEY"
    if [ -f ~/.ssh/id_rsa ]; then
        echo "  - SSH key exists"
    else
        ssh-keygen -t rsa -N "" -f ~/.ssh/id_rsa
        echo "  - SSH key generated"
    fi
    mkdir -p ~/.ssh
    if [ ! -f ~/.ssh/authorized_keys ]; then
        touch ~/.ssh/authorized_keys
    fi
    chmod 600 ~/.ssh/authorized_keys
    if ! grep -qF "$(cat ~/.ssh/id_rsa.pub)" ~/.ssh/authorized_keys 2>/dev/null; then
        cat ~/.ssh/id_rsa.pub >> ~/.ssh/authorized_keys
        echo "  - Key added"
    fi

    section "SSH COPY TO ALL DNs"
    for DN_IP in "${ALL_DNS[@]}"; do
        ssh-copy-id -f "$DN_USER@$DN_IP" 2>/dev/null || true
    done

    section "NOPASSWD (ALL DNs)"
    for DN_IP in "${ALL_DNS[@]}"; do
        echo "  - Checking $DN_IP"
        if ssh -o BatchMode=yes -o ConnectTimeout=5 "$DN_USER@$DN_IP" "sudo -n true" 2>/dev/null; then
            echo "    * Already NOPASSWD"
        else
            echo "    * Configuring NOPASSWD (enter password if prompted)"
            ssh -tt "$DN_USER@$DN_IP" "echo '$DN_USER ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/$DN_USER >/dev/null"
        fi
    done

    section "APT MIRROR"
    apply_apt_mirror_if_enabled

    section "BASE PACKAGES"
    for p in wget gpg ssh pdsh python3-venv python3-pip curl tar rsync unzip build-essential libffi-dev python3-dev libsasl2-dev libldap2-dev default-libmysqlclient-dev; do
        if ! dpkg -s "$p" >/dev/null 2>&1; then
            MISSING_PKGS="$MISSING_PKGS $p"
        fi
    done
    if [ -n "$MISSING_PKGS" ]; then
        sudo apt update
        sudo apt install -y $MISSING_PKGS
        echo "  - Installed:$MISSING_PKGS"
    else
        echo "  - Already installed"
    fi
    ensure_python_symlink

    section "JAVA 11"
    if [ -d "$JAVA11_HOME" ]; then
        echo "  - Already installed"
    else
        ensure_adoptium_repo
        sudo apt update
        sudo apt install -y "$PINNED_JAVA11_PACKAGE"
        echo "  - Installed"
    fi
    [ -d "$JAVA11_HOME" ] || { echo "JAVA_HOME invalid"; exit 1; }

    section "HADOOP"
    if [ -x "$HADOOP_HOME/bin/hdfs" ]; then
        echo "  - Already installed"
    else
        cd "$BASE_HOME"
        wget -4 "https://dlcdn.apache.org/hadoop/common/hadoop-$PINNED_HADOOP_VERSION/hadoop-$PINNED_HADOOP_VERSION.tar.gz"
        tar -xzf "hadoop-$PINNED_HADOOP_VERSION.tar.gz"
        mv "hadoop-$PINNED_HADOOP_VERSION" "$HADOOP_HOME"
        echo "  - Installed"
    fi

    section "SPARK"
    if [ -x "$SPARK_HOME/bin/spark-submit" ]; then
        echo "  - Already installed"
    else
        cd "$BASE_HOME"
        wget -4 "https://dlcdn.apache.org/spark/spark-3.5.8/${PINNED_SPARK_ARTIFACT}.tgz"
        tar -xzf "${PINNED_SPARK_ARTIFACT}.tgz"
        sudo mv "$PINNED_SPARK_ARTIFACT" "$SPARK_HOME"
        sudo chown -R "$NN_USER:$NN_USER" "$SPARK_HOME"
        echo "  - Installed"
    fi

    section "BASHRC"
    if grep -qE "HADOOP_HOME" ~/.bashrc; then
        echo "  - Already configured"
    else
        cat <<EOT >> ~/.bashrc
export JAVA_HOME=$JAVA11_HOME
export HADOOP_HOME=$HADOOP_HOME
export SPARK_HOME=$SPARK_HOME
export HADOOP_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export YARN_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export PATH=\$PATH:\$JAVA_HOME/bin:\$HADOOP_HOME/bin:\$HADOOP_HOME/sbin:\$SPARK_HOME/bin:\$SPARK_HOME/sbin
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh
EOT
        echo "  - Configured"
    fi
    export JAVA_HOME="$JAVA11_HOME"
    export HADOOP_HOME="$HADOOP_HOME"
    export SPARK_HOME="$SPARK_HOME"
    export HADOOP_CONF_DIR="$HADOOP_HOME/etc/hadoop"
    export YARN_CONF_DIR="$HADOOP_HOME/etc/hadoop"

    section "/etc/hosts"
    {
        echo "$HOSTS_BEGIN"
        echo "$NN_PRIVATE_IP namenode"
        idx=1
        for DN_IP in "${ALL_DNS[@]}"; do
            echo "$DN_IP datanode$idx"
            idx=$((idx+1))
        done
        echo "$HOSTS_END"
    } > "$HOSTS_BLOCK_FILE"

    sudo python3 - "$HOSTS_BLOCK_FILE" <<'PYEOF'
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
PYEOF
    echo "  - Updated"

    section "HADOOP DATA DIR"
    if [ -d "$BASE_HOME/hadoopdata/namenode" ]; then
        chmod -R 700 "$BASE_HOME/hadoopdata"
        echo "  - Already exists"
    else
        mkdir -p "$BASE_HOME/hadoopdata/namenode"
        chmod -R 700 "$BASE_HOME/hadoopdata"
        echo "  - Created"
    fi

    section "CORE-SITE.XML"
    if [ -f "$HADOOP_HOME/etc/hadoop/core-site.xml" ] && grep -qE "hdfs://namenode:9000" "$HADOOP_HOME/etc/hadoop/core-site.xml"; then
        echo "  - Already configured"
    else
        cat <<EOT > "$HADOOP_HOME/etc/hadoop/core-site.xml"
<configuration>
  <property>
    <name>fs.defaultFS</name>
    <value>hdfs://namenode:9000</value>
  </property>
</configuration>
EOT
        echo "  - Configured"
    fi

    section "HDFS-SITE.XML"
    if [ "$TOTAL_DN" -lt 3 ]; then
        REP_VALUE="$TOTAL_DN"
    else
        REP_VALUE=3
    fi
    if [ -f "$HADOOP_HOME/etc/hadoop/hdfs-site.xml" ] && grep -qE "dfs\.datanode\.data\.dir" "$HADOOP_HOME/etc/hadoop/hdfs-site.xml"; then
        echo "  - Already configured"
    else
        cat <<EOT > "$HADOOP_HOME/etc/hadoop/hdfs-site.xml"
<configuration>
  <property><name>dfs.replication</name><value>$REP_VALUE</value></property>
  <property><name>dfs.namenode.name.dir</name><value>file://$BASE_HOME/hadoopdata/namenode</value></property>
  <property><name>dfs.datanode.data.dir</name><value>file:///$DN_USER/hadoopdata/datanode</value></property>
</configuration>
EOT
        echo "  - Configured (replication=$REP_VALUE)"
    fi

    section "WORKERS FILE"
    for i in $(seq 1 "$TOTAL_DN"); do
        if ! grep -qEx "datanode$i" "$HADOOP_HOME/etc/hadoop/workers" 2>/dev/null; then
            WORKERS_EXISTS=false
            break
        fi
    done
    if $WORKERS_EXISTS; then
        echo "  - Already configured"
    else
        > "$HADOOP_HOME/etc/hadoop/workers"
        for i in $(seq 1 "$TOTAL_DN"); do
            echo "datanode$i" >> "$HADOOP_HOME/etc/hadoop/workers"
        done
        echo "  - Configured (1-$TOTAL_DN)"
    fi

    section "MAPRED-SITE.XML"
    if [ -f "$HADOOP_HOME/etc/hadoop/mapred-site.xml" ] && grep -qE "mapreduce\.framework\.name" "$HADOOP_HOME/etc/hadoop/mapred-site.xml"; then
        echo "  - Already configured"
    else
        cat <<'EOT' > "$HADOOP_HOME/etc/hadoop/mapred-site.xml"
<configuration>
<property>
<name>mapreduce.framework.name</name>
<value>yarn</value>
</property>
</configuration>
EOT
        echo "  - Configured"
    fi

    section "YARN-SITE.XML"
    if [ -f "$HADOOP_HOME/etc/hadoop/yarn-site.xml" ] && grep -qE "yarn\.nodemanager\.resource\.memory-mb" "$HADOOP_HOME/etc/hadoop/yarn-site.xml"; then
        echo "  - Already configured"
    else
        cat <<'EOT' > "$HADOOP_HOME/etc/hadoop/yarn-site.xml"
<configuration>
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
EOT
        echo "  - Configured (memory=13312, vcores=14)"
    fi

    section "HADOOP-ENV.SH"
    if grep -qE "^export JAVA_HOME=\$\\{JAVA_HOME" "$HADOOP_ENV"; then
        echo "  - Already configured"
    elif grep -qE '^# export JAVA_HOME=' "$HADOOP_ENV"; then
        sed -i "s|^# export JAVA_HOME=.*|$FLEXIBLE_JAVA|" "$HADOOP_ENV"
        echo "  - Updated"
    elif grep -qE '^export JAVA_HOME=' "$HADOOP_ENV"; then
        sed -i "s|^export JAVA_HOME=.*|$FLEXIBLE_JAVA|" "$HADOOP_ENV"
        echo "  - Updated"
    else
        echo "$FLEXIBLE_JAVA" >> "$HADOOP_ENV"
        echo "  - Added"
    fi

    section "FORMAT NAMENODE"
    if [ -d "$BASE_HOME/hadoopdata/namenode/current" ]; then
        echo "  - Already formatted"
    else
        "$HADOOP_HOME/bin/hdfs" namenode -format -force -nonInteractive
        echo "  - Formatted"
    fi

    section "SYNC TO EXISTING DATANODES (CONFIGS ONLY)"
    for DN_IP in "${EXISTING_DN_IPS[@]}"; do
        echo "  - Syncing to $DN_IP (existing)"
        rsync -az --delete "$HADOOP_HOME/" "$DN_USER@$DN_IP:$HADOOP_HOME/"
        rsync -az --delete "$SPARK_HOME/" "$DN_USER@$DN_IP:$SPARK_HOME/" 2>/dev/null || true
    done

    section "SYNC TO NEW DATANODES (FULL)"
    for DN_IP in "${NEW_DN_IPS[@]}"; do
        echo "  - Syncing to $DN_IP (new)"
        rsync -az --delete "$HADOOP_HOME/" "$DN_USER@$DN_IP:$HADOOP_HOME/"
        rsync -az --delete "$SPARK_HOME/" "$DN_USER@$DN_IP:$SPARK_HOME/" 2>/dev/null || true
    done
}


run_remote_datanode_setup() {
    local DN_IP="$1"
    local MY_DN_NUM="$2"
    local MIRROR_FLAG="0"

    if [ "$USE_KAKAO_MIRROR" = "yes" ]; then
        MIRROR_FLAG="1"
    fi

    section "DATANODE SETUP ($DN_IP)"
    ssh -tt "$DN_USER@$DN_IP" "bash -s" -- \
        "$NN_PRIVATE_IP" \
        "$TOTAL_DN" \
        "$MY_DN_NUM" \
        "$DN_USER" \
        "$MIRROR_FLAG" \
        "$JAVA11_HOME" \
        "$SPARK_HOME" \
        "$HADOOP_HOME" \
        <<'REMOTE_EOF'
set -euo pipefail

NN_PRIVATE_IP="$1"
TOTAL_DN="$2"
MY_DN_NUM="$3"
DN_USER="$4"
USE_KAKAO_MIRROR="$5"
JAVA_HOME="$6"
SPARK_HOME="$7"
HADOOP_HOME="$8"
BASE_HOME="/home/$DN_USER"

section() {
    echo "===================="
    echo "REMOTE STEP: $1"
    echo "===================="
}

ensure_adoptium_repo() {
    if [ ! -f /usr/share/keyrings/adoptium.gpg ]; then
        wget -4 -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | sudo gpg --dearmor -o /usr/share/keyrings/adoptium.gpg
    fi
    . /etc/os-release
    ADOPT_CODENAME="${VERSION_CODENAME:-bookworm}"
    if [ ! -f /etc/apt/sources.list.d/adoptium.list ]; then
        echo "deb [signed-by=/usr/share/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb ${ADOPT_CODENAME} main" | sudo tee /etc/apt/sources.list.d/adoptium.list >/dev/null
    fi
}

section "CHECK USER"
if [ "$(whoami)" != "$DN_USER" ]; then
    echo "ERROR: Run as user '$DN_USER' (now: $(whoami))."
    exit 1
fi

section "CHECK HADOOP FROM NAMENODE"
if [ ! -x "$HADOOP_HOME/bin/hdfs" ]; then
    echo "ERROR: $HADOOP_HOME missing."
    echo "Run the unified installer from Namenode so it syncs Hadoop here first."
    exit 1
fi
echo "  - Hadoop present"

section "CHECK SPARK (/opt/spark)"
if [ -x "$SPARK_HOME/bin/spark-submit" ]; then
    echo "  - Spark present"
else
    echo "  WARNING: Spark not found at $SPARK_HOME"
fi

section "APT MIRROR"
if [ "$USE_KAKAO_MIRROR" = "1" ]; then
    if grep -qE "mirror\.kakao\.com/ubuntu" /etc/apt/sources.list; then
        echo "  - Already configured"
    else
        sudo sed -i 's|http://archive.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list
        sudo sed -i 's|http://security.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list
        echo "  - Configured"
    fi
else
    echo "  - Skipped premise-specific Kakao mirror override"
fi

section "BASE PACKAGES"
MISSING=""
for p in wget gpg ssh pdsh python3-venv python3-pip curl tar rsync unzip build-essential libffi-dev python3-dev libsasl2-dev libldap2-dev; do
    if ! dpkg -s "$p" >/dev/null 2>&1; then
        MISSING="$MISSING $p"
    fi
done
if [ -n "$MISSING" ]; then
    sudo apt update
    sudo apt install -y $MISSING
    echo "  - Installed:$MISSING"
else
    echo "  - Already installed"
fi

if [ ! -L /usr/bin/python ]; then
    sudo ln -sf /usr/bin/python3 /usr/bin/python
fi

section "JAVA 11"
if [ -d "$JAVA_HOME" ]; then
    echo "  - Already installed"
else
    ensure_adoptium_repo
    sudo apt update
    sudo apt install -y temurin-11-jdk
    echo "  - Installed"
fi

section "/etc/hosts (MINIMAL LOCAL BLOCK)"
echo "  Note: Full block stays aligned to current tested flow"
MY_IP="$(hostname -I | awk '{print $1}')"
sudo python3 - "$NN_PRIVATE_IP" "$MY_IP" "$MY_DN_NUM" <<'PYEOF'
import sys

nn_ip, my_ip, my_num = sys.argv[1], sys.argv[2], sys.argv[3]
hosts = open("/etc/hosts").read()
begin = "# >>> FOXAI CLUSTER HOSTS >>>"
end = "# <<< FOXAI CLUSTER HOSTS <<<"
start = hosts.find(begin)
if start != -1:
    end_idx = hosts.find(end, start)
    if end_idx != -1:
        hosts = hosts[:start] + hosts[end_idx + len(end):]
minimal = f"""{begin}
{nn_ip} namenode
datanode{my_num} {my_ip}
{end}
"""
hosts = hosts.rstrip() + "\n" + minimal
open("/etc/hosts", "w").write(hosts)
PYEOF
echo "  - Written"

section "HADOOP JAVA_HOME"
HADOOP_ENV="$HADOOP_HOME/etc/hadoop/hadoop-env.sh"
if grep -qE "^export JAVA_HOME=\$\\{JAVA_HOME" "$HADOOP_ENV" 2>/dev/null; then
    echo "  - Already configured (flexible)"
elif grep -qE "^export JAVA_HOME=" "$HADOOP_ENV" 2>/dev/null; then
    sed -i "s|^export JAVA_HOME=.*|export JAVA_HOME=$JAVA_HOME|" "$HADOOP_ENV"
    echo "  - Updated"
else
    echo "export JAVA_HOME=$JAVA_HOME" >> "$HADOOP_ENV"
    echo "  - Added"
fi

section "SHELL ENVIRONMENT"
if grep -qE "HADOOP_HOME" ~/.bashrc 2>/dev/null; then
    echo "  - Already configured"
else
    cat <<EOT >> ~/.bashrc
export JAVA_HOME=$JAVA_HOME
export HADOOP_HOME=$HADOOP_HOME
export SPARK_HOME=$SPARK_HOME
export HADOOP_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export YARN_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export PATH=\$PATH:\$JAVA_HOME/bin:\$HADOOP_HOME/bin:\$HADOOP_HOME/sbin:\$SPARK_HOME/bin:\$SPARK_HOME/sbin
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh
EOT
    echo "  - Configured"
fi

export JAVA_HOME="$JAVA_HOME"
export HADOOP_HOME="$HADOOP_HOME"
export SPARK_HOME="$SPARK_HOME"
export HADOOP_CONF_DIR="$HADOOP_HOME/etc/hadoop"
export YARN_CONF_DIR="$HADOOP_HOME/etc/hadoop"
export PATH="$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin:$PATH"

section "DATANODE DIRECTORY"
if [ -d "$BASE_HOME/hadoopdata/datanode" ]; then
    chmod -R 700 "$BASE_HOME/hadoopdata"
    echo "  - Already exists"
else
    mkdir -p "$BASE_HOME/hadoopdata/datanode"
    chmod -R 700 "$BASE_HOME/hadoopdata"
    echo "  - Created"
fi

echo "=== REMOTE DATANODE SETUP DONE ==="
REMOTE_EOF
}


run_all_datanode_setups() {
    local idx=1
    for DN_IP in "${ALL_DNS[@]}"; do
        run_remote_datanode_setup "$DN_IP" "$idx"
        idx=$((idx + 1))
    done
}


print_next_steps() {
    echo ""
    echo "=== FOXAI INSTALLER DONE ==="
    echo ""
    echo "Next steps:"
    echo "  1. Start HDFS:  start-dfs.sh"
    echo "  2. Start YARN:   start-yarn.sh"
    echo "  3. Verify:      yarn node -list"
    echo ""
    echo "Captured MinIO defaults for future config alignment:"
    echo "  - Endpoint: $MINIO_ENDPOINT"
    echo "  - Access key: $MINIO_ACCESS_KEY"
    echo "  - Secret key: [hidden]"
    echo ""
}


main() {
    require_command python3
    require_command ssh
    require_command rsync

    collect_inputs
    run_namenode_setup
    run_all_datanode_setups
    print_next_steps
}


main "$@"
