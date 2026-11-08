#!/usr/bin/env python3
"""Bounded secondary diagnostics, separate from normal ABCI timing samples."""
import argparse, itertools, json, os, pathlib, subprocess, time
import run

p = argparse.ArgumentParser()
p.add_argument('--hardware', required=True)
p.add_argument('--cpus', type=int, required=True)
p.add_argument('--cgo', type=int, required=True)
p.add_argument('--deadline', type=float, required=True)
a = p.parse_args()
root = pathlib.Path.cwd()
status = {'completed': [], 'omitted': []}
try:
    for variant, method, arm in itertools.product(
            ['lowgas', 'invalid-first', 'invalid-boundary', 'invalid-quorum-end'],
            ['CheckTxRejected', 'PrepareProposalRejected'], ['main', 'complete']):
        case = [variant, method, arm]
        if a.deadline-time.time()<30:
            status['omitted'].append(case)
            continue
        run.execute(arm, a.cgo, 'production', 200, method, 'cold', 0,
                    a.cpus, a.hardware, operations=5, variant=variant,
                    sample_label='budget60-5', timeout=a.deadline-time.time())
        status['completed'].append(case)
    outdir = root/'certificate-results'
    outdir.mkdir(exist_ok=True)
    for validators, variant, mode, arm in itertools.product(
            [100, 1000], ['valid', 'lowgas', 'invalid-first', 'invalid-boundary', 'invalid-quorum-end'],
            ['sequential', 'prepass'], ['main', 'complete']):
        if variant=='lowgas' and mode=='sequential':
            continue
        case = [validators, variant, mode, arm]
        if a.deadline-time.time()<20:
            status['omitted'].append(case)
            continue
        name = f'{a.hardware}-cpu{a.cpus}-{arm}-cgo{a.cgo}-v{validators}-{variant}-{mode}-budget60'
        output = outdir/(name+'.json')
        env = dict(os.environ, GOMAXPROCS=str(a.cpus), PFF_CERTIFICATE_CAMPAIGN='1',
                   PFF_VALIDATORS=str(validators), PFF_VARIANT=variant,
                   PFF_VERIFY_MODE=mode, PFF_OUTPUT=str(output))
        with (outdir/(name+'.log')).open('w') as log:
            subprocess.run(['taskset', '-c', f'0-{a.cpus-1}', str(root/'bin'/f'{arm}-cgo{a.cgo}-cert.test'),
                            '-test.run=^TestPFFCertificateCampaign$', '-test.timeout=5m'],
                           env=env, stdout=log, stderr=subprocess.STDOUT, check=True,
                           timeout=a.deadline-time.time())
        rows = json.loads(output.read_text())
        for row in rows:
            row.update(Checkpoint=arm, CGO=a.cgo, Hardware=a.hardware, CPUs=a.cpus, Sampling='budget60')
        output.write_text(json.dumps(rows, indent=2)+'\n')
        status['completed'].append(case)
    for count, method, arm in itertools.product([200, 2000], ['ProcessProposal', 'FinalizeBlock'], ['main', 'complete']):
        case = ['profile', count, method, arm]
        if a.deadline-time.time()<45:
            status['omitted'].append(case)
            continue
        run.execute(arm, a.cgo, 'override', count, method, 'cold', 0, a.cpus, a.hardware,
                    operations=1, profile=True, timeout=a.deadline-time.time())
        status['completed'].append(case)
    run.summarize()
    status['status'] = 'complete' if not status['omitted'] else 'budget-exhausted'
finally:
    (root/f'diagnostics-{a.hardware}-cgo{a.cgo}.json').write_text(json.dumps(status, indent=2)+'\n')
