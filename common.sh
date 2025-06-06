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

install_cardano_binary() {
  name=$1

  echo "Installing ${name}..."

  yes | nix build --extra-experimental-features nix-command --extra-experimental-features flakes .#$name

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

create_start_script() {
  echo "Creating ${START_SCRIPT_PATH}..."

  public_ip=$(get_public_ip)

  mkdir -p $(dirname $START_SCRIPT_PATH)

  cat > $START_SCRIPT_PATH << END
#!/bin/bash
cardano-node run --config $CONFIG_DIR/config.json --database-path $DB_PATH --socket-path $SOCKET_PATH --host-addr $public_ip --port $CARDANO_NODE_PORT --topology $CONFIG_DIR/topology.json
END || exit 1

  chmod +x $START_SCRIPT_PATH || exit 1

  echo "Created ${START_SCRIPT_PATH}"
}

get_cli_testnet_magic() {
  network_name=$1

  if [[ $network_name == "mainnet" ]]
  then
    echo ""
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

  testnet_magic=$(get_cli_testnet_magic $network_name)

  mkdir -p $(dirname $TIP_SCRIPT_PATH)

  cat > $TIP_SCRIPT_PATH << EOF
#!/bin/bash
cardano-cli query tip $testnet_magic --socket-path $SOCKET_PATH
EOF || exit 1

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
User=${whoami}
Group=${id -g}
WorkingDirectory=${HOME}
ExecStart=${START_SCRIPT_PATH}
KillSignal=SIGINT
RestartKillSignal=SIGINT
StandardOutput=journal
StandardError=jounral
SyslogIdentifier=cardano-node
LimitNOFILE=32768

[Install]
WantedBy=multi-user.target
EOF || exit 1
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