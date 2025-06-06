#!/bin/bash
set -euo pipefail
source <(curl -s https://raw.githubusercontent.com/HeliosLang/cardano-node-install/refs/heads/main/common.sh)

install_cardano_node_and_cli "preprod"