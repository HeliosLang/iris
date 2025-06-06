# Cardano Node installation bash scripts

Promptless Cardano Node install bash scripts, for installing cardano-node and other services on clean Linux bare metal servers without using docker.

Dependencies:
   - bash
   - curl

Tested on clean installs of: 
   - Debian 12

**!!DO NOT RUN THESE SCRIPTS IN YOUR LOCAL ENVIRONMENT!!**

## How to install

Run any of the following commands to install cardano-node along with

### Preprod `cardano-node` + `blockfrost`

```bash
curl -fsSL https://raw.githubusercontent.com/HeliosLang/cardano-node-install/refs/heads/main/preprod-with-blockfrost.sh | sudo bash
```

### Mainnet `cardano-node` + `blockfrost`

```bash
curl -fsSL https://raw.githubusercontent.com/HeliosLang/cardano-node-install/refs/heads/main/mainnet-with-blockfrost.sh | sudo bash
```

## Contributing

Don't waste time with fancy terminal colors.
Add to `PATH` by appending the following line to `$HOME/.bashrc`:

