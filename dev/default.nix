{
  pkgs,
  nixpkgs,
  system,
  microvm,
  ...
}:
{
  host = pkgs.callPackage ./host.nix {
    inherit nixpkgs;
    inherit system;
    inherit microvm;
  };
}
