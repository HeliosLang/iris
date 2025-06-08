if [[ -z "$NETWORK_NAME" ]]
then
    env
    echo "Must provide NETWORK_NAME in environment" 1>&2
    exit 1
fi

CARDANO_NODE_REPO="https://github.com/IntersectMBO/cardano-node"
CARDANO_NODE_VERSION="10.4.1"
CARDANO_DB_SYNC_VERSION="13.6.0.5"
CARDANO_NODE_PORT=3001
BLOCKFROST_PORT=3000
USER=$SUDO_USER
HOME=/home/$USER
CARDANO_NODE_SRC_DIR=$HOME/cardano-node-source
CARDANO_DB_SYNC_SRC_DIR=$HOME/cardano-db-sync-source
DB_PATH="$HOME/db"
SOCKET_PATH="$DB_PATH/node.socket"
CONFIG_DIR="$HOME/cardano-node-config"
SCRIPTS_DIR="$HOME/scripts"
START_SCRIPT_PATH="$SCRIPTS_DIR/start.sh"
START_BLOCKFROST_SCRIPT_PATH="$SCRIPTS_DIR/start_blockfrost.sh"
START_CARDANO_DB_SYNC_SCRIPT_PATH="$SCRIPTS_DIR/start_db_sync.sh"
TIP_SCRIPT_PATH="$SCRIPTS_DIR/tip.sh"
CARDANO_NODE_SERVICE_PATH="/etc/systemd/system/cardano-node.service"
BLOCKFROST_SERVICE_PATH="/etc/systemd/system/blockfrost.service"
CARDANO_DB_SYNC_SERVICE_PATH="/etc/systemd/system/cardano-db-sync.service"

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

add_to_path() {
  p=$1

  echo "export PATH=\$PATH:$p" >> $HOME/.bashrc
}

install_cardano_binary() {
  name=$1

  echo "Installing ${name}..."

  yes | nix_build .#$name

  bin_dir=$(get_nix_store_dir $name)

  echo "Adding ${bin_dir} to $HOME/.bashrc"

  add_to_path $bin_dir

  echo "Installed ${name}"
}

install_cardano_node() {
  install_cardano_binary "cardano-node"
}

install_cardano_cli() {
  install_cardano_binary "cardano-cli"
}

get_magic_number() {
  if [[ $NETWORK_NAME == "mainnet" ]]
  then
    echo "764824073"
  elif [[ $NETWORK_NAME == "preprod" ]]
  then
    echo "1"
  else
    echo "unhandled network name $NETWORK_NAME"
    exit 1
  fi
}

