//go:build linux

package installer

const remoteDataNodeScript = `set -euo pipefail

NN_PRIVATE_IP="$1"
TOTAL_DN="$2"
MY_DN_NUM="$3"
DN_USER="$4"
USE_KAKAO_MIRROR="$5"
JAVA_HOME="$6"
SPARK_HOME="$7"
HADOOP_HOME="$8"
shift 8
ALL_DN_IPS=("$@")
BASE_HOME="/home/$DN_USER"
HOSTS_BEGIN="# >>> FOXAI CLUSTER HOSTS >>>"
HOSTS_END="# <<< FOXAI CLUSTER HOSTS <<<"
ENV_BEGIN="# >>> FOXAI MANAGED ENV >>>"
ENV_END="# <<< FOXAI MANAGED ENV <<<"

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

if [ ! -e /usr/bin/python ]; then
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

section "/etc/hosts (FULL MANAGED BLOCK)"
echo "  Note: ` + remoteDNProbeBlockNote + `"
HOSTS_STATUS="$(sudo python3 - "$NN_PRIVATE_IP" "$HOSTS_BEGIN" "$HOSTS_END" "${ALL_DN_IPS[@]}" <<'PYEOF'
import sys

nn_ip, begin, end = sys.argv[1], sys.argv[2], sys.argv[3]
dn_ips = sys.argv[4:]
hosts = open("/etc/hosts").read()
lines = [begin, f"{nn_ip} namenode"]
for idx, ip in enumerate(dn_ips, start=1):
    lines.append(f"{ip} datanode{idx}")
lines.append(end)
desired = "\n".join(lines) + "\n"
start = hosts.find(begin)
if start != -1:
    end_idx = hosts.find(end, start)
    if end_idx != -1:
        end_idx += len(end)
        current = hosts[start:end_idx]
        if current.strip() == desired.strip():
            print("exact")
            raise SystemExit(0)
        hosts = hosts[:start] + desired + hosts[end_idx:]
        print("updated")
        open("/etc/hosts", "w").write(hosts.rstrip() + "\n")
        raise SystemExit(0)
hosts = hosts.rstrip() + "\n" + desired
open("/etc/hosts", "w").write(hosts.rstrip() + "\n")
print("written")
PYEOF
)"
if [ "$HOSTS_STATUS" = "exact" ]; then
    echo "  - Already configured"
elif [ "$HOSTS_STATUS" = "written" ] || [ "$HOSTS_STATUS" = "updated" ]; then
    echo "  - Updated"
else
    echo "ERROR: failed to manage /etc/hosts FoxAI block"
    exit 1
fi

section "HADOOP JAVA_HOME"
HADOOP_ENV="$HADOOP_HOME/etc/hadoop/hadoop-env.sh"
FLEXIBLE_JAVA="export JAVA_HOME=\${JAVA_HOME:-$JAVA_HOME}"
FIXED_JAVA="export JAVA_HOME=$JAVA_HOME"
if grep -qF "$FLEXIBLE_JAVA" "$HADOOP_ENV" 2>/dev/null; then
    echo "  - Already configured (flexible)"
elif grep -qFx "$FIXED_JAVA" "$HADOOP_ENV" 2>/dev/null; then
    echo "  - Already configured"
elif grep -qE '^# export JAVA_HOME=' "$HADOOP_ENV" 2>/dev/null; then
    sed -i "s|^# export JAVA_HOME=.*|$FLEXIBLE_JAVA|" "$HADOOP_ENV"
    echo "  - Updated"
elif grep -qE "^export JAVA_HOME=" "$HADOOP_ENV" 2>/dev/null; then
    sed -i "s|^export JAVA_HOME=.*|$FLEXIBLE_JAVA|" "$HADOOP_ENV"
    echo "  - Updated"
else
    echo "$FLEXIBLE_JAVA" >> "$HADOOP_ENV"
    echo "  - Added"
fi

section "SHELL ENVIRONMENT"
ENV_BLOCK="$(cat <<EOT
${ENV_BEGIN}
export JAVA_HOME=$JAVA_HOME
export HADOOP_HOME=$HADOOP_HOME
export SPARK_HOME=$SPARK_HOME
export HADOOP_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export YARN_CONF_DIR=\$HADOOP_HOME/etc/hadoop
export PATH=\$PATH:\$JAVA_HOME/bin:\$HADOOP_HOME/bin:\$HADOOP_HOME/sbin:\$SPARK_HOME/bin:\$SPARK_HOME/sbin
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh
${ENV_END}
EOT
)"
ENV_STATUS="$(python3 - "$HOME/.bashrc" "$ENV_BEGIN" "$ENV_END" "$ENV_BLOCK" <<'PYEOF'
import sys

path, begin, end, desired = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
text = open(path).read()
start = text.find(begin)
if start != -1:
    end_idx = text.find(end, start)
    if end_idx == -1:
        print("error")
        raise SystemExit(0)
    end_idx += len(end)
    current = text[start:end_idx]
    if current.strip() == desired.strip():
        print("exact")
        raise SystemExit(0)
    text = text[:start] + desired + "\n" + text[end_idx:].lstrip("\n")
    open(path, "w").write(text.rstrip() + "\n")
    print("updated")
    raise SystemExit(0)
text = text.rstrip() + "\n" + desired + "\n"
open(path, "w").write(text.rstrip() + "\n")
print("written")
PYEOF
)"
if [ "$ENV_STATUS" = "exact" ]; then
    echo "  - Already configured"
elif [ "$ENV_STATUS" = "written" ] || [ "$ENV_STATUS" = "updated" ]; then
    echo "  - Updated"
else
    echo "ERROR: failed to manage FoxAI environment block in ~/.bashrc"
    exit 1
fi

source "$HOME/.bashrc" >/dev/null 2>&1 || true

export JAVA_HOME="$JAVA_HOME"
export HADOOP_HOME="$HADOOP_HOME"
export SPARK_HOME="$SPARK_HOME"
export HADOOP_CONF_DIR="$HADOOP_HOME/etc/hadoop"
export YARN_CONF_DIR="$HADOOP_HOME/etc/hadoop"
export PATH="$JAVA_HOME/bin:$HADOOP_HOME/bin:$HADOOP_HOME/sbin:$SPARK_HOME/bin:$SPARK_HOME/sbin:$PATH"
export HADOOP_SSH_OPTS="-o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=10"
export PDSH_RCMD_TYPE=ssh

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
`
