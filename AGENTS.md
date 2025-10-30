# Project: ZFSilo

This document provides context for the Gemini code assistant to understand the `zfsilo` project.

## Project Overview

`zfsilo` is a ZFS-based network storage layer over iSCSI with CSI integration. It is written in Go and uses a gRPC API for communication. The project is structured as a multi-module Go workspace, with separate modules for the API definitions (`api`), the main application (`app`), and shared libraries (`lib`).

The project uses [Nix flakes](https://nixos.wiki/wiki/Flakes) to provide a reproducible development environment.

## Development Environment

The development environment can be activated by running `nix develop` in the project root. This will provide a shell with all the necessary tools, including:

- Go
- Just
- Git
- Bash

## Building and Running

The project uses `just` as a command runner. The following commands are available:

### API

- `just build`: Generates Go code from the Protobuf definitions using `buf generate`.

### Application

- `just run`: Runs the application. This will start the `zfsilo` server with the configuration from `config.json`.
- `just build`: Builds the application binary.
- `just generate`: Runs `go generate` for the application.
- `just wire`: Runs `wire` to generate dependency injection code.

## API

The gRPC API is defined in `api/src/zfsilo/v1/zfsilo.proto`. It exposes the following services:

- `Service`: For getting storage capacity.
- `VolumeService`: For managing volumes (Create, Get, List, Update, Delete).
- `GreeterService`: A simple service for testing connectivity.

The API is documented using OpenAPI annotations within the Protobuf file.

## Code Style and Linting

The project uses `.golangci.yaml` to configure linting. The configuration is based on the `fast` preset with some additional linters enabled and some disabled. This enforces a consistent code style.
