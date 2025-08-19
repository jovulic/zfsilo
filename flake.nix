{
  description = "A ZFS-based network storage layer over iSCSI with CSI integration.";

  inputs = {
    nixpkgs.url = "nixpkgs/nixos-25.05";
    microvm = {
      url = "github:astro/microvm.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    { ... }@inputs:
    let
      inherit (inputs) nixpkgs;
      utils = import ./utils.nix { inherit nixpkgs; };
      version = nixpkgs.lib.strings.removeSuffix "\n" (builtins.readFile ./version.txt);
      commitHashShort =
        if (builtins.hasAttr "shortRev" inputs.self) then
          inputs.self.shortRev
        else
          inputs.self.dirtyShortRev;
    in
    {
      devShells = utils.eachSystem (
        { pkgs, ... }:
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.git
              pkgs.bash
              pkgs.just
              pkgs.go
            ]
            ++ (pkgs.callPackage ./api {
              inherit version commitHashShort;
            }).shell.packages
            ++ (pkgs.callPackage ./app {
              inherit version commitHashShort;
            }).shell.packages;
          };
        }
      );
      packages = utils.eachSystem (
        { pkgs, ... }:
        {
          api =
            (pkgs.callPackage ./api {
              inherit version commitHashShort;
            }).package;
          app =
            (pkgs.callPackage ./app {
              inherit version commitHashShort;
            }).package;
        }
      );
      nixosConfigurations =
        let
          system = "x86_64-linux";
          pkgs = import nixpkgs {
            inherit system;
            config.allowUnfree = true;
          };
          dev = pkgs.callPackage ./dev {
            inherit nixpkgs;
            inherit system;
            microvm = inputs.microvm;
          };
        in
        {
          inherit dev;
        };
      apps = utils.eachSystem (
        { pkgs, system, ... }:
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
          dev = createApp ''
            nix run .#nixosConfigurations.dev.host.config.microvm.declaredRunner
          '';
        }
      );
    };
}
