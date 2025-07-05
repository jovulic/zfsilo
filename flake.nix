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
    in
    {
      devShells = utils.eachSystem (
        { pkgs, ... }:
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.git
              pkgs.bash
            ];
          };
        }
      );
      packages = utils.eachSystem (
        { pkgs, ... }:
        {
          dev =
            (pkgs.callPackage ./dev/flake.nix {
              nixpkgs = inputs.nixpkgs;
              microvm = inputs.microvm;
            }).config.microvm.declaredRunner;
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
        { pkgs, ... }:
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
          dev = createApp ''
            nix run .#nixosConfigurations.dev.host.config.microvm.declaredRunner
          '';
        }
      );
    };
}
