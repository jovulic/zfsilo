{
  nixpkgs,
  system,
  microvm,
  ...
}:
nixpkgs.lib.nixosSystem {
  inherit system;
  modules = [
    microvm.nixosModules.host
    microvm.nixosModules.microvm
    (
      {
        config,
        pkgs,
        lib,
        ...
      }:
      {
        networking.hostName = "host";
        users.users.root.hashedPassword = "";
        services.getty.helpLine = ''
          Login as "root" with an empty password.
          You can ssh to nested machines by hostname.
          Type ctrl-a h to switch to the qemu console.
        '';
        services.getty.autologinUser = "root";

        microvm = {
          hypervisor = "qemu";
          vcpu = 2;
          mem = 8192;
          interfaces = [
            {
              id = "qumu";
              type = "user";
              mac = "02:00:00:00:00:01";
            }
          ];
          forwardPorts = [
            {
              host.port = 2222;
              guest.port = 22;
            }
          ];
          # You can get vm logs from the host with something like the following:
          # `journalctl -u microvm@take.service -f`
          vms = {
            give.config = (pkgs.callPackage ./give.nix { inherit config; }).config;
            take.config = (pkgs.callPackage ./take.nix { }).config;
          };
          storeDiskType = "squashfs";
        };

        environment.systemPackages = with pkgs; [
          dig
        ];

        systemd.network = {
          enable = true;
          netdevs.virbr0.netdevConfig = {
            Kind = "bridge";
            Name = "virbr0";
          };
          networks.virbr0 = {
            matchConfig.Name = "virbr0";
            # Hand out IP addresses to MicroVMs.
            # Use `networkctl status virbr0` to see leases.
            networkConfig = {
              DHCPServer = true;
              IPv6SendRA = true;
            };
            addresses = [
              {
                Address = "10.0.0.1/24";
              }
              {
                Address = "fd12:3456:789a::1/64";
              }
            ];
            ipv6Prefixes = [
              {
                Prefix = "fd12:3456:789a::/64";
              }
            ];
          };
          networks.microvm-eth0 = {
            matchConfig.Name = "vm-*";
            networkConfig.Bridge = "virbr0";
          };
        };

        networking.firewall = {
          allowedUDPPorts = [
            67 # allow DHCP sever
            5355 # allow LLMNR
          ];
          allowedTCPPorts = [
            22 # allow SSH
            5355 # allow LLMNR
          ];
        };

        # Enable local link hostname resolution via LLMNR.
        services.resolved = {
          enable = true;
        };

        # Enable ssh into the host from the real host.
        # You can see the logs with the following commmand.
        # `journalctl -u sshd -f`
        # You can use the following to get the current configuration of sshd.
        # `sshd -T`
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

        programs.ssh = {
          extraConfig = ''
            Host *
              StrictHostKeyChecking no
          '';
        };

        system.stateVersion = lib.trivial.release;
      }
    )
  ];
}
