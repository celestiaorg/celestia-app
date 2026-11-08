#!/usr/bin/env python3
"""User-requested bounded campaign: main versus complete, OE disabled."""
import argparse, itertools, json, pathlib, subprocess, time
import run

p = argparse.ArgumentParser()
p.add_argument('--hardware', required=True)
p.add_argument('--cpus', type=int, required=True)
p.add_argument('--deadline', type=float, required=True, help='absolute Unix time')
p.add_argument('--extra', action='store_true')
p.add_argument('--cgo', type=int, choices=[0, 1])
p.add_argument('--operations', type=int, default=5)
a = p.parse_args()
root = pathlib.Path.cwd()
for d in ['results', 'logs', 'tmp', 'profiles']:
    (root/d).mkdir(exist_ok=True)
methods = ['PrepareProposal', 'ProcessProposal', 'FinalizeBlock', 'CheckTx', 'ReCheckTx', 'Commit']
cases = list(itertools.product([1000, 2000, 5000], [1, 0], methods, ['cold', 'warm']))
if a.extra:
    cases = list(itertools.product([100, 200, 400, 5800], [1, 0], methods, ['cold', 'warm']))
if a.cgo is not None:
    cases = [c for c in cases if c[1] == a.cgo]
status = dict(hardware=a.hardware, cpus=a.cpus, started=time.time(), deadline=a.deadline,
              cgo=a.cgo, operations=a.operations, requested_cases=len(cases)*2,
              completed_cases=0, omitted=[], status='running')
status_path = root/('quick-'+a.hardware+f'-cgo{a.cgo}'+('-extra' if a.extra else '')+'.json')
def save():
    status_path.write_text(json.dumps(status, indent=2)+'\n')
save()
try:
    for index, (count, cgo, method, cache) in enumerate(cases):
        arms = ['main', 'complete'] if index%2 == 0 else ['complete', 'main']
        for arm in arms:
            remaining = a.deadline-time.time()
            case = dict(count=count, cgo=cgo, method=method, cache=cache, arm=arm)
            if remaining < 20:
                status['omitted'].append(case)
                continue
            status['current'] = case
            save()
            try:
                run.execute(arm, cgo, 'production' if count<=200 else 'override', count,
                            method, cache, 0, a.cpus, a.hardware, operations=a.operations,
                            sample_label=f'budget60-{a.operations}', timeout=max(1, remaining-5))
            except subprocess.TimeoutExpired:
                status['omitted'].append(case)
                continue
            status['completed_cases'] += 1
            save()
    run.summarize()
    status['status'] = 'complete' if not status['omitted'] else 'budget-exhausted'
except Exception as e:
    status['status'] = 'failed'
    status['error'] = repr(e)
    raise
finally:
    status['finished'] = time.time()
    save()
