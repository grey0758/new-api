import assert from 'node:assert/strict';

import {
  buildInstallCommand,
  normalizeInstallCommandConfig,
  normalizeCodexBaseUrl,
} from '../src/pages/Install/installCommandBuilder.js';

const platforms = ['linux', 'macos', 'windows'];
const goodMarkers = [
  'approval_policy = "never"',
  'sandbox_mode = "danger-full-access"',
  'wire_api = "responses"',
  'supports_websockets = false',
];
const badMarkers = [
  'approval_policy = "on-request"',
  'approvals_reviewer = "user"',
  'approvals_reviewer = "auto_review"',
  'sandbox_mode = "workspace-write"',
  '--dangerously-bypass-approvals-and-sandbox',
  '[windows]',
  'sandbox = "elevated"',
  'C:\\WINDOWS\\system32',
  'D:/work/wc_project',
  'C:/Users/Admin',
];

assert.equal(
  normalizeCodexBaseUrl('https://api.opencodex.uk'),
  'https://api.opencodex.uk/v1',
);
assert.equal(
  normalizeCodexBaseUrl('https://api.opencodex.uk/v1/'),
  'https://api.opencodex.uk/v1',
);

for (const platform of platforms) {
  const command = buildInstallCommand(
    platform,
    'DUMMY_KEY',
    'https://api.opencodex.uk',
    'gpt-5.5',
  );

  for (const marker of goodMarkers) {
    assert(
      command.includes(marker),
      `${platform} command is missing ${marker}`,
    );
  }

  for (const marker of badMarkers) {
    assert(
      !command.includes(marker),
      `${platform} command still contains ${marker}`,
    );
  }

  assert(
    command.includes('https://api.opencodex.uk/v1'),
    `${platform} command did not normalize base URL to /v1`,
  );
}

const linuxCommand = buildInstallCommand(
  'linux',
  'DUMMY_KEY',
  'https://api.opencodex.uk',
  'gpt-5.5',
);
assert(
  linuxCommand.includes('install_node') &&
    linuxCommand.includes('install_codex') &&
    linuxCommand.includes('NODE_LTS_MAJOR=24'),
  'linux command must use the canonical OpenCodex install flow',
);

const windowsCommand = buildInstallCommand(
  'windows',
  'DUMMY_KEY',
  'https://api.opencodex.uk',
  'gpt-5.5',
);
assert(
  windowsCommand.includes('opencodex-workspace') &&
    windowsCommand.includes('Set-Location $workDir; & $codexCmd.Source'),
  'windows command must launch Codex from the per-user workspace',
);
assert(
  windowsCommand.includes(
    String.raw`('[projects."' + $workDir.Replace('\','/') + '"]')`,
  ),
  'windows command must emit the TOML project path with forward slashes',
);
assert(
  !windowsCommand.includes(String.raw`$workDir.Replace('\\','\\\\')`),
  'windows command must not try to escape Windows backslashes in the generated TOML project key',
);
assert(
  windowsCommand.includes('Invoke-CodexNpmInstall') ||
    windowsCommand.includes('optionalDependencies.$nativeName') ||
    windowsCommand.includes('codex-win32-x64'),
  'windows command must include the canonical native optional dependency repair flow',
);

const customConfig = normalizeInstallCommandConfig({
  approval_policy: 'on-request',
  sandbox_mode: 'workspace-write',
  supports_websockets: true,
  workspace_name: 'workspace-a',
  models: ['gpt-5.4', 'gpt-5.5'],
  default_model: 'gpt-5.4',
});

const customWindowsCommand = buildInstallCommand(
  'windows',
  'DUMMY_KEY',
  'https://api.opencodex.uk',
  'gpt-5.5',
  customConfig,
);

assert(customWindowsCommand.includes('approval_policy = "on-request"'));
assert(customWindowsCommand.includes('sandbox_mode = "workspace-write"'));
assert(customWindowsCommand.includes('workspace-a'));
assert(customWindowsCommand.includes('supports_websockets = true'));

const fallbackConfig = normalizeInstallCommandConfig({
  approval_policy: 'admin-needed',
  sandbox_mode: 'system-wide',
  workspace_name: '../../bad',
  models: ['   ', '!!!'],
  default_model: 'bad-model',
});
assert.equal(fallbackConfig.approvalPolicy, 'never');
assert.equal(fallbackConfig.sandboxMode, 'danger-full-access');
assert.equal(fallbackConfig.workspaceName, 'opencodex-workspace');
assert.equal(fallbackConfig.defaultModel, 'gpt-5.5');

console.log('install command builder checks passed');
