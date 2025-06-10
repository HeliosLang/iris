#!/bin/bash

NAME=cardano-node-ctl
VERSION=$(grep '"version"' package.json | head -1 | sed -E 's/.*"version": *"([^"]+)".*/\1/')

CARDANO_NODE_PORT=3000

CARDANO_NODE_VERSION=10.4.1
CARDANO_CLI_VERSION=10.8.0.0
CARDANO_DB_SYNC_VERSION=13.6.0.5

TMP=./tmp
DST_DIR=./dist
DST=$DST_DIR/$NAME-$VERSION.deb
PKG_ROOT=$TMP/deb-package
CONTROL_DIR=$PKG_ROOT/DEBIAN
BIN_DIR=$PKG_ROOT/usr/local/bin
SERVICE_DIR=$PKG_ROOT/etc/systemd/system
SERVICE=$SERVICE_DIR/$NAME.service
PREINST=$CONTROL_DIR/preinst
POSTINST=$CONTROL_DIR/postinst
PRERM=$CONTROL_DIR/prerm
POSTRM=$CONTROL_DIR/postrm

rm -fr $PKG_ROOT

mkdir -p $CONTROL_DIR
mkdir -p $BIN_DIR
mkdir -p $SERVICE_DIR
mkdir -p $DST_DIR

# create the package 'control' file
cat > $PKG_ROOT/DEBIAN/control << FILE
Package: $NAME
Version: $VERSION
Architecture: all
Maintainer: cschmitz398@gmail.com
Depends: git, nix, postgresql
Homepage: http://github.com/HeliosLang/cardano-node-install
Description: A Cardano Node Control panel hosted via HTTPS.
FILE

# copy the binary
cp $TMP/cardano-node-ctl $BIN_DIR/$NAME

# create the service definition
cat > $SERVICE << FILE
[Unit]
Description=Cardano Node Controller
Requires=network.target

[Service]
Type=simple
Restart=always
RestartSec=5
User=root
Group=root
WorkingDirectory=/tmp
ExecStart=$NAME
KillSignal=SIGINT
RestartKillSignal=SIGINT
StandardOutput=journal
StandardError=journal
SyslogIdentifier=$NAME
LimitNOFILE=32768

[Install]
WantedBy=multi-user.target
FILE

# the pre install script which will be used to install cardano-node, cardano-cli and cardano-db-sync, and create their service files
cat > $PREINST << FILE
#!/bin/sh

if test \$(systemctl list-unit-files $NAME.service | grep $NAME | wc -l) -eq 1; then
    if test \$(systemctl is-active $NAME) = "active"; then
        systemctl stop $NAME
    fi
fi
FILE

chmod +x $PREINST

# create the post install script that enables and launches the service
# asks for network name (preprod or mainnet)
# cardano-node is installed by cloning its repo, checking out its version tag
cat > $POSTINST << FILE
#!/bin/sh

# determine the network to connect to (defaults to preprod)
network=preprod

while true; do
    read -r -p "Which Cardano network do you want to connect to? (preprod/mainnet, default: preprod): " answer

    answer=\$(echo \$answer | tr '[:upper:]' '[:lower:]')

    if test -z \$answer || test \$answer = "yes"; then
        break
    elif test \$answer = "preprod" || test \$answer = "mainnet"; then
        network=\$answer
        break
    else
        echo "Answer must be one of yes/ENTER/preprod/mainnet, try again"
    fi
done

