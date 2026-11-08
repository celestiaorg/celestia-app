#!/usr/bin/env python3
"""Export pinned checkpoints and overlay only tagged campaign instrumentation."""
import argparse, hashlib, io, json, pathlib, subprocess, tarfile
p=argparse.ArgumentParser();p.add_argument('output');p.add_argument('--base',required=True);p.add_argument('--stack',required=True);p.add_argument('--complete',default='HEAD');a=p.parse_args()
root=pathlib.Path(a.output).resolve();root.mkdir(parents=True,exist_ok=True)
refs={'main':a.base,'pr-stack':a.stack,'complete':a.complete}
harness=subprocess.check_output(['git','ls-files','*campaign*.go'],text=True).splitlines()
manifest={}
for arm,ref in refs.items():
 sha=subprocess.check_output(['git','rev-parse',ref],text=True).strip()
 dest=root/'source'/arm;dest.mkdir(parents=True,exist_ok=True)
 archive=subprocess.check_output(['git','archive',sha])
 with tarfile.open(fileobj=io.BytesIO(archive)) as tf:tf.extractall(dest)
 for name in harness:
  target=dest/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(pathlib.Path(name).read_bytes())
 if arm=='main':
  (dest/'app/pff_recheck_campaign.go').write_text('//go:build pffcampaign\n\npackage app\nfunc (app *App) campaignPurgeRecheck() {}\nfunc (app *App) campaignRecheckStats(stats map[string]int) { stats["decoded_capacity"]=0;stats["decoded_entries"]=0 }\n')
 if arm!='complete':
  (dest/'x/fibre/keeper/pff_evidence_campaign_test.go').write_text('//go:build pffcampaign\n\npackage keeper_test\nimport sdk "github.com/cosmos/cosmos-sdk/types"\nfunc campaignEvidence(ctx sdk.Context) sdk.Context {return ctx}\n')
 manifest[arm]={'sha':sha,'tree':subprocess.check_output(['git','rev-parse',sha+'^{tree}'],text=True).strip(),'go_mod_sha256':hashlib.sha256((dest/'go.mod').read_bytes()).hexdigest(),'harness':{name:hashlib.sha256((dest/name).read_bytes()).hexdigest() for name in harness}}
for name in ['run.py','build.sh','cert.py']:(root/name).write_bytes((pathlib.Path('tools/pffcampaign')/name).read_bytes())
(root/'source-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
with tarfile.open(root/'sources.tar.gz','w:gz') as tf:
 for name in ['source','source-manifest.json','run.py','build.sh','cert.py']:tf.add(root/name,arcname=name)
