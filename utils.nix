{ nixpkgs, ... }:
{
  eachSystem =
    systemsOrF:
    if builtins.isList systemsOrF then
      f:
      nixpkgs.lib.genAttrs systemsOrF (
        system:
        f {
          pkgs = import nixpkgs {
            inherit system;
            config.allowUnfree = true;
          };
          inherit system;
        }
      )
    else
      let
        defaultSystems = [
          "aarch64-linux"
          "aarch64-darwin"
          "x86_64-darwin"
          "x86_64-linux"
        ];
      in
      nixpkgs.lib.genAttrs defaultSystems (
        system:
        systemsOrF {
          pkgs = import nixpkgs {
            inherit system;
            config.allowUnfree = true;
          };
          inherit system;
        }
      );
}
