{ pkgs, ... }:
{
  settings.processes."vm-host" = {
    command = "${pkgs.nix}/bin/nix run .#dev";
    readiness_probe = {
      exec.command = "${pkgs.netcat}/bin/nc -z localhost 2222";
      initial_delay_seconds = 2;
      period_seconds = 1;
      failure_threshold = 60;
    };
  };

  settings.processes."tunnel-give" = {
    command = "${pkgs.openssh}/bin/ssh -o StrictHostKeyChecking=no -L 9000:give:22 root@localhost -p 2222 -N";
    depends_on = {
      vm-host = {
        condition = "process_healthy";
      };
    };
    readiness_probe = {
      exec.command = "${pkgs.netcat}/bin/nc -z localhost 9000";
      initial_delay_seconds = 2;
      period_seconds = 1;
    };
    availability = {
      restart = "on_failure";
      backoff_seconds = 1;
    };
  };

  settings.processes."tunnel-take" = {
    command = "${pkgs.openssh}/bin/ssh -o StrictHostKeyChecking=no -L 9100:take:22 root@localhost -p 2222 -N";
    depends_on = {
      vm-host = {
        condition = "process_healthy";
      };
    };
    readiness_probe = {
      exec.command = "${pkgs.netcat}/bin/nc -z localhost 9100";
      initial_delay_seconds = 2;
      period_seconds = 1;
    };
    availability = {
      restart = "on_failure";
      backoff_seconds = 1;
    };
  };
}
