job "windows" {
  group "vm" {
    count = 2
    restart {
      attempts = 0
    }
    update {
      max_parallel      = 1
      min_healthy_time  = "30s"
      healthy_deadline  = "15m"
      progress_deadline = "30m"
    }
    task "win" {
      driver = "qemu"
      config {
        image_path   = "http://<server.domain>/<version>/windows-server-2025.qcow2"
        emulator     = "qemu-system-custom"
        machine_type = "q35"
        accelerator  = "kvm"
        args = [
          "-mem-min", "4096", # Enable dynamic memory between this and max memory configured in "resources" block
          "-smp", "8",
          "-vlan", "1002"
        ]
        graceful_shutdown = true
        guest_agent = true
      }
      kill_timeout = "5m"
      resources {
        cpu    = 2000  # Reserve 2 CPUs for VM, total CPU cores available for VM is set with "-smp" flag
        memory = 17408 # 16 GB + 1 GB for qemu-system-custom
      }
      template {
        data        = <<-EOF
<?xml version='1.0' encoding='utf-8'?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
	<settings pass="oobeSystem">
		<component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
			<OOBE>
				<SkipMachineOOBE>true</SkipMachineOOBE>
				<HideEULAPage>true</HideEULAPage>
				<SkipUserOOBE>true</SkipUserOOBE>
				<ProtectYourPC>1</ProtectYourPC>
			</OOBE>
			<AutoLogon>
				<Enabled>true</Enabled>
				<Username>administrator</Username>
				<LogonCount>1</LogonCount>
				<Password>
					<Value>P@ssw0rd!</Value>
					<PlainText>true</PlainText>
				</Password>
				<Domain>.</Domain>
			</AutoLogon>
			<UserAccounts>
				<AdministratorPassword>
					<Value>P@ssw0rd!</Value>
					<PlainText>true</PlainText>
				</AdministratorPassword>
			</UserAccounts>
		</component>
	</settings>
</unattend>
 EOF
        destination = "local/config-drive/unattend.xml"
      }
      service {
        name     = "windows-vm-qemu-agent"
        provider = "nomad"
        address  = "127.0.0.1"
        port     = "qemu_guest_agent"
        check {
          name     = "ping"
          type     = "http"
          path     = "/qga/${NOMAD_ALLOC_ID}/win/guest-ping"
          interval = "1m"
          timeout  = "1s"
        }
      }
    }
    network {
      port "qemu_guest_agent" {}
    }
  }
}