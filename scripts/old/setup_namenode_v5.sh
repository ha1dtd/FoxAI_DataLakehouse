#!/bin/bash
set -euo pipefail

echo "=== NAMENODE SETUP V5 (MULTI-DATANODE: EXISTING + NEW) ==="

# ===== INPUTS =====
read -r -p "Namenode PRIVATE IP: " NN_PRIVATE_IP
read -r -p "Number of EXISTING datanodes: " NUM_EXISTING_DN
read -r -p "Number of NEW datanodes: " NUM_NEW_DN

declare -a EXISTING_DN_IPS=()
declare -a NEW_DN_IPS=()

echo ""
echo "=== EXISTING DATANODE IPs ==="
for i in $(seq 1 $NUM_EXISTING_DN); do
    read -r -p "  Existing DN$i IP: " DN_IP
    EXISTING_DN_IPS+=("$DN_IP")
done

echo "=== NEW DATANODE IPs ==="
for i in $(seq 1 $NUM_NEW_DN); do
    read -r -p "  New DN$i IP: " DN_IP
    NEW_DN_IPS+=("$DN_IP")
done

read -r -p "Datanode username: " DN_USER
read -r -p "MinIO endpoint [192.168.100.66:9001]: " MINIO_ENDPOINT
MINIO_ENDPOINT=${MINIO_ENDPOINT:-192.168.100.66:9001}
read -r -p "MinIO access key [admin]: " MINIO_ACCESS_KEY
MINIO_ACCESS_KEY=${MINIO_ACCESS_KEY:-admin}
read -r -p "MinIO secret key [12345678]: " MINIO_SECRET_KEY
MINIO_SECRET_KEY=${MINIO_SECRET_KEY:-12345678}

TOTAL_DN=$((NUM_EXISTING_DN + NUM_NEW_DN))
NN_USER=$(whoami)
BASE_HOME="/home/$NN_USER"
HADOOP_HOME="$BASE_HOME/hadoop"
SPARK_HOME="/opt/spark"
JAVA11_HOME="/usr/lib/jvm/temurin-11-jdk-amd64"

echo ""
echo "===================="
echo "STEP: SSH KEY"
echo "===================="
if [ -f ~/.ssh/id_rsa ]; then
    echo "  - SSH key exists"
else
    ssh-keygen -t rsa -N "" -f ~/.ssh/id_rsa
    echo "  - SSH key generated"
fi
mkdir -p ~/.ssh
if [ -f ~/.ssh/authorized_keys ]; then
    :
else
    touch ~/.ssh/authorized_keys
fi
chmod 600 ~/.ssh/authorized_keys
if ! grep -qF "$(cat ~/.ssh/id_rsa.pub)" ~/.ssh/authorized_keys 2>/dev/null; then
    cat ~/.ssh/id_rsa.pub >> ~/.ssh/authorized_keys
    echo "  - Key added"
fi

# ===== SSH COPY TO ALL DNs =====
ALL_DNS=("${EXISTING_DN_IPS[@]}" "${NEW_DN_IPS[@]}")
for DN_IP in "${ALL_DNS[@]}"; do
    ssh-copy-id -f "$DN_USER@$DN_IP" 2>/dev/null || true
done

echo "===================="
echo "STEP: NOPASSWD (ALL DNs)"
echo "===================="
for DN_IP in "${ALL_DNS[@]}"; do
    echo "  - Checking $DN_IP"
    if ssh -o BatchMode=yes -o ConnectTimeout=5 "$DN_USER@$DN_IP" "sudo -n true" 2>/dev/null; then
        echo "    * Already NOPASSWD"
    else
        echo "    * Configuring NOPASSWD (enter password if prompted)"
        ssh -t "$DN_USER@$DN_IP" "echo '$DN_USER ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/$DN_USER"
    fi
done

echo "===================="
echo "STEP: APT MIRROR"
echo "===================="
if grep -qE "mirror\.kakao\.com/ubuntu" /etc/apt/sources.list; then
    echo "  - Already configured"
