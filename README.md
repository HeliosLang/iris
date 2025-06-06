# Cardano Node installation bash scripts

Promptless Cardano Node install bash scripts, for installing cardano-node and other services on clean Linux bare metal servers without using docker.

Tested on a clean installs of: 
   - Debian 12

**!!DO NOT RUN THESE SCRIPTS ON YOUR LOCAL MACHINE!!**

## How to install

Run any of the following commands to install cardano-node along with

### Preprod `cardano-node` + `blockfrost`

```bash
curl -fsSL \
     https://github.com/HeliosLang/cardano-node-install/bloc/main/preprod-with-blockfrost.sh \
     | bash
```

### Mainnet `cardano-node` + `blockfrost`

```bash
curl -fsSL \
     https://github.com/HeliosLang/cardano-node-install/blob/main/mainnet-with-blockfrost.sh \
     | bash
```

## Contributing

Don't waste time with fancy terminal colors.
Add to `PATH` by appending the following line to `$HOME/.bashrc`:

