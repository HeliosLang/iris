CARDANO_NODE_REPO="https://github.com/IntersectMBO/cardano-node"
CARDANO_NODE_VERSION="10.4.1"
CARDANO_NODE_PORT=3001
USER=$SUDO_USER
HOME=/home/$USER
CARDANO_NODE_SRC_DIR=$HOME/cardano-node-source
DB_PATH="$HOME/db"
SOCKET_PATH="$DB_PATH/node.socket"
CONFIG_DIR="$HOME/cardano-node-config"
SCRIPTS_DIR="$HOME/scripts"
START_SCRIPT_PATH="$SCRIPTS_DIR/start.sh"
TIP_SCRIPT_PATH="$SCRIPTS_DIR/tip.sh"
SYSTEMD_SERVICE_PATH="/etc/systemd/system/cardano-node.service"

install_build_deps() {
  apt-get -y install git nix
}

clone_cardano_node_source() {
  git clone $CARDANO_NODE_REPO $CARDANO_NODE_SRC_DIR || exit 1

  cd $CARDANO_NODE_SRC_DIR

  git switch -d tags/${CARDANO_NODE_VERSION}
}

get_nix_store_dir() {
  file=$1

  find /nix/store/*/bin -name $file | head -1 | xargs dirname
}

nix_build() {
  nix build --extra-experimental-features nix-command --extra-experimental-features flakes $@
}

install_cardano_binary() {
  name=$1

  echo "Installing ${name}..."

  yes | nix_build .#$name

  bin_dir=$(get_nix_store_dir $name)

  echo "Adding ${bin_dir} to $HOME/.bashrc"

  echo "export \$PATH=\$PATH:$bin_dir" >> $HOME/.bashrc

  echo "Installed ${name}"
}

install_cardano_node() {
  install_cardano_binary "cardano-node"
}

install_cardano_cli() {
  install_cardano_binary "cardano-cli"
}

get_magic_number() {
  network_name=$1

  if [[ $network_name == "mainnet" ]]
  then
    echo "764824073"
  elif [[ $network_name == "preprod" ]]
  then
    echo "1"
  else
    echo "unhandled network name $network_name"
    exit 1
  fi
}

download_network_config() {
  network_name=$1

  echo "Downloading network config for ${network_name}..."

  mkdir -p $CONFIG_DIR

  cd $CONFIG_DIR

  curl -O -J "https://book.play.dev.cardano.org/environments/$network_name/{config,db-sync-config,submit-api-config,topology,byron-genesis,shelley-genesis,alonzo-genesis,conway-genesis,checkpoints}.json"

  echo "Downloaded network config for ${network_name}"
}

get_public_ip() {
  hostname -I | awk '{print $1}'
}

get_cardano_node_path() {
  echo "$(get_nix_store_dir "cardano-node")/cardano-node"
}

get_cardano_cli_path() {
  echo "$(get_nix_store_dir "cardano-cli")/cardano-cli"
}

create_start_script() {
  echo "Creating ${START_SCRIPT_PATH}..."

  public_ip=$(get_public_ip)
  bin_path=$(get_cardano_node_path)

  mkdir -p $(dirname $START_SCRIPT_PATH)

  cat > $START_SCRIPT_PATH << END
#!/bin/bash
$bin_path run --config $CONFIG_DIR/config.json --database-path $DB_PATH --socket-path $SOCKET_PATH --host-addr $public_ip --port $CARDANO_NODE_PORT --topology $CONFIG_DIR/topology.json
END

  chmod +x $START_SCRIPT_PATH || exit 1

  echo "Created ${START_SCRIPT_PATH}"
}

get_cli_network_flag() {
  network_name=$1

  if [[ $network_name == "mainnet" ]]
  then
    echo "--mainnet"
  elif [[ $network_name == "preprod" ]]
  then
    echo "--testnet-magic 1"
  else
    echo "unhandled network name $network_name"
    exit 1
  fi
}

create_tip_script() {
  network_name=$1

  echo "Creating ${TIP_SCRIPT_PATH}..."

  network_flag=$(get_cli_network_flag $network_name)
  bin_path=$(get_cardano_cli_path)

  mkdir -p $(dirname $TIP_SCRIPT_PATH)

  cat > $TIP_SCRIPT_PATH << EOF
#!/bin/bash
$bin_path query tip $network_flag --socket-path $SOCKET_PATH
EOF

  chmod +x $TIP_SCRIPT_PATH || exit 1

  echo "Created ${TIP_SCRIPT_PATH}"
}

create_cardano_node_service() {
  mkdir -p $(dirname $SYSTEMD_SERVICE_PATH)

  cat > $SYSTEMD_SERVICE_PATH << EOF
[Unit]
Description=Cardano Node
Requires=network.target

[Service]
Type=simple
Restart=always
RestartSec=60
User=$USER
Group=1000
WorkingDirectory=$HOME
ExecStart=$START_SCRIPT_PATH
KillSignal=SIGINT
RestartKillSignal=SIGINT
StandardOutput=journal
StandardError=journal
SyslogIdentifier=cardano-node
LimitNOFILE=32768

[Install]
WantedBy=multi-user.target
EOF
}

start_cardano_node_service() {
  echo "Starting cardano-node service..."
  create_cardano_node_service

  systemctl enable cardano-node
  systemctl start cardano-node

  echo "Started cardano-node service"
}

install_cardano_node_and_cli() {
  network_name=$1

  install_build_deps || exit 1

  clone_cardano_node_source || exit 1

  install_cardano_node || exit 1

  install_cardano_cli || exit 1

  download_network_config $network_name || exit 1

  create_start_script || exit 1

  create_tip_script $network_name || exit 1

  start_cardano_node_service || exit 1
}

get_blockfrost_archive_name() {
  project_name="blockfrost-platform"
  version="0.0.2"
  short_rev="e06029b"

  isa="$(uname -m)"
  kernel="$(uname -s)"
  case "${isa}-${kernel}" in
  "x86_64-Linux")
    target_system="x86_64-linux"
    #expected_sha256="b08ec52144fffa8332dab3a6590991206df92752e91e809013588dd2ae1077ee"
    ;;
  "aarch64-Linux")
    target_system="aarch64-linux"
    #expected_sha256="f8210e02606ee3ad47174552c0e6b1e60759714b9d44d909f5cf135486bc63bf"
    ;;
  "x86_64-Darwin")
    target_system="x86_64-darwin"
    #expected_sha256="ad6d1f3cffbd0242c21cf8016babce3d834bba4c2340c82ed3dd88fc04ca5800"
    ;;
  # Apple Silicon can appear as "arm64-Darwin" rather than "aarch64-Darwin":
  "arm64-Darwin")
    target_system="aarch64-darwin"
    #expected_sha256="699c352358132b9a349fe74619fa592ffae9478d792d209d903f319bb5c56c31"
    ;;
  "aarch64-Darwin")
    target_system="aarch64-darwin"
    #expected_sha256="699c352358132b9a349fe74619fa592ffae9478d792d209d903f319bb5c56c31"
    ;;
  *)
    echo >&2 "fatal: no matching installer found for ${isa}-${kernel}" >&2
    exit 1
    ;;
  esac
  unset isa
  unset kernel

  archive="${project_name}-${version}-${short_rev}-${target_system}.tar.bz2"

  echo $archive
}

install_blockfrost() {
  archive=$(get_blockfrost_archive_name)
  download_path=$HOME/$archive
  base_url="https://github.com/blockfrost/blockfrost-platform/releases/download/0.0.2"
  wget -O "${download_path}" "$base_url/$archive"

  tar -xjf "$download_path"
}