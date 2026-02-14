{
  description = "A ZFS-based network storage layer over iSCSI with CSI integration.";

  inputs = {
    nixpkgs.url = "nixpkgs/nixos-25.11";
    microvm = {
      url = "github:astro/microvm.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
    };
    process-compose-flake = {
      url = "github:Platonic-Systems/process-compose-flake";
    };
    services-flake = {
      url = "github:juspay/services-flake";
    };
  };

  outputs =
    inputs@{
      self,
      nixpkgs,
      flake-parts,
      ...
    }:
    let
      version = nixpkgs.lib.strings.removeSuffix "\n" (builtins.readFile ./version.txt);
      commitHashShort = if (builtins.hasAttr "shortRev" self) then self.shortRev else self.dirtyShortRev;
    in
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
        "x86_64-darwin"
      ];
      imports = [
        inputs.process-compose-flake.flakeModule
      ];
      perSystem =
        {
          # config,
          # self',
          # inputs',
          system,
          pkgs,
          ...
        }:
        {
          _module.args.pkgs = import nixpkgs {
            inherit system;
            config.allowUnfree = true;
          };
          devShells.default = pkgs.mkShell {
            packages = [
              pkgs.git
              pkgs.bash
              pkgs.just
              pkgs.go
              pkgs.toybox
              pkgs.openssh
            ]
            ++ (pkgs.callPackage ./api {
              inherit version commitHashShort;
            }).shell.packages
            ++ (pkgs.callPackage ./app {
              inherit version commitHashShort;
            }).shell.packages
            ++ (pkgs.callPackage ./csi {
              inherit version commitHashShort;
            }).shell.packages;
          };
          packages = {
            api =
              (pkgs.callPackage ./api {
                inherit version commitHashShort;
              }).package;
            app =
              (pkgs.callPackage ./app {
                inherit version commitHashShort;
              }).package;
            csi =
              (pkgs.callPackage ./csi {
                inherit version commitHashShort;
              }).package;
          };
          apps =
            let
              createApp = text: {
                type = "app";
                program = "${
                  pkgs.writeShellApplication {
                    name = "script";
                    inherit text;
                  }
                }/bin/script";
              };
            in
            {
              app = createApp ''
                # shellcheck disable=SC2068
                nix run .#packages.${system}.app -- $@
              '';
              csi = createApp ''
                # shellcheck disable=SC2068
                nix run .#packages.${system}.csi -- $@
              '';
              dev = createApp ''
                nix run .#nixosConfigurations.dev.host.config.microvm.declaredRunner
              '';
            };
          process-compose."dev-stack" = {
            imports = [
              inputs.services-flake.processComposeModules.default
              ./nix/stacks/dev
            ];
          };
        };
      flake = {
        nixosConfigurations =
          let
            system = "x86_64-linux";
            pkgs = import nixpkgs {
              inherit system;
              config.allowUnfree = true;
            };
            dev = pkgs.callPackage ./nix/stacks/dev/cluster.nix {
              inherit nixpkgs;
              inherit system;
              microvm = inputs.microvm;
            };
          in
          {
            inherit dev;
          };
      };
    };
}
