#!/usr/bin/env python3
"""Separate in-memory keeper diagnostics; never mix with on-disk ABCI results."""
import argparse,itertools,json,os,pathlib,statistics,subprocess
p=argparse.ArgumentParser();p.add_argument('--hardware',default='c3d-highcpu-16');p.add_argument('--cpus',type=int,default=16);a=p.parse_args()
root=pathlib.Path.cwd();outdir=root/'certificate-results';outdir.mkdir(exist_ok=True)
for validators,cgo,variant,mode in itertools.product([100,1000],[0,1],['valid','lowgas','invalid-first','invalid-boundary','invalid-quorum-end'],['sequential','prepass']):
 if variant=='lowgas' and mode=='sequential':continue
 for rep in range(10):
  arms=['main','pr-stack','complete'];arms=arms[rep%3:]+arms[:rep%3]
  for arm in arms:
   name=f'{a.hardware}-cpu{a.cpus}-{arm}-cgo{cgo}-v{validators}-{variant}-{mode}-r{rep}'
   output=outdir/(name+'.json')
   if output.exists():continue
   env=dict(os.environ,GOMAXPROCS=str(a.cpus),PFF_CERTIFICATE_CAMPAIGN='1',PFF_VALIDATORS=str(validators),PFF_VARIANT=variant,PFF_VERIFY_MODE=mode,PFF_OUTPUT=str(output))
   with (outdir/(name+'.log')).open('w') as log:subprocess.run(['taskset','-c',f'0-{a.cpus-1}',str(root/'bin'/f'{arm}-cgo{cgo}-cert.test'),'-test.run=^TestPFFCertificateCampaign$','-test.timeout=5m'],env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
   rows=json.loads(output.read_text())
   for row in rows:row.update(Checkpoint=arm,CGO=cgo,Hardware=a.hardware,CPUs=a.cpus,Repetition=rep)
   output.write_text(json.dumps(rows,indent=2)+'\n')
groups={}
for path in outdir.glob('*.json'):
 for r in json.loads(path.read_text()):
  key=tuple(r[k] for k in ['Hardware','CPUs','Checkpoint','CGO','Validators','Variant','Mode']);groups.setdefault(key,[]).append(r['NS'])
summary=[]
for key,ns in sorted(groups.items()):
 r=dict(zip(['Hardware','CPUs','Checkpoint','CGO','Validators','Variant','Mode'],key));r.update(samples=len(ns),median_us=statistics.median(ns)/1000,p95_us=sorted(ns)[-3]/1000);summary.append(r)
(root/'certificate-summary.json').write_text(json.dumps(summary,indent=2)+'\n')
