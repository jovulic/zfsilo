{
  nixpkgs,
  lib,
  system,
  microvm,
  ...
}:
nixpkgs.lib.nixosSystem {
  inherit system;
  modules = [
    microvm.nixosModules.microvm
    ../src/system.nix
    {
      microvm = {
        hypervisor = "qemu";
        graphics.enable = true;
        vcpu = 4;
        mem = 4096;
        interfaces = [
          {
            type = "user";
            id = "lo";
            mac = "02:00:00:00:00:01"; # "02" means locally administered address.
          }
        ];
        forwardPorts = [
          {
            from = "host";
            host.port = 8080;
            guest.port = 8080;
          }
        ];
        storeDiskType = "squashfs";
      };

      hardware.graphics.enable = true;

      networking = {
        firewall = {
          allowedTCPPorts = [
            8080
          ];
        };
      };

      users = {
        users.pilot = {
          extraGroups = [
            "wheel"
            "video"
          ];
        };
      };

      security.sudo = {
        enable = true;
        wheelNeedsPassword = false;
      };

      programs.neovim = {
        enable = true;
        defaultEditor = true;
        vimAlias = true;
        viAlias = true;
      };

      system.stateVersion = lib.trivial.release;
    }
  ];
}
