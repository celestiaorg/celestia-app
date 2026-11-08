#!/usr/bin/env python3
"""Sequential, resumable interleaved campaign. Run from the campaign root."""
import argparse, csv, hashlib, itertools, json, math, os, pathlib, statistics, subprocess, time

ROOT = pathlib.Path.cwd()
METHODS = ['PrepareProposal','ProcessProposal','ProcessProposalProposer','FinalizeBlock','CheckTx','ReCheckTx','Commit']
OE = ['OEImmediate','OEComplete']
ARMS = ['main','pr-stack','complete']

def execute(arm, cgo, limits, count, method, cache, rep, cpus, hardware, operations=5, profile=False,variant="valid"):
    name = f'{hardware}-cpu{cpus}-{variant}-{limits}-cgo{cgo}-n{count}-{method}-{cache}-r{rep}-{arm}'
    if profile: name+='-profile'
    elif operations==1: name+='-smoke'
    out = ROOT/'results'/f'{name}.json'
    if out.exists() and not profile:
        rows=json.loads(out.read_text())
        if len(rows)==operations: return
    env=dict(os.environ, GOMAXPROCS=str(cpus), PFF_CAMPAIGN='1', PFF_COUNT=str(count), PFF_METHOD=method,
        PFF_CACHE=cache, PFF_VARIANT=variant, PFF_CHECKPOINT=arm, PFF_LIMITS=limits, PFF_CGO=str(cgo),
        PFF_OPERATIONS=str(operations), PFF_OUTPUT=str(out), TMPDIR=str(ROOT/'tmp'),PFF_FIXTURES=str(ROOT/'fixtures'/arm))
    if profile: env['PFF_PROFILE']=str(ROOT/'profiles'/name)
    binary=ROOT/'bin'/f'{arm}-cgo{cgo}-{limits}.test'
    cmd=['taskset','-c',f'0-{cpus-1}',str(binary),'-test.run=^TestPFFCampaign$','-test.timeout=30m']
    print(time.strftime('%FT%TZ',time.gmtime()),name,flush=True)
    with (ROOT/'logs'/f'{name}.log').open('w') as log:
        subprocess.run(cmd,env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
    rows=json.loads(out.read_text())
    assert len(rows)==operations
    for row in rows:
        row.update(Hardware=hardware,CPUs=cpus,Repetition=rep,Profile=profile,Smoke=operations==1)
        assert row['Validators']==100 and row['SignaturesPerPFF']==67
    out.write_text(json.dumps(rows,indent=2)+'\n')

def verify_results():
    groups={}
    for path in (ROOT/'results').glob('*.json'):
        for row in json.loads(path.read_text()):
            if row.get('Profile'):continue
            # OE responses equal ordinary FinalizeBlock; total wall times differ.
            method='FinalizeBlock' if row['Method'].startswith('OE') else row['Method']
            key=(row['Count'],method,row.get('Variant','valid'))
            values=(row['InitialHash'],row['Digest'],row['FinalHash'])
            # CheckTx/recheck gas/results may differ from each other, but never
            # between checkpoints, CGO modes, hardware or application cache states.
            if key in groups and groups[key]!=values:
                raise RuntimeError(f'differential mismatch: {key}: {path}: {groups[key]} != {values}')
            groups[key]=values
    (ROOT/'differential.json').write_text(json.dumps({'groups':len(groups),'status':'passed'},indent=2)+'\n')

def summarize():
    raw=[]
    for path in (ROOT/'results').glob('*.json'):
        raw.extend(r for r in json.loads(path.read_text()) if not r.get('Profile') and not r.get('Smoke'))
    keys=['Hardware','CPUs','Limits','CGO','Count','Method','Cache','Checkpoint','Variant']
    groups={}
    for row in raw: groups.setdefault(tuple(row[k] for k in keys),[]).append(row)
    summaries=[]
    for key,rows in sorted(groups.items()):
        r=dict(zip(keys,key));ns=sorted(x['NS'] for x in rows)
        r.update(samples=len(ns),median_ms=statistics.median(ns)/1e6,p95_ms=ns[math.ceil(.95*len(ns))-1]/1e6,
            wire_MiB=rows[0]['WireMiB'],square_MiB=rows[0]['SquareMiB'],
            median_allocs=statistics.median(x['Allocs'] for x in rows),median_bytes=statistics.median(x['Bytes'] for x in rows),
            peak_RSS_MiB=max(x['PeakRSS'] for x in rows)/(1<<20),
            us_per_tx=statistics.median(ns)/1e3/r['Count'])
        for metric in ['ProcessNS','FinalizeNS','CommitNS','SpeculativeNS','ResidualNS']:
            r[metric.removesuffix('NS')+'_median_ms']=statistics.median(x[metric] for x in rows)/1e6
        summaries.append(r)
    lookup={tuple(r[k] for k in keys):r for r in summaries}
    for r in summaries:
        for arm in ['main','pr-stack']:
            baseline=lookup.get(tuple(r[k] if k!='Checkpoint' else arm for k in keys))
            r[f'change_vs_{arm}_pct']=100*(r['median_ms']/baseline['median_ms']-1) if baseline else None
    (ROOT/'summary.json').write_text(json.dumps(summaries,indent=2)+'\n')
    if summaries:
        with (ROOT/'summary.csv').open('w') as f:
            w=csv.DictWriter(f,fieldnames=list(summaries[0]));w.writeheader();w.writerows(summaries)
    with (ROOT/'raw.csv').open('w') as f:
        fields=sorted(set().union(*(r.keys() for r in raw))) if raw else []
        w=csv.DictWriter(f,fieldnames=fields);w.writeheader();w.writerows(raw)
    verify_results()

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--hardware',default='c3d-highcpu-16');p.add_argument('--cpus',type=int,default=16)
    p.add_argument('--large',action='store_true');p.add_argument('--smoke',action='store_true');p.add_argument('--summarize',action='store_true')
    p.add_argument('--profiles',action='store_true');p.add_argument('--attacks',action='store_true');a=p.parse_args()
    for d in ['results','logs','tmp','profiles']: (ROOT/d).mkdir(exist_ok=True)
    if a.summarize:summarize();raise SystemExit
    if a.attacks:
        for cgo,variant,method in itertools.product([0,1],['lowgas','invalid-first','invalid-boundary','invalid-quorum-end'],['CheckTxRejected','PrepareProposalRejected']):
            for rep in range(10):
                for arm in ARMS[rep%3:]+ARMS[:rep%3]:
                    execute(arm,cgo,'production',200,method,'cold',rep,a.cpus,a.hardware,variant=variant)
        summarize();raise SystemExit
    cases=[('override',n) for n in ([1000,2000,5000] if a.large else [100,200,400,1000,2000,5000,5800])]
    if not a.large:cases=[('production',100),('production',200)]+cases
    if a.smoke:cases=[('production',100)]
    if a.profiles:cases=[('override',200),('override',2000)]
    for limits,count in cases:
        for cgo,method,cache in itertools.product([0,1],METHODS+OE,['cold','warm']):
            if a.profiles and (method not in ['ProcessProposal','FinalizeBlock'] or cache!='cold'):continue
            for rep in range(1 if (a.smoke or a.profiles) else 10):
                rotated=ARMS[rep%3:]+ARMS[:rep%3]
                for arm in rotated:
                    if method in OE and arm=='main':continue
                    execute(arm,cgo,limits,count,method,cache,rep,a.cpus,a.hardware,1 if a.smoke else 5,a.profiles)
    summarize()
