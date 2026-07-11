import assert from 'node:assert/strict';

import {
  buildInstallCommand,
  DEFAULT_INSTALL_COMMAND_CONFIG,
  DEFAULT_INSTALL_REASONING_EFFORT,
  INSTALL_MODELS,
  INSTALL_REASONING_EFFORTS,
  normalizeInstallCommandConfig,
  normalizeCodexBaseUrl,
  normalizeInstallReasoningEffort,
} from '../src/pages/Install/installCommandBuilder.js';

const platforms = ['linux', 'macos', 'windows'];
const goodMarkers = [
  'approval_policy = "never"',
  'sandbox_mode = "danger-full-access"',
  'wire_api = "responses"',
  'supports_websockets = false',
  'model_reasoning_effort = "xhigh"',
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
  '"api_key"',
  'api_key =',
];

assert.equal(
  normalizeCodexBaseUrl('https://api.opencodex.uk'),
  'https://api.opencodex.uk/v1',
);
assert.equal(
  normalizeCodexBaseUrl('https://api.opencodex.uk/v1/'),
  'https://api.opencodex.uk/v1',
);
assert.deepEqual(INSTALL_MODELS, ['gpt-5.5', 'gpt-5.6-sol']);
assert.equal(DEFAULT_INSTALL_COMMAND_CONFIG.defaultModel, 'gpt-5.6-sol');
assert.equal(DEFAULT_INSTALL_REASONING_EFFORT, 'xhigh');
assert.deepEqual(
  INSTALL_REASONING_EFFORTS.map((item) => [item.id, item.label]),
  [
    ['xhigh', '超高'],
    ['max', '最高'],
  ],
);
assert.equal(normalizeInstallReasoningEffort('xhigh'), 'xhigh');
assert.equal(normalizeInstallReasoningEffort('invalid'), 'xhigh');

for (const platform of platforms) {
  const command = buildInstallCommand(
    platform,
    'DUMMY_KEY',
    'https://api.opencodex.uk',
    'gpt-5.6-sol',
    'xhigh',
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
  assert(
    command.includes('opencodex-workspace') &&
      command.includes('[projects.') &&
      command.includes('trust_level = "trusted"'),
    `${platform} command must create and trust the per-user workspace`,
  );
  assert(
    command.includes('env_key = "CODEX_API_KEY"') ||
      command.includes("env_key='CODEX_API_KEY'"),
    `${platform} command must configure provider auth through env_key`,
  );
}

const linuxCommand = buildInstallCommand(
  'linux',
  'DUMMY_KEY',
  'https://api.opencodex.uk',
  'gpt-5.6-sol',
  'xhigh',
);
assert(
  linuxCommand.includes('install_node') &&
    linuxCommand.includes('install_codex') &&
    linuxCommand.includes('NODE_LTS_MAJOR=24') &&
    linuxCommand.includes('cd "$WORK_DIR" && "$CODEX_BIN"'),
  'linux command must use the canonical OpenCodex install flow',
);

const macosCommand = buildInstallCommand(
  'macos',
  'DUMMY_KEY',
  'https://api.opencodex.uk',
  'gpt-5.6-sol',
  'xhigh',
);
assert(
  macosCommand.includes('WORK_DIR="$HOME/opencodex-workspace"') &&
    macosCommand.includes('cd "$WORK_DIR" && codex'),
  'macos command must launch Codex from the per-user workspace',
);

const windowsCommand = buildInstallCommand(
  'windows',
  'DUMMY_KEY',
  'https://api.opencodex.uk',
  'gpt-5.6-sol',
  'xhigh',
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

for (const platform of platforms) {
  for (const effort of ['xhigh', 'max']) {
    const command = buildInstallCommand(
      platform,
      'DUMMY_KEY',
      'https://api.opencodex.uk',
      'gpt-5.6-sol',
      effort,
    );
    const configMarker =
      platform === 'windows'
        ? `model_reasoning_effort='${effort}'`
        : `"model_reasoning_effort":"${effort}"`;
    assert(
      command.includes(configMarker) &&
        command.includes(`model_reasoning_effort = "${effort}"`),
      `${platform} command must use selected reasoning effort ${effort}`,
    );
  }
}

const customConfig = normalizeInstallCommandConfig({
  approval_policy: 'on-request',
  sandbox_mode: 'workspace-write',
  supports_websockets: true,
  workspace_name: 'workspace-a',
  models: ['gpt-5.5', 'gpt-5.6-sol'],
  default_model: 'gpt-5.5',
});

const customWindowsCommand = buildInstallCommand(
  'windows',
  'DUMMY_KEY',
  'https://api.opencodex.uk',
  'gpt-5.5',
  'xhigh',
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
assert.equal(fallbackConfig.defaultModel, 'gpt-5.6-sol');

console.log('install command builder checks passed');