# download the necessary config files
if test \$(ls -1 /etc/cardano-node/\$network/*.json | wc -l) -lt 9; then
    mkdir -p /etc/cardano-node/\$network

    cd /etc/cardano-node/\$network

    # db-sync-config.json refers to others files in same directory, so is placed in /etc/cardano-node/\$network instead of /etc/cardano-db-sync/\$network
    curl -O -J "https://book.play.dev.cardano.org/environments/\$network/{config,db-sync-config,submit-api-config,topology,byron-genesis,shelley-genesis,alonzo-genesis,conway-genesis,checkpoints}.json"
fi

# configure nix with trusted IOG keys
if test \$(grep "^substituters =" /etc/nix/nix.conf | wc -l) -eq 0; then
    echo "substituters = https://cache.nixos.org https://cache.iog.io" >> /etc/nix/nix.conf
fi

if test \$(grep "^trusted-public-keys =" /etc/nix/nix.conf | wc -l) -eq 0; then
    echo "trusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY= hydra.iohk.io:f/Ea+s+dFdN+3Y/G+FDgSq+a5NEWhJGzdjvKNGv0/EQ=" >> /etc/nix/nix.conf
fi

# install cardano-node, assuming absence of the service means it hasn't been installed before
if test \$(systemctl list-unit-files cardano-node.service | grep cardano-node | wc -l) -eq 0; then
    # clone the cardano-node repo if it hasn't been cloned before
    if test \$(ls -1 /tmp/cardano-node | wc -l) -eq 0; then
        git clone https://github.com/IntersectMBO/cardano-node /tmp/cardano-node || exit 1
    fi

    # build the cardano-node binary using nix, if it hasn't been built before
    if test \$(ls -1 /nix/store | grep "cardano-node-exe-cardano-node-$CARDANO_NODE_VERSION\$" | wc -l) -eq 0; then
        cd /tmp/cardano-node

        git switch -d tags/$CARDANO_NODE_VERSION || exit 1
        
        yes | nix build --extra-experimental-features nix-command --extra-experimental-features flakes .#cardano-node || exit 1
    fi

    # create the other necessary directories for cardano-node (/etc/cardano-node should've already been created by now)
    mkdir -p /run/cardano-node
    mkdir -p /var/cache/cardano-node/\$network

    # prepare variables for the service file
    binary=/nix/store/\$(ls -1 /nix/store | grep "cardano-node-exe-cardano-node-$CARDANO_NODE_VERSION\$" | head -1)/bin/cardano-node
    public_ip=\$(hostname -I | awk '{print \$1}')

    # create the cardano-node service file
    cat > /etc/systemd/system/cardano-node.service << EOF
[Unit]
Description=Cardano Node
Requires=network.target

[Service]
Type=simple
Restart=always
RestartSec=60
User=root
Group=root
WorkingDirectory=/var/cache/cardano-node/\$network
ExecStart=\$binary run --config /etc/cardano-node/\$network/config.json --database-path /var/cache/cardano-node/\$network --socket-path /run/cardano-node/node.socket --host-addr \$public_ip --port $CARDANO_NODE_PORT --topology /etc/cardano-node/\$network/topology.json
KillSignal=SIGINT
RestartKillSignal=SIGINT
StandardOutput=journal
StandardError=journal
SyslogIdentifier=cardano-node
LimitNOFILE=32768

[Install]
WantedBy=multi-user.target
EOF
fi

# enable the cardano-node service if it hasn't been enabled before
systemctl enable cardano-node

# start the cardano-node service if it is inactive
cardano_node_status=\$(systemctl is-active cardano-node)
if test \$cardano_node_status = "inactive" || test \$cardano_node_status = "failed"; then
    echo "Starting Cardano Node..."

    systemctl start cardano-node

    echo "Started Cardano Node"
fi

# install cardano-cli, assume that if a /usr/local/bin/cardano-cli symlink is missing it hasn't been installed yet
if test \$(ls -1 /usr/local/bin/cardano-cli | wc -l) -eq 0; then
    # clone the cardano-node repo if it hasn't been cloned before
    if test \$(ls -1 /tmp/cardano-node | wc -l) -eq 0; then
        git clone https://github.com/IntersectMBO/cardano-node /tmp/cardano-node || exit 1
    fi

    # build the cardano-cli binary using nix, if it hasn't been built before
    if test \$(ls -1 /nix/store | grep "cardano-cli-exe-cardano-cli-$CARDANO_CLI_VERSION\$" | wc -l) -eq 0; then
        cd /tmp/cardano-node

        # the specified cardano cli version is bundled with the specified cardano node version
        git switch -d tags/$CARDANO_NODE_VERSION || exit 1
        
        yes | nix build --extra-experimental-features nix-command --extra-experimental-features flakes .#cardano-cli || exit 1
    fi

    # create the symlink
    binary=/nix/store/\$(ls -1 /nix/store | grep "cardano-cli-exe-cardano-cli-$CARDANO_CLI_VERSION\$" | head -1)/bin/cardano-cli
    ln -s \$binary /usr/local/bin/cardano-cli || exit 1
fi

# install cardano-db-sync, assume that if the service file doesn't exist, it hasn't been installed before
if test \$(systemctl list-unit-files cardano-db-sync.service | grep cardano-db-sync | wc -l) -eq 0; then
    # clone the cardano-db-sync repo if it hasn't been cloned before
    if test \$(ls -1 /tmp/cardano-db-sync | wc -l) -eq 0; then
        git clone https://github.com/IntersectMBO/cardano-db-sync /tmp/cardano-db-sync || exit 1
    fi

    # copy the schema update scripts
    cp -r -t /etc/cardano-db-sync/ /tmp/cardano-db-sync/schema

    # build cardano-db-sync binary using nix, if it hasn't been built before
    if test \$(ls -1 /nix/store | grep "cardano-db-sync-exe-cardano-db-sync-$CARDANO_DB_SYNC_VERSION\$" | wc -l) -eq 0; then
        cd /tmp/cardano-db-sync

        # the specified cardano cli version is bundled with the specified cardano node version
        git switch -d tags/$CARDANO_DB_SYNC_VERSION || exit 1
        
        yes | nix build --extra-experimental-features nix-command --extra-experimental-features flakes .#cardano-db-sync || exit 1
    fi

    # make necessary directories
    mkdir -p /var/cache/cardano-db-sync/\$network

    # create the db user if it doesn't already exist
    if test -z \$(sudo -u postgres psql -tAc "SELECT 1 FROM pg_user WHERE usename='root'"); then
        echo "Creating Postgres user 'root'..."

        sudo -u postgres psql -c "CREATE USER root WITH SUPERUSER PASSWORD NULL" || exit 1

        echo "Created Postgres user 'root'"
    fi
    
    # create the database if it hasn't been created before
    cardano_db_count=\$(sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='cardano_\$network'")
    if test -z \$cardano_db_count; then
        echo "Creating Postgres database 'cardano_\$network'..."
        
        sudo -u postgres psql -c "CREATE DATABASE cardano_\$network WITH TEMPLATE template0 OWNER cardano ENCODING UTF8" || exit 1

        echo "Created Postgres database 'cardano_\$network'"
    fi

    # create the PGPASSFILE,
    # only peer authentication is enabled be default, which means current root user must be used (which is why it was added to the pg_user table above)
    cat > /etc/cardano-db-sync/\$network/pgpass << EOF
/var/run/postgresql:5432:cardano_\$network:root:*
EOF

    # prepare variables for the cardano-db-sync service file
    binary=/nix/store/\$(ls -1 /nix/store | grep "cardano-db-sync-exe-cardano-db-sync-$CARDANO_DB_SYNC_VERSION\$" | head -1)/bin/cardano-db-sync

    # create the cardano-db-sync service file
cat > /etc/systemd/system/cardano-db-sync.service << EOF
[Unit]
Description=Cardano DB Sync
After=cardano-node.service

[Service]
Type=simple
Restart=always
RestartSec=60
User=root
Group=root
WorkingDirectory=/var/cache/cardano-db-sync/\$network
Environment="PGPASSFILE=/etc/cardano-db-sync/\$network/pgpass"
ExecStart=\$binary --config /etc/cardano-node/\$network/db-sync-config.json --state-dir /var/cache/cardano-node/\$network/ledger --socket-path /run/cardano-node/node.socket --schema-dir /etc/cardano-db-sync/schema
KillSignal=SIGINT
RestartKillSignal=SIGINT
StandardOutput=journal
StandardError=journal
SyslogIdentifier=cardano-db-sync
LimitNOFILE=32768

[Install]
WantedBy=multi-user.target
EOF
fi

# enable the cardano-db-sync service if it hasn't been enabled before
systemctl enable cardano-db-sync

# start the cardano-db-sync service if it is inactive
cardano_db_sync_status=\$(systemctl is-active cardano-db-sync)
if test \$cardano_db_sync_status = "inactive" || test \$cardano_db_sync_status = "failed"; then
    echo "Starting Cardano DB Sync..."

    systemctl start cardano-db-sync

    echo "Started Cardano DB Sync"
fi

# create the network file in /etc/cardano-node-ctl/
mkdir -p /etc/$NAME
echo \$network > /etc/$NAME/network

# start the actual cardano-node-ctl service
systemctl enable $NAME || exit 1
systemctl start $NAME || exit 1

# firewall rules can be handled by cardano-node-ctl itself during runtime
FILE

chmod +x $POSTINST

# create the pre cleanup script that stops the services
cat > $PRERM << FILE
#!/bin/sh

if test \$(systemctl list-unit-files cardano-node.service | grep "cardano-node" | wc -l) -eq 1; then
    cardano_node_status=\$(systemctl is-active cardano-node)
    if test \$cardano_node_status = "active" || test \$cardano_node_status = "activating"; then
        systemctl stop cardano-node
    fi
fi

if test \$(systemctl list-unit-files cardano-db-sync.service | grep "cardano-db-sync" | wc -l) -eq 1; then
    cardano_db_sync_status=\$(systemctl is-active cardano-db-sync)
    if test \$cardano_db_sync_status = "active" || test \$cardano_db_sync_status = "activating"; then
        systemctl stop cardano-db-sync
    fi
fi

if test \$(systemctl list-unit-files $NAME.service | grep $NAME | wc -l) -eq 1; then
    cardano_node_ctl_status=\$(systemctl is-active cardano-node-ctl)
    if test \$cardano_node_ctl_status = "active" || test \$cardano_node_ctl_status = "activating"; then
        systemctl stop $NAME
    fi
fi
FILE

chmod +x $PRERM

# create the post clenanup script that disables the service
# TODO: remove files related to cardano-node and cardano-db-sync
cat > $POSTRM << FILE
#!/bin/sh

# one of: "remove", "purge", "upgrade", ... 
action=\$1

if test \$action = "remove" || test \$action = "purge"; then
    if test \$(systemctl list-unit-files cardano-node.service | grep "cardano-node" | wc -l) -eq 1; then
        if test \$(systemctl is-enabled cardano-node) = "enabled"; then
            systemctl disable cardano-node
        fi
    fi

    if test \$(systemctl list-unit-files cardano-db-sync.service | grep "cardano-db-sync" | wc -l) -eq 1; then
        if test \$(systemctl is-enabled cardano-db-sync) = "enabled"; then
            systemctl disable cardano-db-sync
        fi
    fi

    if test \$(systemctl list-unit-files $NAME.service | grep $NAME | wc -l) -eq 1; then
        if test \$(systemctl is-enabled $NAME) = "enabled"; then
            systemctl disable $NAME
        fi
    fi
fi
FILE

chmod +x $POSTRM

# created the deb file
dpkg-deb --build $PKG_ROOT $DST