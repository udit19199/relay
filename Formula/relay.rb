class Relay < Formula
  desc "Keep your coding agent working when an AI plan hits its limit"
  homepage "https://github.com/udit19199/relay"
  # The v8.0.x tags are upstream CLIProxyAPI's; Relay's own commits live on main,
  # so the formula pins a commit and carries a release-style version until Relay
  # cuts its own tag.
  url "https://github.com/udit19199/relay/archive/9261d1a2e102a8f12d1fdb5c5d78394ddf05b958.tar.gz"
  version "0.9.0"
  sha256 "2111614764ecca4d466c7a0be8bb4417c3f6487c2556d48487334ce4a71c49cf"
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
