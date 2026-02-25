class Z13ctl < Formula
  desc "CLI and daemon for ASUS ROG Flow Z13 hardware control"
  homepage "https://github.com/dahui/z13ctl"
  url "https://github.com/dahui/z13ctl/releases/download/vVERSION_PLACEHOLDER/z13ctl_VERSION_PLACEHOLDER_linux_amd64.tar.gz"
  sha256 "SHA256_PLACEHOLDER"
  license "Apache-2.0"

  on_macos do
    disable! date: "2025-01-01", because: "z13ctl only supports Linux"
  end

  def install
    bin.install "z13ctl"
  end

  def caveats
    <<~EOS
      After installation, run setup to configure device access rules:
        sudo z13ctl setup

      Then enable the daemon for your user:
        systemctl --user enable --now z13ctl.socket z13ctl.service
    EOS
  end

  test do
    system "#{bin}/z13ctl", "--version"
  end
end