else
    sudo sed -i 's|http://archive.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list
    sudo sed -i 's|http://security.ubuntu.com/ubuntu|http://mirror.kakao.com/ubuntu|g' /etc/apt/sources.list
    echo "  - Configured"
fi

echo "===================="
echo "STEP: BASE PACKAGES"
echo "===================="
BASE_PKGS="wget gpg ssh pdsh python3-venv python3-pip curl tar rsync unzip build-essential libffi-dev python3-dev libsasl2-dev libldap2-dev default-libmysqlclient-dev"
MISSING_PKGS=""
for p in $BASE_PKGS; do
    if dpkg -s "$p" >/dev/null 2>&1; then
        :
    else
        MISSING_PKGS="$MISSING_PKGS $p"
    fi
done
if [ -n "$MISSING_PKGS" ]; then
    sudo apt update
    sudo apt install -y $MISSING_PKGS
    echo "  - Installed: $MISSING_PKGS"
else
    echo "  - Already installed"
fi

if [ ! -x /usr/bin/python ]; then
    sudo ln -sf /usr/bin/python3 /usr/bin/python
    echo "  - Linked python -> python3"
fi

echo "===================="
echo "STEP: JAVA 11"
echo "===================="
if [ -d "$JAVA11_HOME" ]; then
    echo "  - Already installed"
else
    if [ ! -f /usr/share/keyrings/adoptium.gpg ]; then
        wget -4 -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | sudo gpg --dearmor -o /usr/share/keyrings/adoptium.gpg
    fi
    . /etc/os-release
    ADOPT_CODENAME="${VERSION_CODENAME:-bookworm}"
    if [ ! -f /etc/apt/sources.list.d/adoptium.list ]; then
        echo "deb [signed-by=/usr/share/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb ${ADOPT_CODENAME} main" | sudo tee /etc/apt/sources.list.d/adoptium.list
    fi
    sudo apt update
    sudo apt install -y temurin-11-jdk
    echo "  - Installed"
fi

[ -d "$JAVA11_HOME" ] || { echo "JAVA_HOME invalid"; exit 1; }

echo "===================="
echo "STEP: HADOOP"
echo "===================="
if [ -x "$HADOOP_HOME/bin/hdfs" ]; then
    echo "  - Already installed"
else
    cd "$BASE_HOME"
    wget -4 https://dlcdn.apache.org/hadoop/common/hadoop-3.3.6/hadoop-3.3.6.tar.gz
    tar -xzf hadoop-3.3.6.tar.gz
    mv hadoop-3.3.6 "$HADOOP_HOME"
    echo "  - Installed"
fi

echo "===================="
echo "STEP: SPARK"
echo "===================="
if [ -x "$SPARK_HOME/bin/spark-submit" ]; then
    echo "  - Already installed"
else
    cd "$BASE_HOME"
    wget -4 https://dlcdn.apache.org/spark/spark-3.5.8/spark-3.5.8-bin-hadoop3.tgz
    tar -xzf spark-3.5.8-bin-hadoop3.tgz
    sudo mv spark-3.5.8-bin-hadoop3 "$SPARK_HOME"
    sudo chown -R "$NN_USER:$NN_USER" "$SPARK_HOME"
    echo "  - Installed"
fi

echo "===================="
echo "STEP: BASHRC"
echo "===================="
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

echo "===================="
echo "STEP: /etc/hosts"
echo "===================="
HOSTS_BEGIN="# >>> FOXAI CLUSTER HOSTS >>>"
HOSTS_END="# <<< FOXAI CLUSTER HOSTS <<<"

# Build hosts block for Python
HOSTS_BLOCK_FILE="/tmp/v5_hosts_block.txt"
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
hosts_path = '/etc/hosts'

with open(block_file) as f:
    new_block = f.read()

with open(hosts_path) as f:
    text = f.read()

begin = '# >>> FOXAI CLUSTER HOSTS >>>'
end = '# <<< FOXAI CLUSTER HOSTS <<<'

