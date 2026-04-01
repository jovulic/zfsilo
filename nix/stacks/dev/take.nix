{
  pkgs,
  lib,
  ...
}:
{
  config = {
    networking.hostName = "take";
    users.users.root.hashedPassword = "";

    microvm = {
      hypervisor = "qemu";
      volumes = [
        {
          mountPoint = "/var";
          image = "/tmp/take-var.img";
          size = 256;
        }
      ];
      interfaces = [
        {
          type = "tap";
          id = "vm-take";
          mac = "02:00:00:00:00:03";
        }
      ];
      storeDiskType = "squashfs";
    };

    boot = {
      initrd.availableKernelModules = [
        "nvmet"
        "nvmet-tcp"
      ];
      initrd.kernelModules = [
        "nvme-tcp"
        "nvme-fabrics"
      ];
    };

    environment.systemPackages = [
      pkgs.dig
      pkgs.nvme-cli
    ];

    systemd.network.enable = true;

    networking.firewall = {
      allowedUDPPorts = [
        5355 # allow LLMNR
      ];
      allowedTCPPorts = [
        22 # allow SSH
        5355 # allow LLMNR
      ];
    };

    services.openssh = {
      enable = true;
      settings = {
        UsePAM = false;
        PermitRootLogin = "yes";
      };
      extraConfig = ''
        PermitEmptyPasswords yes
      '';
    };

    services.resolved = {
      enable = true;
    };

    services.openiscsi = {
      enable = true;
      name = "iqn.2006-01.org.linux-iscsi.take";
      extraConfig = ''
        node.startup = automatic
      '';
    };

    system.stateVersion = lib.trivial.release;
  };
}
