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
      containerRegistry = "ghcr.io/jovulic";
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
        let
          api = pkgs.callPackage ./api {
            inherit version commitHashShort;
          };
          app = pkgs.callPackage ./app {
            inherit version commitHashShort;
          };
          csi = pkgs.callPackage ./csi {
            inherit version commitHashShort;
          };
        in
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
              pkgs.podman
            ]
            ++ api.shell.packages
            ++ app.shell.packages
            ++ csi.shell.packages;
          };
          packages = {
            api = api.package;
            app = app.packages.binary;
            appimg = app.packages.image;
            csi = csi.packages.binary;
            csiimg = csi.packages.image;
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
              imageBuild =
                pkg: name:
                let
                  localImage = "localhost/${name}:${version}-${commitHashShort}";
                  remoteImage = "${containerRegistry}/${name}:${version}-${commitHashShort}";
                  remoteImageShort = "${containerRegistry}/${name}:${version}";
                in
                createApp ''
                  podman load < "$(nix build .#packages.${system}.${pkg} --print-out-paths)"
                  podman tag "${localImage}" "${remoteImage}"
                  podman tag "${localImage}" "${remoteImageShort}"
                '';
              imagePush =
                name:
                let
                  remoteImage = "${containerRegistry}/${name}:${version}-${commitHashShort}";
                  remoteImageShort = "${containerRegistry}/${name}:${version}";
                in
                createApp ''
                  podman push "${remoteImage}"
                  podman push "${remoteImageShort}"
                '';
            in
            {
              app = createApp ''
                # shellcheck disable=SC2068
                nix run .#packages.${system}.app -- $@
              '';
              appimg = createApp ''
                podman load < "$(nix build .#packages.${system}.appimg --print-out-paths)"
                # shellcheck disable=SC2068
                podman run --rm --network=host -it "localhost/zfsilo:${version}-${commitHashShort}"
              '';
              appImageBuild = imageBuild "appimg" "zfsilo";
              appImagePush = imagePush "zfsilo";
              csi = createApp ''
                # shellcheck disable=SC2068
                nix run .#packages.${system}.csi -- $@
              '';
              csiimg = createApp ''
                podman load < "$(nix build .#packages.${system}.csiimg --print-out-paths)"
                # shellcheck disable=SC2068
                podman run --rm --network=host -it "localhost/zfsilo:${version}-${commitHashShort}"
              '';
              csiImageBuild = imageBuild "csiimg" "zfsilo";
              csiImagePush = imagePush "zfsilo";
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
