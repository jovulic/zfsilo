# Project: ZFSilo

This document provides context for the Gemini code assistant to understand the `zfsilo` project.

## Project Overview

`zfsilo` is a ZFS-based network storage layer over iSCSI with CSI (Container Storage Interface) integration. It is written in Go and uses [ConnectRPC](https://connectrpc.com/) for its gRPC-compatible API. The project provides a control plane for managing ZFS datasets and exporting them as block devices or filesystems via iSCSI, primarily for Kubernetes environments.

## Repository Structure

The project is organized as a multi-module Go workspace (`go.work`).

### `api/`
Defines the public API schema and contains generated code.
- **Source**: `api/src/zfsilo/v1/zfsilo.proto` defines `Service` (general info) and `VolumeService` (lifecycle).
- **Generation**: Uses `buf` to generate Go code and OpenAPI v3 specs.
- **Communication**: Uses ConnectRPC for all internal and external communication.

### `app/`
The main `zfsilo` server (Target side). It runs on the storage node.
- **Internal Architecture** (`app/internal/`):
    - **`command/`**: Typed wrappers around system CLIs (`zfs`, `zpool`, `iscsiadm`, `targetcli`, `mount`, `fs`). This is the bridge to the OS.
    - **`service/`**: Implements the ConnectRPC interfaces. Handles complex workflows (e.g., volume creation with iSCSI target export).
    - **`database/`**: GORM-based persistence (SQLite/JSON) for tracking volume state and metadata.
    - **`converter/`**: Translates between Protobuf API messages and internal database models.
- **Build Tags**: Uses `json1` for SQLite JSON support.

### `csi/`
Implementation of the Container Storage Interface (CSI) driver.
- **Role**: Acts as a bridge between Kubernetes (or any CSI consumer) and the `zfsilo` app.
- **Implementation**: Implements `Identity`, `Controller`, and `Node` services.
- **Node Service**: Handles local iSCSI login/logout and mounting on the client node.
- **Controller Service**: Handles volume provisioning and iSCSI target mapping by calling `app`.

### `lib/`
Shared Go library packages.
- **`command/`**: Abstraction for executing shell commands, facilitating testing/mocking.
- **`try/`**: A "transaction-like" utility for reversible system operations. Ensures that if a multi-step mutation (e.g., "Create ZFS" -> "Export iSCSI") fails mid-way, previous steps are undone.
- **`selfcert/`**: Helpers for generating self-signed certificates for TLS.
- **`tagged/`, `genericutil/`, `stringutil/`, `structutil/`**: General Go helpers.

### `nix/stacks/dev/`
Reproducible development and testing environment using **MicroVMs**.
- **`give` VM**: Acts as the storage server (runs `zfsilo app`, ZFS, iSCSI target).
- **`take` VM**: Acts as the client/initiator (runs `zfsilo csi`, `openiscsi`).
- **Orchestration**: Managed via Nix Flakes and `nix run .#dev-stack`.

## Key Architecture Concepts

1.  **CLI Wrapping**: Instead of C bindings, it manages storage by invoking standard tools (`zfs`, `targetcli`).
2.  **Reversibility**: Uses `lib/try` to maintain consistency across system-level mutations.
3.  **Volume Lifecycle**: Volumes progress through a state machine: `INITIAL` -> `PUBLISHED` (Target ready) -> `CONNECTED` (Initiator logged in) -> `MOUNTED` (FS available).
4.  **ConnectRPC**: Modern, simplified gRPC implementation that works over HTTP/1.1 and HTTP/2.
5.  **Authentication**: Uses Bearer tokens verified via Unary Interceptors in both `app` and `csi`.

## Development & Tooling

The project uses `just` as a command runner and Nix for the environment.
- **`nix develop`**: Enter the dev shell with all dependencies (Go 1.24+, Just, etc.).
- **`just dev`**: Launch the MicroVM dev stack.
- **`just build`**: Compile all binaries.
- **`just test`**: Run all tests.
- **`just app` / `just csi`**: Run the components locally (requires configuration).
- **Dependency Injection**: Uses [Google Wire](https://github.com/google/wire) for wiring components.

## API Services
- **`Service`**:
    - `GetCapacity`: Returns free space in the ZFS pool.
- **`VolumeService`**:
    - `GetVolume`/`ListVolumes`: Query volume metadata and status.
    - `CreateVolume`/`UpdateVolume`/`DeleteVolume`: Manage ZFS datasets and persistence.
    - `PublishVolume`/`UnpublishVolume`: Manage iSCSI Target configuration.
    - `ConnectVolume`/`DisconnectVolume`: Manage iSCSI Initiator login/logout.
    - `MountVolume`/`UnmountVolume`: Manage local filesystem mounting.
    - `StatsVolume`: Get filesystem usage (total/used/available).
    - `SyncVolume`/`SyncVolumes`: Reconcile actual system state with the database.

## Code Style & Conventions
- **Linting**: Enforced via `golangci-lint` (see `.golangci.yaml`).
- **Formatting**: Standard `go fmt`.
- **Persistence**: Database models are in `app/internal/database`.
- **Protobuf**: Managed via `buf`. All proto definitions are in `api/src`.
