#!/usr/bin/env python3
"""Execute generated installers with synthetic credentials and an offline installer.
The shell/config roundtrip is real; package/network acceptance runs separately.
"""
import json, os, pathlib, subprocess, tempfile, tomllib
ROOT = pathlib.Path(__file__).resolve().parents[1]
EMIT = '''import { buildInstallCommand } from './src/pages/Install/installCommandBuilder.js';
let s=''; for await (const c of process.stdin) s+=c;
const x=JSON.parse(s); process.stdout.write(buildInstallCommand(x.os,x.key,x.base,'gpt-5.6-sol','xhigh',{},x.source));'''
fixture = '''#!/bin/sh
set -eu
if [ "${FAIL_INSTALL:-0}" = 1 ]; then exit 19; fi
mkdir -p "$CODEX_INSTALL_DIR"
printf '#!/bin/sh\\nif [ "${1:-}" = login ]; then cat >/dev/null; printf "fixture-login" > "$CODEX_HOME/auth.json"; exit 0; fi\\nif [ "${1:-}" = app-server ]; then printf "restarted" > "$CODEX_HOME/daemon-test"; exit 0; fi\\nprintf "codex-cli fixture\\\\n"\\n' > "$CODEX_INSTALL_DIR/codex"
chmod +x "$CODEX_INSTALL_DIR/codex"
'''
cases=0
with tempfile.TemporaryDirectory(prefix='unix-installer-test-') as td:
 root=pathlib.Path(td); (root/'installer.sh').write_text(fixture)
 bindir=root/'bin';bindir.mkdir()
 (bindir/'curl').write_text('#!/bin/sh\nwhile [ "$#" -gt 0 ]; do if [ "$1" = -o ]; then cp "$INSTALL_FIXTURE" "$2"; exit; fi; shift; done; exit 1\n')
 (bindir/'curl').chmod(0o700)
 for platform in ['linux','macos']:
  for source in ['r2','official']:
   home=root/(platform+'-'+source+' space \' " \\ $(`touch NEVER`)'); home.mkdir()
   ch=home/'.codex'; ch.mkdir()
   original='model = "preserve-before-install"\n[features]\nsearch = true\n'; (ch/'config.toml').write_text(original)
   (ch/'auth.json').write_text('synthetic-auth-do-not-touch'); (home/'.profile').write_text('export KEEP=yes\n[ -f "$HOME/.claude/env" ] && . "$HOME/.claude/env"\n')
   key='DUMMY_\'"$(`touch INJECTION`)\\end'
   command=subprocess.run(['node','--input-type=module','-e',EMIT],cwd=ROOT,input=json.dumps({'os':platform,'source':source,'key':key,'base':'https://api.open-codex.com/v1'}),text=True,capture_output=True,check=True).stdout
   assert '\\${FULL_VER}' not in command
   env=dict(os.environ,HOME=str(home),CODEX_HOME=str(ch),PATH=str(bindir)+':/usr/bin:/bin',INSTALL_FIXTURE=str(root/'installer.sh'),OPENCODEX_INSTALL_NO_LAUNCH='1')
   bad=subprocess.run(['bash'],input=command,env=dict(env,FAIL_INSTALL='1'),text=True,capture_output=True)
   assert bad.returncode !=0 and (ch/'config.toml').read_text()==original, bad.stderr
   for n in range(2):
    run=subprocess.run(['bash'],input=command,env=env,text=True,capture_output=True)
    assert run.returncode==0, (platform,source,run.stdout,run.stderr)
    parsed=tomllib.loads((ch/'config.toml').read_text())
    assert parsed['projects'][str(home/'opencodex-workspace')]['trust_level']=='trusted'
    assert parsed['notice']['model_migrations']['gpt-5.3-codex']=='gpt-5.6-sol'
    provider='manual' if platform=='macos' else 'codex'
    assert parsed['model_provider']==provider
    assert parsed['model_providers'][provider]['base_url']=='https://api.open-codex.com/v1'
    assert parsed['model_reasoning_effort']=='xhigh'
    assert (ch/'auth.json').read_text()==('fixture-login' if platform=='macos' else 'synthetic-auth-do-not-touch')
    if platform=='macos':
     assert parsed['features']['search'] is True
     assert (ch/'daemon-test').read_text()=='restarted'
    for name in ['env','config.toml']: assert (ch/name).stat().st_mode & 0o777==0o600
    check=subprocess.run(['sh','-c','. "$CODEX_HOME/env"; printf "%s" "$CODEX_API_KEY"'],env=env,text=True,capture_output=True,check=True)
    assert check.stdout==key
    assert (home/'.profile').read_text().count('# OpenCodex environment')==1
    assert (home/'.zshenv').read_text().count('# OpenCodex environment')==1
    assert '.claude/env' in (home/'.profile').read_text()
    assert not (home/'.bash_profile').exists(), 'do not create a profile which shadows .profile'
    assert key not in run.stdout+run.stderr
    assert not (ROOT/'INJECTION').exists() and not (ROOT/'NEVER').exists()
   backups=list(ch.glob('install-backup.*/config.toml'))
   assert any(x.read_text()==original for x in backups)
   cases+=1
print(f'{cases} generated Unix installer cases passed; each covers failure and two installations')
