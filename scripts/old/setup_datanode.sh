#!/bin/bash
set -euo pipefail

echo "=== DATANODE SETUP V2 (FLEXIBLE, MULTI-DATANODE) ==="

read -r -p "Namenode PRIVATE IP: " NN_PRIVATE_IP
read -r -p "Total number of datanodes in cluster: " TOTAL_DN
read -r -p "This datanode number (e.g. 1, 2, 3...): " MY_DN_NUM
read -r -p "Datanode username: " DN_USER

if [ "$(whoami)" != "$DN_USER" ]; then
    echo "ERROR: Run as user '$DN_USER' (now: $(whoami))."
    exit 1
fi

BASE_HOME="/home/$DN_USER"
HADOOP_HOME="$BASE_HOME/hadoop"
SPARK_HOME="/opt/spark"
JAVA_HOME="/usr/lib/jvm/temurin-11-jdk-amd64"

echo ""
echo "===================="
echo "STEP: CHECK HADOOP FROM NAMENODE"
echo "===================="
if [ ! -x "$HADOOP_HOME/bin/hdfs" ]; then
    echo "  ERROR: $HADOOP_HOME missing."
    echo "  Run setup_namenode_v5.sh on Namenode first so it rsyncs Hadoop here."
    exit 1
fi
echo "  - Hadoop present"

echo "===================="
echo "STEP: CHECK SPARK (/opt/spark)"
echo "===================="
if [ -x "$SPARK_HOME/bin/spark-submit" ]; then
    echo "  - Spark present"
else
    echo "  WARNING: Spark not found at $SPARK_HOME"
fi

echo "===================="
echo "STEP: APT MIRROR (KAKAO)"
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
BASE_PKGS="wget gpg ssh pdsh python3-venv python3-pip curl tar rsync unzip build-essential libffi-dev python3-dev libsasl2-dev libldap2-dev"
MISSING=""
for p in $BASE_PKGS; do
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

echo "===================="
echo "STEP: JAVA 11 (TEMURIN 11)"
echo "===================="
if [ -d "$JAVA_HOME" ]; then
    echo "  - Already installed"
else
    if [ ! -f /usr/share/keyrings/adoptium.gpg ]; then
        wget -4 -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | \
            sudo gpg --dearmor -o /usr/share/keyrings/adoptium.gpg
    fi
    . /etc/os-release
    ADOPT_CODENAME="${VERSION_CODENAME:-bookworm}"
    if [ ! -f /etc/apt/sources.list.d/adoptium.list ]; then
        echo "deb [signed-by=/usr/share/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb ${ADOPT_CODENAME} main" | \
            sudo tee /etc/apt/sources.list.d/adoptium.list >/dev/null
    fi
    sudo apt update
    sudo apt install -y temurin-11-jdk
    echo "  - Installed"
fi

echo "===================="
echo "STEP: /etc/hosts (MINIMAL LOCAL BLOCK)"
echo "===================="
echo "  Note: Full block will be pushed by Namenode rsync after setup_namenode_v5.sh runs"
MY_IP=$(hostname -I | awk '{print $1}')

sudo python3 - "$NN_PRIVATE_IP" "$MY_IP" "$MY_DN_NUM" <<'PYEOF'
import sys
nn_ip, my_ip, my_num = sys.argv[1], sys.argv[2], sys.argv[3]
hosts = open('/etc/hosts').read()
begin = '# >>> FOXAI CLUSTER HOSTS >>>'
end = '# <<< FOXAI CLUSTER HOSTS <<<'
start = hosts.find(begin)
if start != -1:
    end_idx = hosts.find(end, start)
    if end_idx != -1:
        hosts = hosts[:start] + hosts[end_idx + len(end):]
minimal = f'''{begin}
{nn_ip} namenode
datanode{my_num} {my_ip}
{end}
'''
hosts = hosts.rstrip() + '\n' + minimal
open('/etc/hosts', 'w').write(hosts)
PYEOF
echo "  - Written (full block pushed by Namenode rsync later)"

echo "===================="
echo "STEP: HADOOP JAVA_HOME"
echo "===================="
HADOOP_ENV="$HADOOP_HOME/etc/hadoop/hadoop-env.sh"
if grep -qE "^export JAVA_HOME=\\$\\{JAVA_HOME" "$HADOOP_ENV" 2>/dev/null; then
    echo "  - Already configured (flexible)"
elif grep -qE "^export JAVA_HOME=" "$HADOOP_ENV" 2>/dev/null; then
    sed -i "s|^export JAVA_HOME=.*|export JAVA_HOME=$JAVA_HOME|" "$HADOOP_ENV"
    echo "  - Updated"
else
    echo "export JAVA_HOME=$JAVA_HOME" >> "$HADOOP_ENV"
    echo "  - Added"
fi

echo "===================="
echo "STEP: SHELL ENVIRONMENT"
echo "===================="
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

echo "===================="
echo "STEP: DATANODE DIRECTORY"
echo "===================="
if [ -d "$BASE_HOME/hadoopdata/datanode" ]; then
    chmod -R 700 "$BASE_HOME/hadoopdata"
    echo "  - Already exists"
else
    mkdir -p "$BASE_HOME/hadoopdata/datanode"
    chmod -R 700 "$BASE_HOME/hadoopdata"
    echo "  - Created"
fi

echo ""
echo "=== DATANODE SETUP DONE ==="
echo ""
echo "On Namenode: start-dfs.sh && start-yarn.sh"