start = text.find(begin)
if start != -1:
    end_idx = text.find(end, start)
    if end_idx != -1:
        end_idx += len(end)
        text = text[:start] + text[end_idx:]

if text and not text.endswith('\n'):
    text += '\n'
text += new_block

with open(hosts_path, 'w') as f:
    f.write(text)

print("done")
PYEOF
echo "  - Updated"

echo "===================="
echo "STEP: HADOOP DATA DIR"
echo "===================="
if [ -d "$BASE_HOME/hadoopdata/namenode" ]; then
    chmod -R 700 "$BASE_HOME/hadoopdata"
    echo "  - Already exists"
else
    mkdir -p "$BASE_HOME/hadoopdata/namenode"
    chmod -R 700 "$BASE_HOME/hadoopdata"
    echo "  - Created"
fi

echo "===================="
echo "STEP: CORE-SITE.XML"
echo "===================="
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

echo "===================="
echo "STEP: HDFS-SITE.XML"
echo "===================="
REP_VALUE=$([ $TOTAL_DN -lt 3 ] && echo $TOTAL_DN || echo 3)
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

echo "===================="
echo "STEP: WORKERS FILE"
echo "===================="
WORKERS_EXISTS=true
for i in $(seq 1 $TOTAL_DN); do
    if ! grep -qEx "datanode$i" "$HADOOP_HOME/etc/hadoop/workers" 2>/dev/null; then
        WORKERS_EXISTS=false
        break
    fi
done

if $WORKERS_EXISTS; then
    echo "  - Already configured"
else
> "$HADOOP_HOME/etc/hadoop/workers"
for i in $(seq 1 $TOTAL_DN); do
    echo "datanode$i" >> "$HADOOP_HOME/etc/hadoop/workers"
done
    echo "  - Configured (1-$TOTAL_DN)"
fi

echo "===================="
echo "STEP: MAPRED-SITE.XML"
echo "===================="
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

echo "===================="
echo "STEP: YARN-SITE.XML"
echo "===================="
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

echo "===================="
echo "STEP: HADOOP-ENV.SH"
echo "===================="
HADOOP_ENV="$HADOOP_HOME/etc/hadoop/hadoop-env.sh"
FLEXIBLE_JAVA="export JAVA_HOME=\${JAVA_HOME:-$JAVA11_HOME}"
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

echo "===================="
echo "STEP: FORMAT NAMENODE"
echo "===================="
if [ -d "$BASE_HOME/hadoopdata/namenode/current" ]; then
    echo "  - Already formatted"
else
    "$HADOOP_HOME/bin/hdfs" namenode -format -force -nonInteractive
    echo "  - Formatted"
fi

echo "===================="
echo "STEP: SYNC TO EXISTING DATANODES (CONFIGS ONLY)"
echo "===================="
for DN_IP in "${EXISTING_DN_IPS[@]}"; do
    echo "  - Syncing to $DN_IP (existing)"
    rsync -az --delete "$HADOOP_HOME/" "$DN_USER@$DN_IP:$HADOOP_HOME/"
    rsync -az --delete "$SPARK_HOME/" "$DN_USER@$DN_IP:$SPARK_HOME/" 2>/dev/null || true
done

echo "===================="
echo "STEP: SYNC TO NEW DATANODES (FULL)"
echo "===================="
for DN_IP in "${NEW_DN_IPS[@]}"; do
    echo "  - Syncing to $DN_IP (new)"
    rsync -az --delete "$HADOOP_HOME/" "$DN_USER@$DN_IP:$HADOOP_HOME/"
    rsync -az --delete "$SPARK_HOME/" "$DN_USER@$DN_IP:$SPARK_HOME/" 2>/dev/null || true
done

echo ""
echo "=== V5 SETUP DONE ==="
echo ""
echo "Next steps:"
echo "  1. Start HDFS:  start-dfs.sh"
echo "  2. Start YARN:   start-yarn.sh"
echo "  3. Verify:      yarn node -list"
echo ""
