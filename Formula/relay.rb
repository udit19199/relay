class Relay < Formula
  desc "Keep your coding agent working when an AI plan hits its limit"
  homepage "https://github.com/udit19199/relay"
  # The v8.0.x tags are upstream CLIProxyAPI's; Relay's own commits live on main,
  # so the formula pins a commit. Move to a release tag once Relay cuts one.
  url "https://github.com/udit19199/relay/archive/5f79aaea5f10619b5fca90fe386c6adeadbe24e8.tar.gz"
  version "5f79aaea"
  sha256 "cae9266f8672c2604d8c497036b2fdb8b824681c8673b2f90d85ff29243103f6"
  license "MIT"
  head "https://github.com/udit19199/relay.git", branch: "main"

  depends_on "go" => :build
  depends_on :macos

  def install
    ldflags = "-X main.Version=#{version} -X main.BuildDate=#{time.iso8601}"
    system "go", "build", "-buildvcs=false",
           *std_go_args(ldflags: ldflags, output: bin/"relay"), "./cmd/server"

    pkgshare.install "config.example.yaml"
    doc.install "README.md"
  end

  def caveats
    <<~EOS
      Relay keeps its files in ~/.relay. Set it up once:
        relay setup
      That writes the config and both keys, installs the login service
      com.relay.proxy, starts Relay on 127.0.0.1:8317, and prints the
      environment lines for Claude Code and OpenAI clients.

      If you already run Relay from a manual build, stop that one first:
        launchctl bootout gui/$(id -u)/com.relay.proxy

      The notch app is a separate Swift build; this formula installs the
      proxy and its relay command line only.
    EOS
  end

  test do
    assert_match "Relay Version: #{version}", shell_output("#{bin}/relay --help")
    assert_match "relay setup", shell_output("#{bin}/relay help")
  end
end
