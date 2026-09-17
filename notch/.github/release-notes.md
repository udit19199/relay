- 修复 macOS 使用 fish 作为登录 shell 时，账号操作报“Could not read the login shell's Codex path”的问题（#10）。
- 保留登录 shell 的 PATH 与 CODEX_CLI_PATH 设置，覆盖 sh、bash、zsh、fish 的真实登录 shell 回归测试。
- Windows 使用相同版本号重新构建，运行时查找行为保持不变。

- Fix macOS account actions failing with fish as the login shell (#10).
- Preserve login-shell PATH and CODEX_CLI_PATH settings, with regression coverage for sh, bash, zsh, and fish.
- Rebuild Windows at the same version with unchanged runtime discovery behavior.

**下载 / Downloads:** 在 Assets 中选择 macOS `.dmg` 或 Windows `.exe`。SHA-256 校验文件同时提供。