download_network_config() {
  echo "Downloading network config for ${NETWORK_NAME}..."

  mkdir -p $CONFIG_DIR

  cd $CONFIG_DIR

  curl -O -J "https://book.play.dev.cardano.org/environments/$NETWORK_NAME/{config,db-sync-config,submit-api-config,topology,byron-genesis,shelley-genesis,alonzo-genesis,conway-genesis,checkpoints}.json"

  echo "Downloaded network config for ${NETWORK_NAME}"
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

get_cardano_db_sync_path() {
  echo "$(get_nix_store_dir "cardano-db-sync")/cardano-db-sync"
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
  if [[ $NETWORK_NAME == "mainnet" ]]
  then
    echo "--mainnet"
  elif [[ $NETWORK_NAME == "preprod" ]]
  then
    echo "--testnet-magic 1"
  else
    echo "unhandled network name $NETWORK_NAME"
    exit 1
  fi
}

create_tip_script() {
  echo "Creating ${TIP_SCRIPT_PATH}..."

  network_flag=$(get_cli_network_flag $NETWORK_NAME)
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
  mkdir -p $(dirname $CARDANO_NODE_SERVICE_PATH)

  cat > $CARDANO_NODE_SERVICE_PATH << EOF
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
  install_build_deps || exit 1

  clone_cardano_node_source || exit 1

  install_cardano_node || exit 1

  install_cardano_cli || exit 1

  download_network_config || exit 1

  create_start_script || exit 1

  create_tip_script || exit 1

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

get_blockfrost_bin_dir() {
  echo "$HOME/blockfrost-platform/bin"
}

get_blockfrost_bin_path() {
  echo "$(get_blockfrost_bin_dir)/blockfrost-platform"
}

create_start_blockfrost_script() {
  echo "Creating ${START_BLOCKFROST_SCRIPT_PATH}..."

  public_ip=$(get_public_ip)
  bin_path=$(get_blockfrost_bin_path)

  mkdir -p $(dirname $START_BLOCKFROST_SCRIPT_PATH)

  cat > $START_BLOCKFROST_SCRIPT_PATH << END
#!/bin/bash
$bin_path --solitary --server-address $public_ip --server-port $BLOCKFROST_PORT --network $NETWORK_NAME --node-socket-path $SOCKET_PATH --mode full
END

  chmod +x $START_BLOCKFROST_SCRIPT_PATH || exit 1

  echo "Created ${START_BLOCKFROST_SCRIPT_PATH}"
}

create_blockfrost_service() {
  mkdir -p $(dirname $BLOCKFROST_SERVICE_PATH)

  cat > $BLOCKFROST_SERVICE_PATH << EOF
[Unit]
Description=Blockfrost
After=cardano-node.service

[Service]
Type=simple
Restart=always
RestartSec=60
User=$USER
Group=1000
WorkingDirectory=$HOME
ExecStart=$START_BLOCKFROST_SCRIPT_PATH
KillSignal=SIGINT
RestartKillSignal=SIGINT
StandardOutput=journal
StandardError=journal
SyslogIdentifier=blockfrost
LimitNOFILE=32768

[Install]
WantedBy=multi-user.target
EOF
}

start_blockfrost_service() {
  echo "Starting blockfrost service..."
  create_blockfrost_service

  systemctl enable blockfrost
  systemctl start blockfrost

  echo "Started blockfrost service"
}

install_blockfrost() {
  archive=$(get_blockfrost_archive_name)

  download_path=$HOME/$archive

  base_url="https://github.com/blockfrost/blockfrost-platform/releases/download/0.0.2"

  wget -O "${download_path}" "$base_url/$archive" || exit 1

  tar -xjf "$download_path" || exit 1

  add_to_path $(get_blockfrost_bin_dir) || exit 1

  create_start_blockfrost_script || exit 1

  start_blockfrost_service || exit 1
}

install_postgresql() {
  # TODO: search for the latest available version first
  apt-get -y install postgresql-15
}

create_start_cardano_db_sync_script() {
  echo "Creating ${START_CARDANO_DB_SYNC_SCRIPT_PATH}..."

  bin_path=$(get_cardano_db_sync_bin_path)

  mkdir -p $(dirname $START_CARDANO_DB_SYNC_SCRIPT_PATH)

  cat > $START_CARDANO_DB_SYNC_SCRIPT_PATH << END
#!/bin/bash
export PGPASSFILE=$CARDANO_DB_SYNC_SRC_DIR/config/pgpass-mainnet
$bin_path --config $CONFIG_DIR/db-sync-config.json --socket-path $SOCKET_PATH --state-dir $DB_PATH/ledger --schema-dir $CARDANO_DB_SYNC_SRC_DIR/schema
END

  chmod +x $START_CARDANO_DB_SYNC_SCRIPT_PATH || exit 1

  echo "Created ${START_CARDANO_DB_SYNC_SCRIPT_PATH}"
}

create_cardano_db_sync_service() {
  mkdir -p $(dirname $START_CARDANO_DB_SYNC_SCRIPT_PATH)

  cat > $START_CARDANO_DB_SYNC_SCRIPT_PATH << EOF
[Unit]
Description=Cardano DB Sync
After=cardano-node.service

[Service]
Type=simple
Restart=always
RestartSec=60
User=$USER
Group=1000
WorkingDirectory=$HOME
ExecStart=$START_CARDANO_DB_SYNC_SCRIPT_PATH
KillSignal=SIGINT
RestartKillSignal=SIGINT
StandardOutput=journal
StandardError=journal
SyslogIdentifier=cardano_db_sync
LimitNOFILE=32768

[Install]
WantedBy=multi-user.target
EOF
}

start_cardano_db_sync_service() {
  echo "Starting blockfrost service..."
  create_cardano_db_sync_service

  systemctl enable cardano-db-sync
  systemctl start cardano-db-sync

  echo "Started cardano-db-sync service"
}

install_cardano_db_sync() {
  install_postgresql || exit 1

  git clone https://github.com/IntersectMBO/cardano-db-sync.git $CARDANO_DB_SYNC_SRC_DIR

  cd $CARDANO_DB_SYNC_SRC_DIR

  git fetch --all --tags 

  git checkout tags/$CARDANO_DB_SYNC_VERSION

  yes | nix_build .

  install_cardano_binary "cardano-db-sync" || exit 1

  # all the config files should've already been downloaded by now
  cd $HOME
  export PGPASSFILE="${CARDANO_DB_SYNC_SRC_DIR}/config/pgpass-mainnet"

  # add the current user as a db role (password doesn't seem to matter?)
  sudo -u postgres psql -c "CREATE USER $USER WITH SUPERUSER PASSWORD '12345678'"

  # create the database
  sudo -u postgres -E ${CARDANO_DB_SYNC_SRC_DIR}/scripts/postgresql-setup.sh --createdb

  create_start_cardano_db_sync_script || exit 1

  start_cardano_db_sync_service || exit 1
}