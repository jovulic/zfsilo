[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://opensource.org/licenses/MIT)

# ZFSilo

_A ZFS-based network storage layer over iSCSI with CSI integration._

## 📌 Description

ZFSilo is a high-performance control plane for managing ZFS datasets and exporting them as block devices or filesystems via iSCSI. Designed primarily for Kubernetes environments, it provides a robust and reversible storage layer that bridges the gap between ZFS's advanced data management and network-accessible storage.

The project is split into two primary components:

- **`app`**: The storage node agent. It runs on the ZFS-enabled server, managing datasets and configuring the iSCSI target.
- **`csi`**: The Container Storage Interface driver. It facilitates volume provisioning and manages iSCSI initiator connections on client nodes.

Built with [Go](https://go.dev/) and [ConnectRPC](https://connectrpc.com/) and leveraging [Nix](https://nixos.org/) for a fully reproducible development and testing environment, including a MicroVM-based dev stack that mirrors production storage configurations.

## 🚀 Usage

ZFSilo requires both the storage agent (`app`) and the CSI driver (`csi`) to be configured and running.

### Running the App

The `app` component manages the ZFS pool and iSCSI target. To start it with a local configuration:

```bash
nix run .#app -- start --config=./app/config.json
```

Or using `just`:

```bash
just app
```

### Running the CSI Driver

The `csi` component acts as the bridge for Kubernetes. To start it:

```bash
nix run .#csi -- start --config=./csi/config.json
```

Or using `just`:

```bash
just csi
```

### Configuration

Both components use JSON configuration files to define service addresses, authentication tokens, and storage parameters. See the example `config.json` files in the `app/` and `csi/` directories for details on available options.

## 🛠️ Build

ZFSilo uses [Nix](https://nixos.org/) for dependency management and [just](https://github.com/casey/just) as a command runner.

### Environment

Enter the reproducible development shell to access all required tools (Go, Buf, Wire, etc.):

```bash
nix develop
```

### Common Commands

Once inside the development shell, you can use `just` to perform common tasks:

- **Build all components**: `just build`
- **Run tests**: `just test`
- **Lint the codebase**: `just lint`
- **Launch the Dev Stack**: `just dev` (Starts the `give` and `take` MicroVMs for end-to-end testing)

### Architecture

The project is organized as a Go workspace:

- `api/`: Protobuf definitions and generated ConnectRPC code.
- `app/`: Storage agent implementation.
- `csi/`: CSI driver implementation.
- `lib/`: Shared libraries for command execution, reversibility, and utilities.
- `nix/`: Nix Flake configurations and MicroVM stack definitions.
