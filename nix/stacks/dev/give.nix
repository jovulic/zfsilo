{
  config,
  pkgs,
  lib,
  ...
}:
{
  config = {
    networking.hostName = "give";
    networking.hostId = "637578f7";
    users.users.root.hashedPassword = "";

    microvm = {
      hypervisor = "qemu";
      volumes = [
        {
          mountPoint = "/var";
          image = "/tmp/give-var.img";
          size = 256;
        }
        {
          mountPoint = "/data";
          image = "/tmp/give-data.img";
          size = 512 + 64;
        }
      ];
      interfaces = [
        {
          type = "tap";
          id = "vm-give";
          mac = "02:00:00:00:00:02";
        }
      ];
      storeDiskType = "squashfs";
    };

    environment.systemPackages = [
      pkgs.dig
    ];

    systemd.network.enable = true;

    networking.firewall = {
      allowedUDPPorts = [
        5355 # allow LLMNR
      ];
      allowedTCPPorts = [
        22 # allow SSH
        5355 # allow LLMNR
        3260 # allow ISCSI
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

    boot = {
      supportedFilesystems = [ "zfs" ];
    };

    services.target = {
      enable = true;
    };

    systemd.services.setuptank = {
      enable = true;
      wantedBy = [ "multi-user.target" ];
      after = [ "getty@tty1.service" ];
      serviceConfig = {
        Type = "exec";
        ExecStart = pkgs.writeShellScript "setuptank.sh" ''
          source ${config.system.build.setEnvironment}

          set -veuo pipefail

          if ! zpool list tank -Ho name > /dev/null 2>&1; then
            dd if=/dev/zero of=/data/disk1 bs=1M count=256
            dd if=/dev/zero of=/data/disk2 bs=1M count=256
            zpool create \
              -f \
              -o ashift=12 \
              -m /tank \
              tank \
              mirror \
                /data/disk1 \
                /data/disk2
          fi
        '';
        StandardInput = "null";
        StandardOutput = "journal";
        StandardError = "inherit";
      };
    };

    system.stateVersion = lib.trivial.release;
  };
}
