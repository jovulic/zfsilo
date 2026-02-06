# Project: ZFSilo

This document provides context for the Gemini code assistant to understand the `zfsilo` project.

## Project Overview

`zfsilo` is a ZFS-based network storage layer over iSCSI with CSI integration. It is written in Go and uses a gRPC API for communication. The project aims to provide a robust control plane for managing ZFS datasets and exporting them as block devices via iSCSI, primarily for Kubernetes environments.

## Repository Structure

The project is organized as a multi-module Go workspace.

### `api/`
Contains the schema and generated code for the public API.
- **Source**: `api/src/zfsilo/v1/zfsilo.proto` defines the gRPC services (`VolumeService`, `Service`).
- **Generation**: Uses `buf` to generate Go code and OpenAPI specs.

### `app/`
Contains the main `zfsilo` server application.
- **Entry Points**: `main.go` / `main_app.go` setup the application lifecycle.
- **Internal Architecture** (`app/internal/`):
    - **`command/`**: Typed wrappers around system CLIs (`zfs`, `iscsiadm`, `mount`, `fs`). This is the bridge to the OS.
    - **`service/`**: Core business logic implementing the gRPC interfaces. Handles complex workflows like volume creation.
    - **`database/`**: GORM-based database models (e.g., `Volume`) for tracking state.
    - **`converter/`**: Translates between Protobuf API messages and internal database models.

### `csi/`
Reserved for the Container Storage Interface (CSI) driver implementation. *Currently empty.*

### `dev/`
Contains configuration for a reproducible development and testing environment using **MicroVMs**.
- **`give.nix`**: Defines the "Server" VM (`give`). It runs ZFS, creates a pool named `tank` on startup, and acts as the iSCSI target.
- **`take.nix`**: Defines the "Client" VM (`take`). It runs `openiscsi` to consume volumes exported by `give`.
- **`host.nix`**: Host-specific configuration.

### `lib/`
Shared Go library packages used by `app` and potentially `csi`.
- **`command/`**: A command execution abstraction to simplify testing and mocking of shell commands.
- **`try/`**: A utility for handling reversible operations (transactions), essential for robustly handling multi-step system mutations (e.g., "Create ZFS dataset" -> "Fail" -> "Rollback ZFS dataset").
- **`genericutil/`, `stringutil/`, `structutil/`**: General helpers.

## Key Architecture Concepts

1.  **CLI Wrapping**: The application manages storage by invoking standard CLI tools (`zfs`, `iscsiadm`) rather than using C bindings. This is handled in `app/internal/command`.
2.  **Reversibility**: Critical operations use the `lib/try` package to ensure that if a step fails (e.g., database write fails after ZFS creation), previous steps are undone (ZFS dataset is destroyed) to prevent inconsistent state.
3.  **Development Flow**: Developers use the `give` (server) and `take` (client) MicroVMs to test the full storage lifecycle in an isolated environment that mirrors production ZFS/iSCSI setups.

## Development Environment

The development environment is managed via Nix Flakes.
- **`nix develop`**: Drops you into a shell with Go, Just, Git, and other dependencies.

## Building and Running

The project uses `just` as a command runner.

### API
- `just build`: Generates Go code from Protobuf definitions (`buf generate`).

### Application
- `just run`: Runs the application locally (requires valid config).
- `just build`: Compiles the binary.
- `just generate`: Runs `go generate`.
- `just wire`: Generates dependency injection code using Google Wire.

## API Services
Defined in `api/src/zfsilo/v1/zfsilo.proto`:
- **`Service`**: General server info (e.g., Capacity).
- **`VolumeService`**: Full lifecycle management:
    - `Create`/`Delete`: Manage ZFS datasets and DB entries.
    - `Publish`: Export a volume via iSCSI (Target).
    - `Connect`: Connect to an iSCSI target (Initiator).
    - `Mount`: Mount a connected device to a local path.

## Code Style
- **Linting**: Enforced via `.golangci.yaml`.
- **Formatting**: Standard Go formatting.